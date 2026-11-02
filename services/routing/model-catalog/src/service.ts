// Catalog service: owns the authority cadence, the build passes that publish response files, and
// the models-table refresh job. All mutable state lives on the main thread; shared work runs with
// its own deadline so a disconnecting client only stops its own wait.
//
// Memory (design D6): a build pass parses the authority rows, compiles once, projects every
// requested and recently read key, writes each body to a file and drops everything. Between
// passes the process holds only the authority text, the output index and the table summary, so a
// client version seen for the first time in a generation costs one more pass (compile included)
// rather than keeping the compiled generation resident.
import { AuthorityOwner } from "./authority.ts";
import { CatalogConfigError, CatalogUnavailableError, prepare, project, type BuildDeps, type PreparedGeneration } from "./build.ts";
import { MetadataConfigError } from "./core.ts";
import { errorMessage, type Logger } from "./log.ts";
import { PROJECTION_TTL_MS, type Lease, type OutputStore } from "./outputs.ts";
import { codexBody, renderTableHead, renderTableRows, standardBody, summarize, TABLE_TAIL, tableData, type TablePage, type TableSummary } from "./project.ts";
import { READ_CACHE_TTL_MS } from "./read-cache.ts";

/** Channel inventory version used for the diagnostic table (CPA's own manifest stays pinned to 1). */
export const TABLE_CLIENT_VERSION = "v0.65.0";
export const STANDARD_KEY = "standard";
export const TABLE_KEY = "table";
export const PINNED_KEYS: ReadonlySet<string> = new Set([STANDARD_KEY, TABLE_KEY]);
/** Codex keys read within this window are rebuilt by every pass; older ones expire with their generation. */
export const ACTIVE_KEY_MS = 30 * 60_000;
const codexKey = (clientVersion: string) => `codex:${clientVersion}`;

// Exposed only by `--expose-gc` (the image entrypoint). Builds leave their garbage, an enlarged
// young generation and empty pages behind, and an idle process allocates nothing that would make
// V8 collect them; a memory-reducing collection after the work settles returns them to the OS.
type GC = (options: { type: "major"; execution: "sync"; flavor: "last-resort" }) => void;
const collect = (globalThis as { gc?: GC }).gc;
const SETTLE_MS = 1_000;

/** A response: `body`, then (when present) the streamed file and its tail. */
export interface Reply {
  status: number;
  contentType: string;
  body: string;
  file?: Lease & { tail: string };
}

const json = (status: number, body: string): Reply => ({ status, contentType: "application/json", body });
const HTML = "text/html; charset=utf-8";
export const errorReply = (status: number, code: string, message: string) => json(status, JSON.stringify({ error: { code, message } }));

function errorFor(error: unknown): Reply {
  if (error instanceof CatalogUnavailableError) return errorReply(502, "native_manifest_failed", error.message);
  if (error instanceof CatalogConfigError) return errorReply(500, "runtime_config_invalid", error.message);
  if (error instanceof MetadataConfigError) return errorReply(500, "metadata_config_invalid", error.message);
  return errorReply(500, "catalog_build_failed", errorMessage(error));
}

/** Resolves when `promise` settles, or rejects as soon as `signal` aborts (the work continues). */
function wait<T>(promise: Promise<T>, signal?: AbortSignal): Promise<T> {
  if (!signal) return promise;
  return new Promise<T>((resolve, reject) => {
    if (signal.aborted) return reject(signal.reason);
    const onAbort = () => reject(signal.reason);
    signal.addEventListener("abort", onAbort, { once: true });
    promise.then(resolve, reject).finally(() => signal.removeEventListener("abort", onAbort));
  });
}

interface RefreshResult {
  ok: boolean;
  at: number;
  staleInputs: number;
  status: number;
  error?: string;
}

interface Pass {
  readonly generation: number;
  readonly keys: Set<string>;
  table: boolean;
  bypass: boolean;
  started: boolean;
  promise?: Promise<number>;
}

export interface ServiceOptions {
  build: BuildDeps;
  owner: AuthorityOwner;
  outputs: OutputStore;
  log: Logger;
  clock?: () => number;
  /** Authority cadence; defaults to the read-cache freshness window. */
  authorityIntervalMs?: number;
}

export class CatalogService {
  readonly build: BuildDeps;
  readonly owner: AuthorityOwner;
  readonly outputs: OutputStore;
  readonly log: Logger;
  readonly clock: () => number;
  readonly authorityIntervalMs: number;

