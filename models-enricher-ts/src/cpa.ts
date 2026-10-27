// CPA client: native manifest, management reads and the management api-call proxy.
// Credentials travel only in request headers; errors and logs carry paths and status codes only.
import { InvalidResponseError, ReadStatusError, type HttpPool } from "./http.ts";
import { asString, exactModelID, isRecord, parseJSON, type Row } from "./json.ts";
import type { Logger } from "./log.ts";
import { errorMessage } from "./log.ts";
import { readJSON, type CachePolicy, type JsonResult, type ReadCache, type ReadOptions } from "./read-cache.ts";

/** The raw native fetch is pinned so CPA's legacy-client trimming never applies. */
export const CPA_CATALOG_CLIENT_VERSION = "1";

export const NATIVE_MANIFEST_LIMIT = 64 << 20;
export const MANAGEMENT_READ_LIMIT = 8 << 20;
export const API_CALL_LIMIT = 64 << 20;

export const CHANNEL_KINDS = [
  "openai-compatibility",
  "claude-api-key",
  "gemini-api-key",
  "codex-api-key",
  "xai-api-key",
  "vertex-api-key",
  "interactions-api-key",
] as const;

export type ChannelKind = (typeof CHANNEL_KINDS)[number];

export interface Channel {
  readonly type: ChannelKind;
  readonly name: string;
  readonly prefix: string;
  readonly baseUrl: string;
  readonly authIndex: string;
  /** Declared effective upstream names: alias when set, else name. */
  readonly models: readonly string[];
}

export interface Discovery {
  readonly channels: readonly Channel[];
  /** A kind failed to load or an entry lacked an auth index: identity coverage is not complete. */
  readonly incomplete: boolean;
  readonly texts: readonly string[];
}

export interface ApiCallRequest {
  method: "GET" | "POST";
  url: string;
  headers: Record<string, string>;
  data?: string;
}

export interface CpaClientOptions {
  baseUrl: string;
  managementKey: string;
  clientKey: string;
  pool: HttpPool;
  cache?: ReadCache;
  log: Logger;
}

export class CpaClient {
  readonly base: string;
  readonly #managementKey: string;
  readonly #clientKey: string;
  readonly pool: HttpPool;
  readonly cache: ReadCache | undefined;
  readonly log: Logger;

  constructor(options: CpaClientOptions) {
    this.base = options.baseUrl.replace(/\/+$/, "");
    this.#managementKey = options.managementKey;
    this.#clientKey = options.clientKey;
    this.pool = options.pool;
    this.cache = options.cache;
    this.log = options.log;
  }

  nativeManifest(options?: ReadOptions): Promise<JsonResult<Row[]>> {
    return readJSON(
      this.pool,
      this.cache,
      {
        request: {
          url: `${this.base}/v1/models?client_version=${CPA_CATALOG_CLIENT_VERSION}`,
          headers: { Authorization: `Bearer ${this.#clientKey}` },
          limit: NATIVE_MANIFEST_LIMIT,
        },
        policy: "authority",
        decode: decodeNativeManifest,
      },
      options,
    );
  }

  /** Authority read of a management endpoint (process memory cache only). */
  managementRead<T>(path: string, decode: (text: string) => T, options?: ReadOptions, limit = MANAGEMENT_READ_LIMIT): Promise<JsonResult<T>> {
    return readJSON(
      this.pool,
      this.cache,
      { request: { url: this.base + path, headers: { Authorization: `Bearer ${this.#managementKey}` }, limit }, policy: "authority", decode },
      options,
    );
  }

  async discover(options?: ReadOptions): Promise<Discovery> {
    const results = await Promise.all(
      CHANNEL_KINDS.map(async (kind) => {
        try {
          return await this.managementRead(`/v0/management/${kind}`, (text) => parseChannels(kind, text), options);
        } catch (error) {
          this.log.warn("channel list failed", { kind, err: errorMessage(error) });
          return undefined;
        }
      }),
    );
    const channels: Channel[] = [];
    const texts: string[] = [];
    let incomplete = false;
    for (const result of results) {
      if (!result) {
        incomplete = true;
        texts.push("");
        continue;
      }
      channels.push(...result.value.channels);
      incomplete ||= result.value.incomplete;
      texts.push(result.text);
    }
    return { channels, incomplete, texts };
  }

