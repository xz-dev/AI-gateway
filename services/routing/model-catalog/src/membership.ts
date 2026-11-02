// Membership script host (design D1, D2, D6–D9; warm-worker D1–D5): reads the profile's `catalog.lua`,
// runs it in one resident worker reused across authority collections (a new Lua state per run),
// enforces the init and wall-time bounds from this thread and turns the per-candidate decisions into
// members and rejected candidates.
import { createHash } from "node:crypto";
import { open } from "node:fs/promises";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { Worker } from "node:worker_threads";

export type MemberSource = "cpa" | "aisix" | "static";
export type PrefixMode = "prefix" | "forced" | "fallback";

/** What `select(m)` sees: a copy, never a host object. */
export interface Candidate {
  readonly id: string;
  readonly sources: Readonly<Partial<Record<MemberSource, true>>>;
  readonly mode: PrefixMode;
  readonly has_prefix: boolean;
  readonly prefix?: string;
  /** Host passthrough flag (OAuth registration under an inferred prefix, forced or fallback mode). */
  readonly native: boolean;
}

export type Decision = { readonly keep: true; readonly source?: MemberSource; readonly compile: boolean } | { readonly keep: false; readonly reason?: string };

export const LIMITS = Object.freeze({
  loadInstructions: 1_000_000,
  callInstructions: 100_000,
  totalInstructions: 20_000_000,
  memoryBytes: 16 << 20,
  wallMs: 2_000,
  /** Worker start-up, runtime preparation and the new Lua state; trusted code only. */
  initMs: 10_000,
  scriptBytes: 256 << 10,
  workerHeapMb: 32,
  /** Young-generation cap: a resident worker would otherwise keep up to ~65 MiB of new space committed. */
  workerYoungMb: 8,
});
export type Limits = { readonly [K in keyof typeof LIMITS]: number };

/** Phase 1: a new Lua state with the trusted prelude; the worker never sees the script here. */
export interface WorkerInit {
  readonly op: "init";
  readonly limits: Limits;
  /** Count-hook granularity in VM instructions. */
  readonly hookStep: number;
}

/** Phase 2, under the execution clock: load the script, decide every candidate, close the state. */
export interface WorkerRun {
  readonly op: "run";
  readonly script: string;
  readonly candidates: readonly Candidate[];
}

export type WorkerMessage = WorkerInit | WorkerRun;

export interface InstructionUse {
  readonly load: number;
  readonly maxCall: number;
  readonly total: number;
}

export type WorkerReply =
  | { ok: true; ready: true }
  | { ok: true; decisions: Decision[]; instructions: InstructionUse }
  | { ok: false; errorClass: string; message: string; candidate?: string };

export const REJECTED_DEFAULT_REASON = "rejected by catalog.lua";

/** A script load error, runtime error, invalid return, exceeded bound or worker failure. */
export class MembershipScriptError extends Error {
  readonly errorClass: string;
  readonly candidate: string | undefined;
  constructor(errorClass: string, message: string, candidate?: string) {
    super(`membership script ${errorClass}${candidate === undefined ? "" : ` at ${JSON.stringify(candidate)}`}: ${message}`);
    this.errorClass = errorClass;
    this.candidate = candidate;
  }
}

export interface MembershipScript {
  readonly text: string;
  readonly digest: string;
}

export function membershipScript(bytes: Buffer): MembershipScript {
  if (bytes.length > LIMITS.scriptBytes) throw new MembershipScriptError("script_file", `catalog.lua exceeds ${LIMITS.scriptBytes} bytes`);
  return { text: bytes.toString("utf8"), digest: createHash("sha256").update(bytes).digest("hex") };
}

