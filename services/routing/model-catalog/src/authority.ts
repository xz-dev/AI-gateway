// Authority snapshot: CPA native inventory, management discovery of CPA prefixes and the prefix
// mode, and the optional AISIX inventory, collected independently of catalog traffic and
// published as immutable generations.
import { createHash } from "node:crypto";
import { CpaClient, decodeNativeManifest, type Channel, type Discovery } from "./cpa.ts";
import { HttpPool, InvalidResponseError } from "./http.ts";
import { exactModelID, isRecord, parseJSON, type Row } from "./json.ts";
import type { Logger } from "./log.ts";
import { errorMessage } from "./log.ts";
import {
  MembershipScriptError,
  REJECTED_DEFAULT_REASON,
  runMembershipScript,
  type Candidate,
  type Decision,
  type MembershipScript,
  type MemberSource,
  type PrefixMode,
  type RunOptions,
} from "./membership.ts";
import { readJSON, type JsonResult, type ReadCache, type ReadOptions } from "./read-cache.ts";

export type Availability = "usable" | "empty" | "failed";

export type { PrefixMode } from "./membership.ts";

/** A member chosen by the membership script, in candidate order (output order is set by the build). */
export interface MemberEntry {
  readonly id: string;
  readonly origin: MemberSource;
  /** CPA members: position of the native manifest row (the `@self` record) in `nativeRows(generation)`. */
  readonly index?: number;
  /** Emit this CPA record as CPA returned it (only `priority` replaced): the native flag without `compile`. */
  readonly preserveNative: boolean;
}

/** A candidate the membership script rejected, shown on the models table only. */
export interface RejectedCandidate {
  readonly id: string;
  readonly sources: readonly MemberSource[];
  readonly reason: string;
}

export interface AuthorityGeneration {
  /** 0 until the first collection has been published. */
  readonly generation: number;
  readonly cpa: Availability;
  readonly aisix: Availability | "disabled";
  /**
   * The validated native manifest body. Rows are parsed per build (nativeRows) instead of staying
   * resident; the string is the one the authority read cache already holds.
   */
  readonly nativeText: string;
  /** Members decided by `catalog.lua`, in candidate order (CPA first occurrence, AISIX, static). */
  readonly members: readonly MemberEntry[];
  readonly rejected: readonly RejectedCandidate[];
  /** Every unique AISIX ID, in first-seen order with exact bytes. */
  readonly aisixIDs: readonly string[];
  readonly channels: readonly Channel[];
  readonly discoveryIncomplete: boolean;
  /** How CPA IDs were admitted: CPA prefix rule, forced-prefix compatibility, or fallback. */
  readonly prefixMode: PrefixMode;
  /** CPA members kept as-is because prefix discovery was incomplete. */
  readonly prefixFallbackCount: number;
}

export interface RefreshOutcome {
  readonly generation: AuthorityGeneration;
  /** A new generation was published by this attempt. */
  readonly published: boolean;
  readonly errors: readonly string[];
  /** The membership script failed; nothing was published and the previous generation keeps serving. */
  readonly scriptError?: MembershipScriptError;
  /** Inputs answered from a retained response instead of a fresh read. */
  readonly staleInputs: number;
}

const EMPTY: AuthorityGeneration = Object.freeze({
  generation: 0,
  cpa: "failed",
  aisix: "failed",
  nativeText: "",
  members: Object.freeze([]),
  rejected: Object.freeze([]),
  aisixIDs: Object.freeze([]),
  channels: Object.freeze([]),
  discoveryIncomplete: true,
  prefixMode: "fallback",
  prefixFallbackCount: 0,
});

// ---------------------------------------------------------------------------
// Membership inputs: CPA prefixes and the prefix mode come from management data only.

export interface Prefixes {
  /** `fallback` when a management read failed with no eligible cached response. */
  readonly mode: PrefixMode;
  /** Key-channel prefixes plus OAuth prefixes inferred from `P/x` + `x` registrations. */
  readonly prefixes: ReadonlySet<string>;
  /** IDs an OAuth account registers under an inferred prefix (forced mode: every OAuth registration). */
  readonly oauth: ReadonlySet<string>;
  readonly texts: readonly string[];
}

type AuthFile = { name: string; disabled: boolean };

function wrappedField(text: string, field: string): unknown {
  const wrapped = parseJSON(text);
  if (!isRecord(wrapped) || !(field in wrapped) || wrapped[field] === null) throw new InvalidResponseError("missing management response field");
  return wrapped[field];
}

