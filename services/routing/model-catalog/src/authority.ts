// Authority snapshot: CPA native inventory, management discovery and identity, and the optional
// AISIX inventory, collected independently of catalog traffic and published as immutable generations.
import { createHash } from "node:crypto";
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
  /** Emit this CPA record exactly as-is: OAuth-only routes, or identity that could not be read. */
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
  /** AISIX IDs absent from the complete raw CPA inventory, in first-seen order with exact bytes. */
  readonly supplemental: readonly string[];
  readonly channels: readonly Channel[];
  readonly discoveryIncomplete: boolean;
  /** Admitted records kept as-is because identity data was unavailable. */
  readonly identityFallbackCount: number;
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
  supplemental: Object.freeze([]),
  channels: Object.freeze([]),
  discoveryIncomplete: true,
  identityFallbackCount: 0,
});

// ---------------------------------------------------------------------------
// Identity: management evidence only filters current members; it never adds public records.

export interface Identities {
  readonly qualified: ReadonlySet<string>;
  readonly unavailable: ReadonlySet<string>;
  readonly nativeOnly: ReadonlySet<string>;
  /** OAuth originals/aliases whose own ID carries a vendor namespace (e.g. devin/swe-2): unprefixed. */
  readonly bare: ReadonlySet<string>;
  readonly incomplete: boolean;
  readonly texts: readonly string[];
}

type AuthFile = { name: string; provider: string; disabled: boolean };

function wrappedField(text: string, field: string, allowNull = false): unknown {
  const wrapped = parseJSON(text);
  if (!isRecord(wrapped) || !(field in wrapped) || (wrapped[field] === null && !allowNull)) {
    throw new InvalidResponseError("missing identity response field");
  }
  return wrapped[field];
}

function decodeAuthFiles(text: string): AuthFile[] {
  const files = wrappedField(text, "files");
  if (!Array.isArray(files)) throw new InvalidResponseError("invalid auth identity");
  return files.map((file) => {
    if (!isRecord(file) || typeof file.name !== "string" || typeof file.provider !== "string" || !file.name || !file.provider) {
      throw new InvalidResponseError("invalid auth identity");
    }
    return { name: file.name, provider: file.provider, disabled: file.disabled === true };
  });
}

function decodeModelIDs(text: string): string[] {
  const models = wrappedField(text, "models");
  if (!Array.isArray(models)) throw new InvalidResponseError("invalid model identity");
  return models.map((model) => {
    if (!isRecord(model) || typeof model.id !== "string" || model.id === "") throw new InvalidResponseError("invalid model identity");
    return model.id;
  });
}

function decodeAliases(text: string): Map<string, Array<{ name: string; alias: string }>> {
  const value = wrappedField(text, "oauth-model-alias", true);
  const out = new Map<string, Array<{ name: string; alias: string }>>();
  if (value === null) return out;
  if (!isRecord(value)) throw new InvalidResponseError("invalid alias identity");
  for (const [provider, entries] of Object.entries(value)) {
    if (entries === null) continue;
    if (!Array.isArray(entries)) throw new InvalidResponseError("invalid alias identity");
    out.set(
      provider,
      entries.map((entry) => {
        if (!isRecord(entry) || typeof entry.name !== "string" || typeof entry.alias !== "string" || !entry.name || !entry.alias) {
          throw new InvalidResponseError("invalid alias identity");
        }
        return { name: entry.name, alias: entry.alias };
      }),
    );
  }
  return out;
}

export async function readIdentities(cpa: CpaClient, discovery: Discovery, options: ReadOptions): Promise<Identities> {
  const qualified = new Set<string>();
  const unavailable = new Set<string>();
  const nativeOnly = new Set<string>();
  const bare = new Set<string>();
  const texts: string[] = [];
  let incomplete = discovery.incomplete;
  for (const channel of discovery.channels) {
    if (!channel.prefix) continue;
    for (const id of channel.models) qualified.add(`${channel.prefix}/${id}`);
  }
  const fail = (step: string) => cpa.log.warn("catalog identity read failed", { step });
  let files: AuthFile[];
  try {
    const result = await cpa.managementRead("/v0/management/auth-files", decodeAuthFiles, options);
    files = result.value;
    texts.push(result.text);
  } catch {
    fail("auth list");
    return { qualified, unavailable, nativeOnly, bare, incomplete: true, texts };
  }
  if (files.length === 0) return { qualified, unavailable, nativeOnly, bare, incomplete, texts };

  let aliases: Map<string, Array<{ name: string; alias: string }>> | undefined;
  try {
    const result = await cpa.managementRead("/v0/management/oauth-model-alias", decodeAliases, options);
    aliases = result.value;
    texts.push(result.text);
  } catch {
    texts.push("");
  }
  const active = files.filter((file) => !file.disabled);
  const perFile = await Promise.all(
    active.map(async (file) => {
      let registered: JsonResult<string[]>;
      try {
        registered = await cpa.managementRead(`/v0/management/auth-files/models?name=${encodeURIComponent(file.name)}`, decodeModelIDs, options);
      } catch {
        return { file, registered: undefined, definitions: undefined };
      }
      let definitions: JsonResult<string[]> | undefined;
      if (aliases) {
        try {
          definitions = await cpa.managementRead(`/v0/management/model-definitions/${encodeURIComponent(file.provider)}`, decodeModelIDs, options);
        } catch {
          definitions = undefined;
        }
      }
      return { file, registered, definitions };
    }),
  );
  for (const { file, registered, definitions } of perFile) {
    if (!registered) {
      // Without the account's registration set, overlaps with other channels cannot be excluded.
      incomplete = true;
      fail("auth models");
      texts.push("");
      continue;
    }
    texts.push(registered.text, definitions?.text ?? "");
    if (!aliases || !definitions) {
      for (const id of registered.value) unavailable.add(id);
      fail("OAuth definitions or aliases");
      continue;
    }
    const originals = new Set(definitions.value);
    const aliasIDs = new Set<string>();
    for (const alias of aliases.get(file.provider) ?? []) if (originals.has(alias.name)) aliasIDs.add(alias.alias);
    for (const id of registered.value) {
      // Originals and bare aliases are not routes; a namespaced original is an unprefixed model.
      if (originals.has(id) || aliasIDs.has(id)) {
        if (id.includes("/")) bare.add(id);
        continue;
      }
      // A route must strip to this provider's original or alias at some `/` boundary.
      for (let index = id.indexOf("/", 1); index > 0; index = id.indexOf("/", index + 1)) {
        const name = id.slice(index + 1);
        if (originals.has(name) || aliasIDs.has(name)) {
          if (!qualified.has(id)) nativeOnly.add(id);
          qualified.add(id);
          break;
        }
      }
    }
  }
  return { qualified, unavailable, nativeOnly, bare, incomplete, texts };
}