  readonly #stop = new AbortController();
  #gate: Promise<unknown> = Promise.resolve();
  #passes: Pass[] = [];
  #timers: NodeJS.Timeout[] = [];
  #pending = new Set<Promise<unknown>>();

  #refresh: Promise<RefreshResult> | undefined;
  #table: RefreshResult | undefined;
  #lastAttempt: RefreshResult | undefined;
  #lastSuccess = 0;
  #nextScheduled = 0;
  #settle: NodeJS.Timeout | undefined;

  constructor(options: ServiceOptions) {
    this.build = options.build;
    this.owner = options.owner;
    this.outputs = options.outputs;
    this.log = options.log;
    this.clock = options.clock ?? Date.now;
    this.authorityIntervalMs = options.authorityIntervalMs ?? READ_CACHE_TTL_MS;
  }

  get stopped(): boolean {
    return this.#stop.signal.aborted;
  }

  ready(): { ready: boolean; generation: number } {
    const current = this.owner.current();
    return { ready: current.generation > 0 && current.cpa !== "failed", generation: current.generation };
  }

  /** Startup table job, scheduled table jobs and the authority cadence. */
  start(): void {
    const { scheduledRefreshMs } = this.build.config;
    this.#nextScheduled = this.clock() + scheduledRefreshMs;
    this.startRefresh(false);
    this.#timers.push(
      setInterval(() => {
        this.#nextScheduled = this.clock() + scheduledRefreshMs;
        this.startRefresh(false);
      }, scheduledRefreshMs),
      setInterval(() => this.#track(this.owner.refresh({ signal: this.#stop.signal }).catch(() => undefined)).finally(() => this.#settleLater()), this.authorityIntervalMs),
    );
  }