function decodeAuthFiles(text: string): AuthFile[] {
  const files = wrappedField(text, "files");
  if (!Array.isArray(files)) throw new InvalidResponseError("invalid auth file list");
  return files.map((file) => {
    if (!isRecord(file) || typeof file.name !== "string" || !file.name) throw new InvalidResponseError("invalid auth file list");
    return { name: file.name, disabled: file.disabled === true };
  });
}

function decodeModelIDs(text: string): string[] {
  const models = wrappedField(text, "models");
  if (!Array.isArray(models)) throw new InvalidResponseError("invalid registered models");
  return models.map((model) => {
    if (!isRecord(model) || typeof model.id !== "string" || model.id === "") throw new InvalidResponseError("invalid registered models");
    return model.id;
  });
}

function decodeForceModelPrefix(text: string): boolean {
  const value = wrappedField(text, "force-model-prefix");
  if (typeof value !== "boolean") throw new InvalidResponseError("invalid force-model-prefix");
  return value;
}

/** First segments `P` with both `P/x` and `x` registered: CPA's default `applyModelPrefixes` shape. */
export function inferredPrefixes(registered: readonly string[]): Set<string> {
  const ids = new Set(registered);
  const out = new Set<string>();
  for (const id of ids) {
    const slash = id.indexOf("/");
    if (slash > 0 && ids.has(id.slice(slash + 1))) out.add(id.slice(0, slash));
  }
  return out;
}

export async function readPrefixes(cpa: CpaClient, discovery: Discovery, options: ReadOptions): Promise<Prefixes> {
  const prefixes = new Set<string>();
  const oauth = new Set<string>();
  const texts: string[] = [];
  for (const channel of discovery.channels) if (channel.prefix) prefixes.add(channel.prefix);
  const fallback = (step: string): Prefixes => {
    cpa.log.warn("CPA prefix discovery failed; admitting slash IDs as CPA returned them", { step });
    return { mode: "fallback", prefixes, oauth, texts };
  };
  let forced: boolean;
  try {
    const result = await cpa.managementRead("/v0/management/force-model-prefix", decodeForceModelPrefix, options);
    forced = result.value;
    texts.push(result.text);
  } catch {
    return fallback("force-model-prefix");
  }
  let files: AuthFile[];
  try {
    const result = await cpa.managementRead("/v0/management/auth-files", decodeAuthFiles, options);
    files = result.value;
    texts.push(result.text);
  } catch {
    return fallback("auth list");
  }
  const registered = await Promise.all(
    files
      .filter((file) => !file.disabled)
      .map((file) => cpa.managementRead(`/v0/management/auth-files/models?name=${encodeURIComponent(file.name)}`, decodeModelIDs, options).catch(() => undefined)),
  );
  let failed = false;
  for (const result of registered) {
    if (!result) {
      failed = true;
      continue;
    }
    texts.push(result.text);
    if (forced) {
      for (const id of result.value) oauth.add(id);
      continue;
    }
    const own = inferredPrefixes(result.value);
    for (const prefix of own) prefixes.add(prefix);
    for (const id of result.value) {
      const slash = id.indexOf("/");
      if (slash > 0 && own.has(id.slice(0, slash))) oauth.add(id);
    }
  }
  if (failed) return fallback("auth models");
  if (discovery.incomplete) return fallback("channel discovery");
  return { mode: forced ? "forced" : "prefix", prefixes, oauth, texts };
}

/** Parses the generation's native rows; callers drop them when their build ends. */
export function nativeRows(generation: AuthorityGeneration): Row[] {
  return generation.nativeText === "" ? [] : decodeNativeManifest(generation.nativeText);
}

/** The shortest known CPA prefix `P` such that `id` starts with `P/` at a `/` boundary. */
function shortestPrefix(id: string, prefixes: ReadonlySet<string>): string | undefined {
  for (let index = id.indexOf("/"); index > 0; index = id.indexOf("/", index + 1)) if (prefixes.has(id.slice(0, index))) return id.slice(0, index);
  return undefined;
}

export interface MembershipInput {
  readonly candidates: readonly Candidate[];
  /** Native manifest row index of each CPA candidate, by candidate position. */
  readonly rowIndex: ReadonlyArray<number | undefined>;
}

/**
 * Every unique candidate in candidate order: CPA native IDs (first occurrence), then AISIX, then
 * static slugs. The native flag: OAuth registrations under an inferred prefix (forced mode: every
 * OAuth registration; fallback: every CPA row), never for static slugs.
 */