/** Reads a regular file of at most 256 KiB; a directory (a bind mount of a missing source) fails. */
export async function readMembershipScript(path: string): Promise<MembershipScript> {
  let handle;
  try {
    handle = await open(path, "r");
  } catch (error) {
    throw new MembershipScriptError("script_file", `cannot open ${path}: ${(error as NodeJS.ErrnoException).code ?? "error"}`);
  }
  try {
    const stat = await handle.stat();
    if (!stat.isFile()) throw new MembershipScriptError("script_file", `${path} is not a regular file`);
    if (stat.size > LIMITS.scriptBytes) throw new MembershipScriptError("script_file", `${path} exceeds ${LIMITS.scriptBytes} bytes`);
    const buffer = Buffer.alloc(LIMITS.scriptBytes + 1);
    const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0);
    return membershipScript(buffer.subarray(0, bytesRead));
  } finally {
    await handle.close();
  }
}

const SOURCE_RUN = import.meta.url.endsWith(".ts");
// Source runs (tests, `node src/main.ts`) use the .ts worker and the package's wasm; the bundle
// ships dist/membership-worker.mjs and dist/glue.wasm.
export const MEMBERSHIP_WORKER_FILE = new URL(SOURCE_RUN ? "./membership-worker.ts" : "./membership-worker.mjs", import.meta.url);
export const WASM_PATH = SOURCE_RUN ? createRequire(import.meta.url).resolve("wasmoon/dist/glue.wasm") : fileURLToPath(new URL("./glue.wasm", import.meta.url));

export interface RunOptions {
  readonly workerFile?: string | URL;
  readonly limits?: Limits;
  readonly hookStep?: number;
  /** `false`: a worker for this run only, terminated when it finishes (dry-run, validate-config). */
  readonly resident?: boolean;
}

export interface ScriptRun {
  readonly decisions: readonly Decision[];
  readonly instructions: InstructionUse;
  /** The worker thread that ran the script. */
  readonly threadId: number;
}

// One resident worker per process (design D4). `key` covers what is fixed at spawn: the script
// digest, the worker file and the heap limits. Runs are serialized.
let resident: { worker: Worker; key: string } | undefined;
const live = new Set<Worker>();
let queue: Promise<unknown> = Promise.resolve();

/** The resident worker and the count of membership worker threads still alive. */
export function membershipWorkers(): { resident: Worker | undefined; live: number } {
  return { resident: resident?.worker, live: live.size };
}

function spawn(file: string | URL, limits: Limits): Worker {
  const worker = new Worker(file, { workerData: { wasmPath: WASM_PATH }, resourceLimits: { maxOldGenerationSizeMb: limits.workerHeapMb, maxYoungGenerationSizeMb: limits.workerYoungMb } });
  live.add(worker);
  // An idle exit or error only drops the record; a run in progress sees it through its own listeners.
  worker.on("error", () => undefined);
  worker.once("exit", () => {
    live.delete(worker);
    if (resident?.worker === worker) resident = undefined;
  });
  return worker;
}

async function discard(worker: Worker): Promise<void> {
  if (resident?.worker === worker) resident = undefined;
  await worker.terminate();
  live.delete(worker);
}

/** Posts one message and waits for the reply, a timeout, an error or an exit. */
function exchange(worker: Worker, message: WorkerMessage, ms: number, timeout: () => MembershipScriptError): Promise<WorkerReply> {
  return new Promise((resolve, reject) => {
    const done = (finish: () => void) => {
      clearTimeout(timer);
      worker.off("message", onMessage).off("error", onError).off("exit", onExit);
      finish();
    };
    const onMessage = (reply: WorkerReply) => done(() => resolve(reply));
    const onError = (error: Error) => done(() => reject(new MembershipScriptError("worker_exit", error.message.slice(0, 300))));
    const onExit = (code: number) => done(() => reject(new MembershipScriptError("worker_exit", `worker exited with code ${code}`)));
    const timer = setTimeout(() => done(() => reject(timeout())), ms);
    worker.on("message", onMessage).on("error", onError).on("exit", onExit);
    worker.postMessage(message);
  });
}