/**
 * Filters native rows by identity. Unprefixed models are always admitted for chain enrichment
 * (former bare_models_takeover); prefixed ones need a positive identity match, except that read
 * failures keep the existing CPA record unchanged instead of dropping it.
 */
/** Parses the generation's native rows; callers drop them when their build ends. */
export function nativeRows(generation: AuthorityGeneration): Row[] {
  return generation.nativeText === "" ? [] : decodeNativeManifest(generation.nativeText);
}

export function admitModels(rows: readonly Row[], ids: Identities, staticSlugs: ReadonlySet<string>): { admitted: AdmittedModel[]; fallbackCount: number } {
  const admitted: AdmittedModel[] = [];
  const seen = new Set<string>();
  let fallbackCount = 0;
  for (const [index, row] of rows.entries()) {
    const id = exactModelID(row);
    if (seen.has(id)) continue;
    if ((!id.includes("/") || ids.bare.has(id)) && !ids.qualified.has(id)) {
      admitted.push({ id, index, preserveNative: false });
    } else if (ids.qualified.has(id)) {
      admitted.push({ id, index, preserveNative: ids.nativeOnly.has(id) && !staticSlugs.has(id) });
    } else if (ids.incomplete || ids.unavailable.has(id)) {
      fallbackCount++;
      admitted.push({ id, index, preserveNative: !staticSlugs.has(id) });
    } else {
      continue;
    }
    seen.add(id);
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

/** unique(AISIX) minus the complete raw CPA inventory; case-sensitive, first-seen order. */
export function supplementalIDs(aisixIDs: readonly string[], rawCpaIDs: ReadonlySet<string>): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const id of aisixIDs) {
    if (id.trim() === "" || rawCpaIDs.has(id) || seen.has(id)) continue;
    seen.add(id);
    out.push(id);
  }
  return out;
}

// ---------------------------------------------------------------------------
// Owner: collection, change detection and atomic publication.

export interface AuthorityOwnerOptions {
  cpa: CpaClient;
  aisix?: AisixClient;
  staticSlugs: ReadonlySet<string>;
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
    let admitted: AdmittedModel[] = [];
    let channels: readonly Channel[] = [];
    let discoveryIncomplete = true;
    let identityFallbackCount = 0;
    const rawCpaIDs = new Set<string>();
    const cpaOptions = phase(this.options.cpaTimeoutMs);
    try {
      const native = await cpa.nativeManifest(cpaOptions);
      part(native.text);
      nativeText = native.text;
      for (const row of native.value) rawCpaIDs.add(exactModelID(row));
      cpaState = rawCpaIDs.size > 0 ? "usable" : "empty";
      const discovery = await cpa.discover(cpaOptions);
      discovery.texts.forEach(part);
      const ids = await readIdentities(cpa, discovery, cpaOptions);
      ids.texts.forEach(part);
      ({ admitted, fallbackCount: identityFallbackCount } = admitModels(native.value, ids, staticSlugs));
      channels = discovery.channels;
      discoveryIncomplete = discovery.incomplete;
      if (discovery.incomplete) log.warn("channel identity discovery incomplete");
      if (identityFallbackCount > 0) log.warn("catalog identity unavailable; preserving CPA records", { count: identityFallbackCount });
    } catch (error) {
      errors.push(`CPA catalog: ${errorMessage(error)}`);
      part("!cpa-failed");
    }

    // Optional AISIX phase: isolated budget; failure never touches CPA output.
    let aisixState: AuthorityGeneration["aisix"] = "disabled";
    let supplemental: string[] = [];
    if (aisix) {
      try {
        const result = await aisix.fetchModelIDs(phase(this.options.aisixTimeoutMs));
        part(result.text);
        aisixState = result.value.length > 0 ? "usable" : "empty";
        if (cpaState !== "failed") supplemental = supplementalIDs(result.value, rawCpaIDs);
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
      supplemental: Object.freeze(supplemental),
      channels: Object.freeze(channels),
      discoveryIncomplete,
      identityFallbackCount,
    });
    this.#current = next;
    this.#fingerprint = fingerprint;
    return { generation: next, published: true, errors, staleInputs };
  }
}