export function membershipCandidates(rows: readonly Row[], prefixes: Prefixes, aisixIDs: readonly string[], staticSlugs: readonly string[]): MembershipInput {
  const index = new Map<string, { sources: Partial<Record<MemberSource, true>>; row?: number }>();
  for (const [row, value] of rows.entries()) {
    const id = exactModelID(value);
    if (!index.has(id)) index.set(id, { sources: { cpa: true }, row });
  }
  const add = (id: string, source: MemberSource) => {
    const entry = index.get(id);
    if (entry) entry.sources[source] = true;
    else index.set(id, { sources: { [source]: true } });
  };
  for (const id of aisixIDs) add(id, "aisix");
  for (const slug of staticSlugs) add(slug, "static");
  const candidates: Candidate[] = [];
  const rowIndex: Array<number | undefined> = [];
  for (const [id, { sources, row }] of index) {
    const prefix = shortestPrefix(id, prefixes.prefixes);
    const native = sources.cpa === true && sources.static !== true && (prefixes.mode === "fallback" || prefixes.oauth.has(id));
    candidates.push({ id, sources, mode: prefixes.mode, has_prefix: prefix !== undefined, ...(prefix === undefined ? {} : { prefix }), native });
    rowIndex.push(row);
  }
  return { candidates, rowIndex };
}

const SOURCE_ORDER: readonly MemberSource[] = ["cpa", "aisix", "static"];

/** Members and rejected candidates from the script's decisions (one per candidate). */
export function applyDecisions(input: MembershipInput, decisions: readonly Decision[]): { members: MemberEntry[]; rejected: RejectedCandidate[] } {
  const members: MemberEntry[] = [];
  const rejected: RejectedCandidate[] = [];
  for (const [position, candidate] of input.candidates.entries()) {
    const decision = decisions[position]!;
    const sources = SOURCE_ORDER.filter((source) => candidate.sources[source]);
    if (!decision.keep) {
      rejected.push({ id: candidate.id, sources, reason: decision.reason ?? REJECTED_DEFAULT_REASON });
      continue;
    }
    const origin = decision.source ?? sources[0]!;
    if (origin === "cpa") members.push({ id: candidate.id, origin, index: input.rowIndex[position], preserveNative: candidate.native && !decision.compile });
    else members.push({ id: candidate.id, origin, preserveNative: false });
  }
  return { members, rejected };
}

/** Runtime conflicts that make the configured catalog ambiguous. */
export function runtimeConflicts(channels: readonly Channel[], customPoolKeys: Iterable<string>, members: readonly MemberEntry[]): string[] {
  const conflicts: string[] = [];
  const prefixes = new Map<string, string>();
  for (const channel of channels) {
    if (!channel.prefix) {
      conflicts.push(`channel (type=${channel.type} name=${JSON.stringify(channel.name)}) has empty prefix`);
      continue;
    }
    const previous = prefixes.get(channel.prefix);
    if (previous) conflicts.push(`duplicate channel prefix ${JSON.stringify(channel.prefix)} (types ${JSON.stringify(previous)} and ${JSON.stringify(channel.type)})`);
    else prefixes.set(channel.prefix, channel.type);
  }
  if (conflicts.length > 0) return conflicts;
  const nativePrefixes = new Set<string>();
  for (const model of members) {
    if (model.origin !== "cpa") continue;
    const slash = model.id.indexOf("/");
    if (slash > 0) nativePrefixes.add(model.id.slice(0, slash));
  }
  for (const key of customPoolKeys) {
    if (prefixes.has(key)) conflicts.push(`custom channel ${JSON.stringify(key)} conflicts with CPA channel prefix`);
    if (nativePrefixes.has(key)) conflicts.push(`custom channel ${JSON.stringify(key)} conflicts with native manifest provider prefix`);
  }
  return conflicts;
}

// ---------------------------------------------------------------------------
// AISIX: optional router inventory read from its models API only.

export const AISIX_READ_LIMIT = 16 << 20;
const BROWSER_UA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36";

/** Requires a data array (may be empty) whose IDs are non-blank strings; IDs keep their exact bytes. */
export function decodeAISIXModels(text: string): string[] {
  const body = parseJSON(text);
  const data = isRecord(body) ? body.data : undefined;
  if (!Array.isArray(data)) throw new InvalidResponseError("missing data array in aisix models response");
  return data.map((item) => {
    const id = isRecord(item) ? item.id : undefined;
    if (typeof id !== "string" || id.trim() === "") throw new InvalidResponseError("empty or blank model id in aisix models response");
    return id;
  });
}