  /** Stops timers, cancels active work and waits for it to settle. */
  async stop(): Promise<void> {
    for (const timer of this.#timers) clearInterval(timer);
    this.#timers = [];
    clearTimeout(this.#settle);
    this.#nextScheduled = 0;
    this.#stop.abort(new DOMException("service stopping", "AbortError"));
    while (this.#pending.size > 0) await Promise.allSettled([...this.#pending]);
  }

  #track<T>(promise: Promise<T>): Promise<T> {
    this.#pending.add(promise);
    promise.finally(() => this.#pending.delete(promise)).catch(() => undefined);
    return promise;
  }

  /** Collects once no pass or refresh job is left; repeated calls share one timer. */
  #settleLater(): void {
    if (!collect || this.stopped || this.#settle) return;
    this.#settle = setTimeout(() => {
      this.#settle = undefined;
      if (this.#passes.length > 0 || this.#refresh) return; // the last one to finish settles again
      collect({ type: "major", execution: "sync", flavor: "last-resort" });
    }, SETTLE_MS).unref();
  }

  #deadline(): AbortSignal {
    return AbortSignal.any([this.#stop.signal, AbortSignal.timeout(this.build.config.overallDeadlineMs + 5_000)]);
  }

  /** Passes run one at a time so peak memory stays bounded. */
  #serialize<T>(work: () => Promise<T>): Promise<T> {
    const run = this.#gate.then(work, work);
    this.#gate = run.catch(() => undefined);
    return run;
  }

  // -------------------------------------------------------------------------
  // Build passes.

  /**
   * Publishes `keys` (and the table when asked) for the current generation. Joins a pass of this
   * generation that already covers the request, or widens the queued one; otherwise queues a new
   * pass. Resolves with the table projection's stale-input count.
   */
  #publish(keys: readonly string[], options: { table?: boolean; bypass?: boolean } = {}): Promise<number> {
    const generation = this.owner.current().generation;
    const covers = (pass: Pass) =>
      pass.generation === generation && keys.every((key) => pass.keys.has(key)) && (!options.table || (pass.table && (pass.bypass || !options.bypass)));
    const joined = this.#passes.find(covers);
    if (joined) return joined.promise!;
    const queued = this.#passes.find((pass) => !pass.started && pass.generation === generation);
    if (queued) {
      for (const key of keys) queued.keys.add(key);
      queued.table ||= options.table === true;
      queued.bypass ||= options.bypass === true;
      return queued.promise!;
    }
    const pass: Pass = { generation, keys: new Set(keys), table: options.table === true, bypass: options.bypass === true, started: false };
    pass.promise = this.#track(this.#serialize(() => this.#run(pass))).finally(() => {
      this.#passes = this.#passes.filter((other) => other !== pass);
      this.#settleLater();
    });
    this.#passes.push(pass);
    return pass.promise;
  }

  async #run(pass: Pass): Promise<number> {
    pass.started = true;
    const authority = this.owner.current();
    if (authority.cpa === "failed") throw new CatalogUnavailableError("CPA native manifest unavailable");
    const signal = this.#deadline();
    let prepared: PreparedGeneration;
    try {
      prepared = await prepare(this.build, authority, signal);
    } catch (error) {
      this.log.error("catalog preparation failed", { generation: authority.generation, err: errorMessage(error) });
      throw error;
    }
    const { generation } = authority;
    const now = this.clock();
    const keys = new Set([STANDARD_KEY, ...pass.keys, ...this.outputs.activeKeys(now - ACTIVE_KEY_MS, PINNED_KEYS)]);
    for (const key of keys) {
      const body = key === STANDARD_KEY ? standardBody(prepared) : codexBody(await project(this.build, prepared, { clientVersion: key.slice("codex:".length), signal }));
      await this.outputs.publish(key, generation, body, now);
    }
    let staleInputs = 0;
    if (pass.table) {
      const projection = await project(this.build, prepared, { clientVersion: TABLE_CLIENT_VERSION, bypass: pass.bypass, signal });
      staleInputs = projection.staleInputs;
      const data = tableData(prepared, projection, TABLE_CLIENT_VERSION);
      await this.outputs.publish(TABLE_KEY, generation, renderTableRows(data), now, summarize(data));
    }
    // Keys nobody read recently leave with their generation; the table stays until replaced.
    this.outputs.retireOlder(generation, new Set([...keys, TABLE_KEY]));
    return staleInputs;
  }

  // -------------------------------------------------------------------------
  // Catalogs.

  /** `GET /v1/models`: Codex manifest with a client_version, otherwise the standard list. */
  /** The client's `signal` only ends its own wait; the shared work below never sees it. */
  async catalog(clientVersion: string, signal?: AbortSignal): Promise<Reply> {
    const key = clientVersion === "" ? STANDARD_KEY : codexKey(clientVersion);
    const failure = await wait(this.#track(this.#catalog(key)), signal);
    if (failure) return failure;
    // Leased synchronously after the wait: a newer file published meanwhile is the one served.
    const lease = this.outputs.lease(key, this.clock());
    return lease ? { status: 200, contentType: "application/json", body: "", file: { ...lease, tail: "" } } : errorReply(500, "catalog_build_failed", "published catalog missing");
  }

  /** Ensures `key` is published; returns an error reply instead when it cannot be served. */
  async #catalog(key: string): Promise<Reply | undefined> {
    try {
      if (this.owner.current().generation === 0) await this.owner.refresh({ signal: this.#stop.signal });
      const authority = this.owner.current();
      if (authority.cpa === "failed") throw new CatalogUnavailableError("CPA native manifest unavailable");
      const entry = this.outputs.get(key);
      if (entry?.generation === authority.generation) {
        // Stale-while-revalidate: fresher @cpa-upstream rows arrive with the background pass.
        if (this.clock() - entry.stored >= PROJECTION_TTL_MS) this.#publish([key]).catch(() => undefined);
        return undefined;
      }
      try {
        await this.#publish([key]);
      } catch (error) {
        // The last good file keeps serving unless the authority itself is unavailable.
        if (error instanceof CatalogUnavailableError || !this.outputs.get(key)) throw error;
      }
      return undefined;
    } catch (error) {
      return errorFor(error);
    }
  }

  // -------------------------------------------------------------------------
  // Models table: one coalesced full-chain refresh job.

  /** Starts or joins the refresh job; `entry` skips it inside the successful-refresh window. */
  startRefresh(entry: boolean): Promise<RefreshResult> | undefined {
    if (this.stopped) return undefined;
    if (entry && this.#lastSuccess > 0 && this.clock() - this.#lastSuccess <= this.build.config.entryRefreshWindowMs) return undefined;
    if (this.#refresh) return this.#refresh;
    const job = this.#track(this.#forceRefresh()).then((result) => {
      this.#lastAttempt = result;
      if (result.ok && !this.stopped) {
        this.#table = result;
        if (result.staleInputs === 0) this.#lastSuccess = result.at;
      }
      this.#refresh = undefined;
      this.#settleLater();
      return result;
    });
    this.#refresh = job;
    return job;
  }

  async #forceRefresh(): Promise<RefreshResult> {
    const signal = this.#deadline();
    let staleInputs = 0;
    try {
      const outcome = await this.owner.refresh({ bypass: true, signal });
      staleInputs += outcome.staleInputs + (outcome.generation.aisix === "failed" ? 1 : 0);
      if (outcome.scriptError) {
        // The generation still served keeps its table; the attempt is reported as failed.
        return { ok: false, at: this.clock(), staleInputs, status: 500, error: outcome.scriptError.message };
      }
      if (outcome.generation.cpa === "failed") {
        return { ok: false, at: this.clock(), staleInputs, status: 502, error: outcome.errors.join("; ") || "CPA native manifest unavailable" };
      }
      if (outcome.errors.length > 0) this.log.warn("forced refresh incomplete; rebuilding from current snapshot", { err: outcome.errors.join("; ") });
      // The same pass republishes every recently read catalog from the fresh inputs.
      staleInputs += await this.#publish([], { table: true, bypass: true });
      return { ok: true, at: this.clock(), staleInputs, status: 200 };
    } catch (error) {
      const reply = errorFor(error);
      return { ok: false, at: this.clock(), staleInputs, status: reply.status, error: errorMessage(error) };
    }
  }

  /** The table page: head rendered now, published rows streamed from their file. */
  #tableReply(status: number, page: Omit<TablePage, "table">): Reply {
    const lease = page.error === undefined ? this.outputs.lease(TABLE_KEY, this.clock()) : undefined;
    const head = renderTableHead({ ...page, table: lease?.meta as TableSummary | undefined });
    if (!lease) return { status, contentType: HTML, body: head + TABLE_TAIL };
    return { status, contentType: HTML, body: head, file: { ...lease, tail: TABLE_TAIL } };
  }

  /** `GET /models-table`: last published table without waiting; may start a background refresh. */
  tablePage(): Reply {
    this.startRefresh(true);
    const table = this.#table;
    const attempted = this.#lastAttempt;
    let notice: { level: "success" | "warning"; text: string } | undefined;
    if (!table) notice = { level: "warning", text: "加载中 · 暂无目录数据；后台刷新不阻塞页面，可手动刷新并等待完成" };
    else if (this.#refresh) notice = { level: "warning", text: "正在后台刷新 · 当前显示最近可用的目录" };
    else if (table.staleInputs > 0) notice = { level: "warning", text: "部分输入刷新失败；显示由最后成功输入重新合成的目录" };
    if (attempted && !attempted.ok) {
      notice = { level: "warning", text: `上次刷新失败 · ${new Date(attempted.at).toISOString()}${attempted.error ? ` · ${attempted.error}` : ""}${notice ? ` · ${notice.text}` : ""}` };
    }
    // A scheduled authority collection can fail on catalog.lua without a table refresh job.
    const failure = this.owner.scriptFailure();
    if (failure && !(attempted && !attempted.ok && attempted.at >= failure.at)) {
      notice = { level: "warning", text: `catalog.lua 执行失败 · ${new Date(failure.at).toISOString()} · ${failure.error.message} · 仍在提供上一代目录${notice ? ` · ${notice.text}` : ""}` };
    }
    const page = { notice, lastSuccess: this.#lastSuccess, nextScheduled: this.#nextScheduled, renderedAt: table?.at ?? this.clock() };
    return table ? this.#tableReply(200, page) : { status: 200, contentType: HTML, body: renderTableHead(page) + TABLE_TAIL };
  }

  /** `POST /models-table/refresh`: waits for the shared job and renders its own result. */
  async refreshPage(signal?: AbortSignal): Promise<Reply> {
    const job = this.startRefresh(false);
    if (!job) return errorReply(503, "refresh_stopped", "catalog refresh is shutting down");
    const result = await wait(job, signal);
    const at = new Date(result.at).toISOString();
    const page = { lastSuccess: this.#lastSuccess, nextScheduled: this.#nextScheduled, renderedAt: result.at };
    if (!result.ok) return this.#tableReply(result.status, { ...page, error: result.error ?? "refresh failed", notice: { level: "warning", text: `刷新失败 · ${at}` } });
    const notice =
      result.staleInputs > 0
        ? { level: "warning" as const, text: `刷新完成 · ${at} · ${result.staleInputs} 个输入接口刷新失败，已使用其最后成功的响应或省略不可用补充源` }
        : { level: "success" as const, text: `刷新成功 · ${at}` };
    return this.#tableReply(200, { ...page, notice });
  }
}