async function runOnce(script: MembershipScript, candidates: readonly Candidate[], options: RunOptions): Promise<ScriptRun> {
  const limits = options.limits ?? LIMITS;
  const file = options.workerFile ?? MEMBERSHIP_WORKER_FILE;
  const keep = options.resident !== false;
  const key = `${script.digest}\n${String(file)}\n${limits.workerHeapMb}\n${limits.workerYoungMb}`;
  let worker: Worker;
  if (keep && resident?.key === key) worker = resident.worker;
  else {
    if (keep && resident) await discard(resident.worker);
    worker = spawn(file, limits);
    if (keep) resident = { worker, key };
  }
  const threadId = worker.threadId;
  worker.ref();
  try {
    const ready = await exchange(worker, { op: "init", limits, hookStep: options.hookStep ?? 1_000 }, limits.initMs, () => new MembershipScriptError("init_timeout", `initialization exceeded ${limits.initMs} ms`));
    if (!ready.ok) throw new MembershipScriptError(ready.errorClass, ready.message, ready.candidate);
    // The execution clock starts only now; the script travels in this message (design D1).
    const reply = await exchange(worker, { op: "run", script: script.text, candidates }, limits.wallMs, () => new MembershipScriptError("wall_time", `collection exceeded ${limits.wallMs} ms`));
    if (!reply.ok) throw new MembershipScriptError(reply.errorClass, reply.message, reply.candidate);
    if (!("decisions" in reply)) throw new MembershipScriptError("worker_exit", "unexpected reply");
    if (!keep) await discard(worker);
    return { decisions: reply.decisions, instructions: reply.instructions, threadId };
  } catch (error) {
    await discard(worker);
    throw error;
  } finally {
    if (resident?.worker === worker) worker.unref();
  }
}

/**
 * One run on the resident worker: init (new Lua state, 10 s bound), then execution (script load and
 * every `select`, 2 s bound). Any failure terminates the worker, awaited, before the rejection.
 */
export function runMembershipScript(script: MembershipScript, candidates: readonly Candidate[], options: RunOptions = {}): Promise<ScriptRun> {
  const result = queue.then(() => runOnce(script, candidates, options));
  queue = result.catch(() => undefined);
  return result;
}

/** Server shutdown: terminates the resident worker. */
export async function closeMembershipWorker(): Promise<void> {
  const worker = resident?.worker;
  if (worker) await discard(worker);
}

// ---------------------------------------------------------------------------
// Dry-run (design D9): every source combination in every prefix mode, prefixed, unprefixed
// slash and bare, plus the configuration's static slugs. No CPA or AISIX read.

const COMBINATIONS: ReadonlyArray<Partial<Record<MemberSource, true>>> = [
  { cpa: true },
  { aisix: true },
  { static: true },
  { cpa: true, aisix: true },
  { cpa: true, static: true },
  { aisix: true, static: true },
  { cpa: true, aisix: true, static: true },
];

export function dryRunCandidates(staticSlugs: readonly string[]): Candidate[] {
  const out: Candidate[] = [];
  let n = 0;
  for (const mode of ["prefix", "forced", "fallback"] as const) {
    for (const sources of COMBINATIONS) {
      for (const shape of ["prefixed", "slash", "bare"] as const) {
        n++;
        const id = shape === "prefixed" ? `dry/model-${n}` : shape === "slash" ? `vendor/model-${n}` : `model-${n}`;
        const cpa = sources.cpa === true && sources.static !== true;
        const native = cpa && (mode !== "prefix" || shape === "prefixed");
        out.push({ id, sources, mode, has_prefix: shape === "prefixed", ...(shape === "prefixed" ? { prefix: "dry" } : {}), native });
      }
    }
  }
  const seen = new Set(out.map((candidate) => candidate.id));
  for (const slug of staticSlugs) if (!seen.has(slug)) out.push({ id: slug, sources: { static: true }, mode: "prefix", has_prefix: false, native: false });
  return out;
}

/** Validates the script as a collection would; returns the rejected count of the synthetic set. */
export async function dryRun(script: MembershipScript, staticSlugs: readonly string[], options: RunOptions = {}): Promise<{ candidates: number; rejected: number }> {
  const candidates = dryRunCandidates(staticSlugs);
  const { decisions } = await runMembershipScript(script, candidates, { ...options, resident: false });
  return { candidates: candidates.length, rejected: decisions.filter((decision) => !decision.keep).length };
}