export class AisixClient {
  readonly endpoint: string;
  readonly #token: string;
  readonly pool: HttpPool;
  readonly cache: ReadCache | undefined;

  constructor(endpoint: string, token: string, pool: HttpPool, cache: ReadCache | undefined) {
    this.endpoint = endpoint;
    this.#token = token.trim();
    this.pool = pool;
    this.cache = cache;
  }

  fetchModelIDs(options?: ReadOptions): Promise<JsonResult<string[]>> {
    const headers: Record<string, string> = { "User-Agent": BROWSER_UA };
    if (this.#token) headers.Authorization = `Bearer ${this.#token}`;
    return readJSON(this.pool, this.cache, { request: { url: this.endpoint, headers, limit: AISIX_READ_LIMIT }, policy: "authority", decode: decodeAISIXModels }, options);
  }
}

/** Exact-byte, case-sensitive dedup in first-seen order. */
export function uniqueIDs(ids: readonly string[]): string[] {
  return [...new Set(ids)];
}

// ---------------------------------------------------------------------------
// Owner: collection, change detection and atomic publication.

export interface AuthorityOwnerOptions {
  cpa: CpaClient;
  aisix?: AisixClient;
  /** Static model slugs in configuration order. */
  staticSlugs: readonly string[];
  /** The profile's `catalog.lua`, loaded and dry-run at startup. */
  script: MembershipScript;
  /** Worker file and bounds overrides (tests). */
  membership?: RunOptions;
  clock?: () => number;
  /** Budget for the mandatory CPA phase. */
  cpaTimeoutMs: number;
  /** Independent budget for the optional AISIX phase. */
  aisixTimeoutMs: number;
  log: Logger;
}

export class AuthorityOwner {
  #current: AuthorityGeneration = EMPTY;
  /** Inputs fingerprint plus the decisions digest of the current generation. */
  #fingerprint = "";
  /** Inputs fingerprint and decisions of the last successful script run. */
  #inputs = "";
  #decided: { members: MemberEntry[]; rejected: RejectedCandidate[]; digest: string } | undefined;
  #scriptFailure: { at: number; error: MembershipScriptError } | undefined;
  #active: { promise: Promise<RefreshOutcome>; bypass: boolean } | undefined;

  readonly options: AuthorityOwnerOptions;

  constructor(options: AuthorityOwnerOptions) {
    this.options = options;
  }

  current(): AuthorityGeneration {
    return this.#current;
  }

  /** The latest membership script failure, until a later run succeeds. */
  scriptFailure(): { at: number; error: MembershipScriptError } | undefined {
    return this.#scriptFailure;
  }

  /** Coalesced: callers join an active attempt unless they need a forced one and it is not. */
  refresh(options: { bypass?: boolean; signal?: AbortSignal } = {}): Promise<RefreshOutcome> {
    const bypass = options.bypass === true;
    const active = this.#active;
    if (active && (active.bypass || !bypass)) return active.promise;
    const promise = (active?.promise ?? Promise.resolve())
      .catch(() => undefined)
      .then(() => this.#collect(bypass, options.signal))
      .finally(() => {
        if (this.#active?.promise === promise) this.#active = undefined;
      });
    this.#active = { promise, bypass };
    return promise;
  }

  async #collect(bypass: boolean, signal: AbortSignal | undefined): Promise<RefreshOutcome> {
    const { cpa, aisix, staticSlugs, log } = this.options;
    const errors: string[] = [];
    let staleInputs = 0;
    const phase = (ms: number): ReadOptions => ({
      bypass,
      signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(ms)]) : AbortSignal.timeout(ms),
      onStale: () => staleInputs++,
    });
    const hash = createHash("sha256");
    const part = (text: string) => hash.update(String(text.length)).update(":").update(text);

    // Mandatory CPA phase.
    let cpaState: Availability = "failed";
    let nativeText = "";
    let rows: readonly Row[] = [];
    let prefixes: Prefixes | undefined;
    let channels: readonly Channel[] = [];
    let discoveryIncomplete = true;
    const cpaOptions = phase(this.options.cpaTimeoutMs);
    try {
      const native = await cpa.nativeManifest(cpaOptions);
      part(native.text);
      nativeText = native.text;
      rows = native.value;
      cpaState = native.value.length > 0 ? "usable" : "empty";
      const discovery = await cpa.discover(cpaOptions);
      discovery.texts.forEach(part);
      prefixes = await readPrefixes(cpa, discovery, cpaOptions);
      prefixes.texts.forEach(part);
      part(`!mode:${prefixes.mode}`);
      channels = discovery.channels;
      discoveryIncomplete = discovery.incomplete;
      if (discovery.incomplete) log.warn("channel discovery incomplete");
    } catch (error) {
      cpaState = "failed";
      nativeText = "";
      rows = [];
      prefixes = undefined;
      errors.push(`CPA catalog: ${errorMessage(error)}`);
      part("!cpa-failed");
    }

