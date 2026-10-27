// Catalog service: owns the authority cadence, per-generation preparation, the projection cache and
// the models-table refresh job. All mutable state lives on the main thread; shared work runs with
// its own deadline so a disconnecting client only stops its own wait.
import { AuthorityOwner } from "./authority.ts";
import { CatalogConfigError, CatalogUnavailableError, prepare, project, type BuildDeps, type PreparedGeneration } from "./build.ts";
import { MetadataConfigError } from "./core.ts";
import { errorMessage, type Logger } from "./log.ts";
import { codexBody, ProjectionCache, renderTable, standardBody, tableData, type TableData } from "./project.ts";
import { READ_CACHE_TTL_MS } from "./read-cache.ts";

/** Channel inventory version used for the diagnostic table (CPA's own manifest stays pinned to 1). */
export const TABLE_CLIENT_VERSION = "v0.65.0";

export interface Reply {
  status: number;
  contentType: string;
  body: string;
}

const json = (status: number, body: string): Reply => ({ status, contentType: "application/json", body });
const html = (status: number, body: string): Reply => ({ status, contentType: "text/html; charset=utf-8", body });
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
  table?: TableData;
  status: number;
  error?: string;
}

export interface ServiceOptions {
  build: BuildDeps;
  owner: AuthorityOwner;
  log: Logger;
  clock?: () => number;
  /** Authority cadence; defaults to the read-cache freshness window. */
  authorityIntervalMs?: number;
}

export class CatalogService {
  readonly build: BuildDeps;
  readonly owner: AuthorityOwner;
  readonly log: Logger;
  readonly clock: () => number;
  readonly cache: ProjectionCache;
  readonly authorityIntervalMs: number;

  readonly #stop = new AbortController();
  #prepared: { generation: number; promise: Promise<PreparedGeneration> } | undefined;
  #lastGood: PreparedGeneration | undefined;
  #gate: Promise<unknown> = Promise.resolve();
  #timers: NodeJS.Timeout[] = [];
  #pending = new Set<Promise<unknown>>();

  #refresh: Promise<RefreshResult> | undefined;
  #table: RefreshResult | undefined;
  #lastAttempt: RefreshResult | undefined;
  #lastSuccess = 0;
  #nextScheduled = 0;

  constructor(options: ServiceOptions) {
    this.build = options.build;
    this.owner = options.owner;
    this.log = options.log;
    this.clock = options.clock ?? Date.now;
    this.cache = new ProjectionCache(this.clock);
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
      setInterval(() => this.#track(this.owner.refresh({ signal: this.#stop.signal }).catch(() => undefined)), this.authorityIntervalMs),
    );
  }

