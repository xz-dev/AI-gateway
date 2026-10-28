// Authority snapshot: CPA native inventory, management discovery of CPA prefixes and the prefix
// mode, and the optional AISIX inventory, collected independently of catalog traffic and
// published as immutable generations.
import { createHash } from "node:crypto";
import type { SyncRules } from "./config.ts";
import { selectModels } from "./core.ts";
import { CpaClient, decodeNativeManifest, type Channel, type Discovery } from "./cpa.ts";
import { HttpPool, InvalidResponseError } from "./http.ts";
import { exactModelID, isRecord, parseJSON, type Row } from "./json.ts";
import type { Logger } from "./log.ts";
import { errorMessage } from "./log.ts";
import { readJSON, type JsonResult, type ReadCache, type ReadOptions } from "./read-cache.ts";

export type Availability = "usable" | "empty" | "failed";

export interface AdmittedModel {
  readonly id: string;
  /** Position of the CPA native manifest row (the `@self` record) in `nativeRows(generation)`. */
  readonly index: number;
  /** Emit this CPA record exactly as-is: OAuth routes under an inferred prefix, or prefix-discovery fallback. */
  readonly preserveNative: boolean;
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
  /** Admitted CPA models in native manifest order; first occurrence wins for duplicate IDs. */
  readonly admitted: readonly AdmittedModel[];
  /** Every unique AISIX ID, in first-seen order with exact bytes. */
  readonly aisixIDs: readonly string[];
  readonly channels: readonly Channel[];
  readonly discoveryIncomplete: boolean;
  /** How CPA IDs were admitted: CPA prefix rule, forced-prefix compatibility, or fallback. */
  readonly prefixMode: PrefixMode;
  /** Admitted records kept as-is because prefix discovery was incomplete. */
  readonly prefixFallbackCount: number;
}

export interface RefreshOutcome {
  readonly generation: AuthorityGeneration;
  /** A new generation was published by this attempt. */
  readonly published: boolean;
  readonly errors: readonly string[];
  /** Inputs answered from a retained response instead of a fresh read. */
  readonly staleInputs: number;
}

const EMPTY: AuthorityGeneration = Object.freeze({
  generation: 0,
  cpa: "failed",
  aisix: "failed",
  nativeText: "",
  admitted: Object.freeze([]),
  aisixIDs: Object.freeze([]),
  channels: Object.freeze([]),
  discoveryIncomplete: true,
  prefixMode: "fallback",
  prefixFallbackCount: 0,
});

// ---------------------------------------------------------------------------
// Membership (D1, D2, D4): CPA prefixes and the prefix mode come from management data only.

export type PrefixMode = "prefix" | "forced" | "fallback";

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

function hasPrefix(id: string, prefixes: ReadonlySet<string>): boolean {
  for (let index = id.indexOf("/"); index > 0; index = id.indexOf("/", index + 1)) if (prefixes.has(id.slice(0, index))) return true;
  return false;
}

/**
 * The D1 CPA admission rule. Prefix mode admits IDs under a CPA prefix; forced and fallback modes
 * admit every slash ID (CPA lists no bare copies, or prefixes are unknown). `cpaInclude` admits
 * the rest. Fallback rows and OAuth registrations under an inferred prefix are emitted as-is.
 */
export function admitModels(
  rows: readonly Row[],
  prefixes: Prefixes,
  cpaInclude: SyncRules | undefined,
  staticSlugs: ReadonlySet<string>,
): { admitted: AdmittedModel[]; fallbackCount: number } {
  const ids = rows.map(exactModelID);
  const prefixed = (id: string) => (prefixes.mode === "prefix" ? hasPrefix(id, prefixes.prefixes) : id.includes("/"));
  const included = cpaInclude ? new Set(selectModels({ includeModels: cpaInclude, warnings: [] }, ids).kept) : new Set<string>();
  const admitted: AdmittedModel[] = [];
  const seen = new Set<string>();
  let fallbackCount = 0;
  for (const [index, id] of ids.entries()) {
    if (seen.has(id) || !(prefixed(id) || included.has(id))) continue;
    seen.add(id);
    let preserveNative: boolean;
    if (prefixes.mode === "fallback") {
      fallbackCount++;
      preserveNative = true;
    } else {
      // Prefix mode: OAuth IDs carry an inferred prefix, so cpa_include never applies to them.
      preserveNative = prefixes.oauth.has(id) && !(prefixes.mode === "forced" && included.has(id));
    }
    admitted.push({ id, index, preserveNative: preserveNative && !staticSlugs.has(id) });
  }
  return { admitted, fallbackCount };
}

/** Runtime conflicts that make the configured catalog ambiguous. */
export function runtimeConflicts(channels: readonly Channel[], customPoolKeys: Iterable<string>, admitted: readonly AdmittedModel[]): string[] {
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
  for (const model of admitted) {
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
  staticSlugs: ReadonlySet<string>;
  /** `membership.cpa_include`: admits CPA IDs without a CPA prefix. */
  cpaInclude?: SyncRules;
  /** Budget for the mandatory CPA phase. */
  cpaTimeoutMs: number;
  /** Independent budget for the optional AISIX phase. */
  aisixTimeoutMs: number;
  log: Logger;
}

export class AuthorityOwner {
  #current: AuthorityGeneration = EMPTY;
  #fingerprint = "";
  #active: { promise: Promise<RefreshOutcome>; bypass: boolean } | undefined;

  readonly options: AuthorityOwnerOptions;

  constructor(options: AuthorityOwnerOptions) {
    this.options = options;
  }

  current(): AuthorityGeneration {
    return this.#current;
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
    const { cpa, aisix, staticSlugs, cpaInclude, log } = this.options;
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
    let admitted: AdmittedModel[] = [];
    let channels: readonly Channel[] = [];
    let discoveryIncomplete = true;
    let prefixMode: PrefixMode = "fallback";
    let prefixFallbackCount = 0;
    const cpaOptions = phase(this.options.cpaTimeoutMs);
    try {
      const native = await cpa.nativeManifest(cpaOptions);
      part(native.text);
      nativeText = native.text;
      cpaState = native.value.length > 0 ? "usable" : "empty";
      const discovery = await cpa.discover(cpaOptions);
      discovery.texts.forEach(part);
      const prefixes = await readPrefixes(cpa, discovery, cpaOptions);
      prefixes.texts.forEach(part);
      part(`!mode:${prefixes.mode}`);
      ({ admitted, fallbackCount: prefixFallbackCount } = admitModels(native.value, prefixes, cpaInclude, staticSlugs));
      channels = discovery.channels;
      discoveryIncomplete = discovery.incomplete;
      prefixMode = prefixes.mode;
      if (discovery.incomplete) log.warn("channel discovery incomplete");
      if (prefixFallbackCount > 0) log.warn("CPA prefixes unavailable; preserving CPA slash records", { count: prefixFallbackCount });
    } catch (error) {
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

    const fingerprint = hash.digest("hex");
    const previous = this.#current;
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
      admitted: Object.freeze(admitted),
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