    // Optional AISIX phase: isolated budget; failure never touches CPA output.
    let aisixState: AuthorityGeneration["aisix"] = "disabled";
    let aisixIDs: string[] = [];
    if (aisix) {
      try {
        const result = await aisix.fetchModelIDs(phase(this.options.aisixTimeoutMs));
        part(result.text);
        aisixState = result.value.length > 0 ? "usable" : "empty";
        aisixIDs = uniqueIDs(result.value);
      } catch (error) {
        aisixState = "failed";
        errors.push(`AISIX catalog: ${errorMessage(error)}`);
        part("!aisix-failed");
      }
    }

    // Membership stage (design D4): the script decides every candidate. Unchanged inputs reuse the
    // last decisions; a forced refresh runs the script again.
    const inputs = hash.digest("hex");
    const previous = this.#current;
    let members: MemberEntry[] = [];
    let rejected: RejectedCandidate[] = [];
    let digest = "!no-membership";
    if (prefixes) {
      if (!bypass && this.#decided && inputs === this.#inputs) ({ members, rejected, digest } = this.#decided);
      else {
        const input = membershipCandidates(rows, prefixes, aisixIDs, staticSlugs);
        try {
          const run = await runMembershipScript(this.options.script, input.candidates, this.options.membership);
          ({ members, rejected } = applyDecisions(input, run.decisions));
        } catch (error) {
          const scriptError = error instanceof MembershipScriptError ? error : new MembershipScriptError("worker_exit", errorMessage(error));
          this.#scriptFailure = { at: (this.options.clock ?? Date.now)(), error: scriptError };
          log.warn("membership script failed; previous generation keeps serving", {
            class: scriptError.errorClass,
            ...(scriptError.candidate === undefined ? {} : { candidate: scriptError.candidate }),
            err: scriptError.message,
          });
          return { generation: previous, published: false, errors: [...errors, scriptError.message], staleInputs, scriptError };
        }
        this.#scriptFailure = undefined;
        digest = createHash("sha256").update(JSON.stringify([members, rejected])).digest("hex");
        this.#inputs = inputs;
        this.#decided = { members, rejected, digest };
        if (members.length === 0 && input.candidates.length > 0) log.info("catalog.lua kept no candidate; publishing an empty catalog", { candidates: input.candidates.length, rejected: rejected.length });
      }
    }
    const prefixMode: PrefixMode = prefixes?.mode ?? "fallback";
    const prefixFallbackCount = prefixMode === "fallback" ? members.filter((member) => member.origin === "cpa" && member.preserveNative).length : 0;
    if (prefixFallbackCount > 0) log.warn("CPA prefixes unavailable; preserving CPA slash records", { count: prefixFallbackCount });

    const fingerprint = `${inputs}:${digest}`;
    if (previous.generation !== 0 && fingerprint === this.#fingerprint) {
      // Same content: hold the read cache's current string so an equal old copy is not kept alive.
      if (nativeText !== "") this.#current = Object.freeze({ ...previous, nativeText });
      return { generation: this.#current, published: false, errors, staleInputs };
    }
    const next: AuthorityGeneration = Object.freeze({
      generation: previous.generation + 1,
      cpa: cpaState,
      aisix: aisixState,
      nativeText,
      members: Object.freeze(members),
      rejected: Object.freeze(rejected),
      aisixIDs: Object.freeze(aisixIDs),
      channels: Object.freeze(channels),
      discoveryIncomplete,
      prefixMode,
      prefixFallbackCount,
    });
    if (prefixMode !== previous.prefixMode || previous.generation === 0) {
      if (prefixMode === "forced") log.info("CPA forces model prefixes; catalog membership in compatibility mode");
      else if (prefixMode === "prefix") log.info("catalog membership follows the CPA prefix rule");
    }
    this.#current = next;
    this.#fingerprint = fingerprint;
    return { generation: next, published: true, errors, staleInputs };
  }
}