  /** Stops timers, cancels active work and waits for it to settle. */
  async stop(): Promise<void> {
    for (const timer of this.#timers) clearInterval(timer);
    this.#timers = [];
    this.#nextScheduled = 0;
    this.#stop.abort(new DOMException("service stopping", "AbortError"));
    while (this.#pending.size > 0) await Promise.allSettled([...this.#pending]);
  }

  #track<T>(promise: Promise<T>): Promise<T> {
    this.#pending.add(promise);
    promise.finally(() => this.#pending.delete(promise)).catch(() => undefined);
    return promise;
  }

  #deadline(): AbortSignal {
    return AbortSignal.any([this.#stop.signal, AbortSignal.timeout(this.build.config.overallDeadlineMs + 5_000)]);
  }

  /** Builds run one at a time so peak memory stays bounded. */
  #serialize<T>(work: () => Promise<T>): Promise<T> {
    const run = this.#gate.then(work, work);
    this.#gate = run.catch(() => undefined);
    return run;
  }

  /** One preparation per authority generation; a failure keeps serving the last good one. */
  async #prepare(strict: boolean): Promise<PreparedGeneration> {
    const authority = this.owner.current();
    if (authority.cpa === "failed") throw new CatalogUnavailableError("CPA native manifest unavailable");
    let entry = this.#prepared;
    if (!entry || entry.generation !== authority.generation) {
      const promise = this.#track(this.#serialize(() => prepare(this.build, authority, this.#deadline())));
      entry = { generation: authority.generation, promise };
      this.#prepared = entry;
      const current = entry;
      promise.then(
        (prepared) => {
          if (!this.#lastGood || prepared.authority.generation >= this.#lastGood.authority.generation) this.#lastGood = prepared;
        },
        (error: unknown) => {
          this.log.error("catalog preparation failed", { generation: authority.generation, err: errorMessage(error) });
          if (this.#prepared === current) this.#prepared = undefined;
        },
      );
    }
    try {
      return await entry.promise;
    } catch (error) {
      if (!strict && this.#lastGood && !(error instanceof CatalogUnavailableError)) return this.#lastGood;
      throw error;
    }
  }

  /** `GET /v1/models`: Codex manifest with a client_version, otherwise the standard list. */
  /** The client's `signal` only ends its own wait; the shared work below never sees it. */
  catalog(clientVersion: string, signal?: AbortSignal): Promise<Reply> {
    return wait(this.#track(this.#catalog(clientVersion)), signal);
  }

  async #catalog(clientVersion: string): Promise<Reply> {
    try {
      if (this.owner.current().generation === 0) await this.owner.refresh({ signal: this.#stop.signal });
      const prepared = await this.#prepare(false);
      if (clientVersion === "") return json(200, standardBody(prepared));
      const body = await this.cache.load(prepared.authority.generation, `codex:${clientVersion}`, () =>
        this.#serialize(async () => codexBody(await project(this.build, prepared, { clientVersion, signal: this.#deadline() }))),
      );
      return json(200, body);
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
      if (outcome.generation.cpa === "failed") {
        return { ok: false, at: this.clock(), staleInputs, status: 502, error: outcome.errors.join("; ") || "CPA native manifest unavailable" };
      }
      if (outcome.errors.length > 0) this.log.warn("forced refresh incomplete; rebuilding from current snapshot", { err: outcome.errors.join("; ") });
      const prepared = await this.#prepare(true);
      const projection = await this.#serialize(() => project(this.build, prepared, { clientVersion: TABLE_CLIENT_VERSION, bypass: true, signal }));
      staleInputs += projection.staleInputs;
      // Fresh inputs were stored; JSON projections rebuild from them instead of a pre-refresh body.
      this.cache.clear();
      return { ok: true, at: this.clock(), staleInputs, status: 200, table: tableData(prepared, projection, TABLE_CLIENT_VERSION) };
    } catch (error) {
      const reply = errorFor(error);
      return { ok: false, at: this.clock(), staleInputs, status: reply.status, error: errorMessage(error) };
    }
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
    return html(200, renderTable({ table: table?.table, notice, lastSuccess: this.#lastSuccess, nextScheduled: this.#nextScheduled, renderedAt: table?.at ?? this.clock() }));
  }

  /** `POST /models-table/refresh`: waits for the shared job and renders its own result. */
  async refreshPage(signal?: AbortSignal): Promise<Reply> {
    const job = this.startRefresh(false);
    if (!job) return errorReply(503, "refresh_stopped", "catalog refresh is shutting down");
    const result = await wait(job, signal);
    const at = new Date(result.at).toISOString();
    const page = { lastSuccess: this.#lastSuccess, nextScheduled: this.#nextScheduled, renderedAt: result.at };
    if (!result.ok) {
      return html(result.status, renderTable({ ...page, error: result.error ?? "refresh failed", notice: { level: "warning", text: `刷新失败 · ${at}` } }));
    }
    const notice =
      result.staleInputs > 0
        ? { level: "warning" as const, text: `刷新完成 · ${at} · ${result.staleInputs} 个输入接口刷新失败，已使用其最后成功的响应或省略不可用补充源` }
        : { level: "success" as const, text: `刷新成功 · ${at}` };
    return html(200, renderTable({ ...page, table: result.table, notice }));
  }
}
