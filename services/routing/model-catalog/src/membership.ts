// Membership script host (design D1, D2, D6–D9): reads the profile's `catalog.lua`, runs it in a
// short-lived worker per authority collection, enforces the wall-time bound from this thread and
// turns the per-candidate decisions into members and rejected candidates.
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
  scriptBytes: 256 << 10,
  workerHeapMb: 32,
});
export type Limits = typeof LIMITS;

export interface WorkerRequest {
  readonly script: string;
  readonly candidates: readonly Candidate[];
  readonly wasmPath: string;
  readonly limits: Limits;
  /** Count-hook granularity in VM instructions. */
  readonly hookStep: number;
}

export interface InstructionUse {
  readonly load: number;
  readonly maxCall: number;
  readonly total: number;
}

export type WorkerReply =
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
}

export interface ScriptRun {
  readonly decisions: readonly Decision[];
  readonly instructions: InstructionUse;
}

/** One worker, one Lua state, one call per candidate; terminated at the wall-time bound. */
export function runMembershipScript(script: MembershipScript, candidates: readonly Candidate[], options: RunOptions = {}): Promise<ScriptRun> {
  const limits = options.limits ?? LIMITS;
  const request: WorkerRequest = { script: script.text, candidates, wasmPath: WASM_PATH, limits, hookStep: options.hookStep ?? 1_000 };
  return new Promise((resolve, reject) => {
    const worker = new Worker(options.workerFile ?? MEMBERSHIP_WORKER_FILE, { workerData: request, resourceLimits: { maxOldGenerationSizeMb: limits.workerHeapMb } });
    let settled = false;
    const settle = (result: ScriptRun | MembershipScriptError) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      void worker.terminate();
      if (result instanceof MembershipScriptError) reject(result);
      else resolve(result);
    };
    const timer = setTimeout(() => settle(new MembershipScriptError("wall_time", `collection exceeded ${limits.wallMs} ms`)), limits.wallMs);
    worker.once("message", (reply: WorkerReply) => {
      if (reply.ok) settle({ decisions: reply.decisions, instructions: reply.instructions });
      else settle(new MembershipScriptError(reply.errorClass, reply.message, reply.candidate));
    });
    worker.once("error", (error: Error) => settle(new MembershipScriptError("worker_exit", error.message.slice(0, 300))));
    worker.once("exit", (code) => settle(new MembershipScriptError("worker_exit", `worker exited with code ${code}`)));
  });
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
  const { decisions } = await runMembershipScript(script, candidates, options);
  return { candidates: candidates.length, rejected: decisions.filter((decision) => !decision.keep).length };
}