  /**
   * Forwards a channel request through POST /v0/management/api-call. CPA substitutes `$TOKEN$`
   * from the channel's auth index, so channel keys never leave CPA. Only read-only inner requests
   * (GET, or POST /api/show) are cached, as metadata inputs.
   */
  async apiCall<T>(channel: Channel, call: ApiCallRequest, decode: (inner: unknown) => T, options?: ReadOptions): Promise<T> {
    const payload: Row = { method: call.method, url: call.url, header: call.headers };
    if (call.data) payload.data = call.data;
    if (channel.authIndex) payload.auth_index = channel.authIndex;
    const result = await readJSON(
      this.pool,
      this.cache,
      {
        request: {
          url: `${this.base}/v0/management/api-call`,
          method: "POST",
          headers: { Authorization: `Bearer ${this.#managementKey}`, "Content-Type": "application/json" },
          body: JSON.stringify(payload),
          limit: API_CALL_LIMIT,
        },
        policy: apiCallPolicy(call),
        decode: (text) => decode(parseAPICall(text)),
      },
      options,
    );
    return result.value;
  }
}

function apiCallPolicy(call: ApiCallRequest): CachePolicy {
  if (call.method === "GET") return "metadata";
  let path = "";
  try {
    path = new URL(call.url).pathname;
  } catch {
    // unparsable URL: never cache
  }
  return call.method === "POST" && path === "/api/show" ? "metadata" : "none";
}

/** Unwraps {status_code, body}; a non-2xx inner status is a failure, and the inner body must be JSON. */
export function parseAPICall(text: string): unknown {
  const outer = parseJSON(text);
  if (!isRecord(outer)) throw new InvalidResponseError("invalid api-call envelope");
  const status = typeof outer.status_code === "number" ? outer.status_code : 0;
  if (status < 200 || status >= 300) throw new ReadStatusError(status);
  const body = outer.body;
  if (typeof body === "string") {
    try {
      return parseJSON(body);
    } catch {
      throw new InvalidResponseError("invalid api-call body JSON");
    }
  }
  if (body === undefined) throw new InvalidResponseError("invalid api-call body JSON");
  return body;
}

export function decodeNativeManifest(text: string): Row[] {
  const envelope = parseJSON(text);
  const models = isRecord(envelope) ? envelope.models : undefined;
  if (!Array.isArray(models)) throw new InvalidResponseError("CPA native manifest is missing a models array");
  models.forEach((model, index) => {
    if (!isRecord(model) || exactModelID(model) === "") throw new InvalidResponseError(`CPA native manifest model ${index} has no valid slug or id`);
  });
  return models as Row[];
}

function authIndexOf(entry: Row): string {
  return asString(entry["auth-index"]) || asString(entry.auth_index);
}

export function parseChannels(kind: ChannelKind, text: string): { channels: Channel[]; incomplete: boolean } {
  const wrapped = parseJSON(text);
  if (!isRecord(wrapped) || !(kind in wrapped)) throw new InvalidResponseError(`missing ${kind}`);
  const entries = wrapped[kind];
  if (!Array.isArray(entries)) throw new InvalidResponseError(`invalid ${kind}`);
  const channels: Channel[] = [];
  let incomplete = false;
  for (const entry of entries) {
    if (!isRecord(entry)) throw new InvalidResponseError(`invalid ${kind} entry`);
    if (entry.disabled === true) continue;
    let auth = authIndexOf(entry);
    const keys = entry["api-key-entries"];
    if (Array.isArray(keys) && keys.length > 0) {
      if (isRecord(keys[0])) auth = authIndexOf(keys[0]) || auth;
    } else if (auth === "") {
      // A credential-bearing entry without an auth index cannot be matched to its identity.
      incomplete = true;
      continue;
    }
    const models: string[] = [];
    if (entry.models !== undefined && entry.models !== null) {
      if (!Array.isArray(entry.models)) throw new InvalidResponseError(`invalid ${kind} models`);
      for (const model of entry.models) {
        if (!isRecord(model) || asString(model.name) === "") throw new InvalidResponseError(`invalid ${kind} model name`);
        if (model.alias !== undefined && model.alias !== null && typeof model.alias !== "string") {
          throw new InvalidResponseError(`invalid ${kind} model alias`);
        }
        models.push(asString(model.alias) || asString(model.name));
      }
    }
    channels.push({
      type: kind,
      name: asString(entry.name),
      prefix: asString(entry.prefix).replace(/^\/+|\/+$/g, ""),
      baseUrl: asString(entry["base-url"]) || asString(entry.base_url),
      authIndex: auth,
      models,
    });
  }
  return { channels, incomplete };
}
