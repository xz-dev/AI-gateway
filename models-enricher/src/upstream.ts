// `@cpa-upstream` record source: the rich model rows CPA reads from a channel's upstream through the
// management api-call. Read only for channels the build asks for; rows only contribute fields.
import type { Channel, ChannelKind, CpaClient } from "./cpa.ts";
import { InvalidResponseError } from "./http.ts";
import { isRecord, type Row } from "./json.ts";
import { errorMessage } from "./log.ts";
import type { ReadOptions } from "./read-cache.ts";

interface Adapter {
  headers: Record<string, string>;
  path(clientVersion: string): string;
  parse(body: unknown): Map<string, Row>;
}

function adapterFor(kind: ChannelKind): Adapter {
  const bearer = { Authorization: "Bearer $TOKEN$" };
  switch (kind) {
    case "claude-api-key":
      return { headers: { "x-api-key": "$TOKEN$", "anthropic-version": "2023-06-01" }, path: () => "/v1/models", parse: parseClaude };
    case "gemini-api-key":
    case "interactions-api-key":
      return { headers: { "x-goog-api-key": "$TOKEN$" }, path: () => "/v1beta/models", parse: parseGemini };
    case "codex-api-key":
      return { headers: bearer, path: (v) => `/v1/models?client_version=${encodeURIComponent(v)}`, parse: parseOpenAI };
    case "openai-compatibility":
      return { headers: bearer, path: (v) => (v ? `/v1/models?client_version=${encodeURIComponent(v)}` : "/v1/models"), parse: parseOpenAI };
    case "xai-api-key":
    case "vertex-api-key":
      return { headers: bearer, path: () => "/v1/models", parse: parseOpenAI };
  }
}

function rowsOf(body: unknown, field: string): Row[] | undefined {
  if (!isRecord(body)) throw new InvalidResponseError("invalid model list");
  const rows = body[field];
  if (rows === undefined || rows === null) return undefined;
  if (!Array.isArray(rows) || !rows.every(isRecord)) throw new InvalidResponseError(`invalid model list ${field}`);
  return rows as Row[];
}

function firstString(row: Row, ...keys: string[]): string {
  for (const key of keys) {
    const value = row[key];
    if (typeof value === "string" && value !== "") return value;
  }
  return "";
}

// Declared upstream fields fill canonical names only when absent; 0/false/[] are not absent.
function mapDeclared(row: Row, key: string, value: unknown): void {
  if ((row[key] === undefined || row[key] === null) && value !== undefined && value !== null) row[key] = value;
}

function nested(row: Row, object: string, key: string): unknown {
  const inner = row[object];
  return isRecord(inner) ? inner[key] : undefined;
}

function collect(rows: readonly Row[] | undefined, idOf: (row: Row) => string, normalize: (row: Row) => void): Map<string, Row> {
  const out = new Map<string, Row>();
  for (const row of rows ?? []) {
    const id = idOf(row);
    if (id === "" || out.has(id)) continue;
    normalize(row);
    out.set(id, row);
  }
  return out;
}

export function parseOpenAI(body: unknown): Map<string, Row> {
  const data = rowsOf(body, "data");
  const rows = data && data.length > 0 ? data : rowsOf(body, "models");
  return collect(rows, (row) => firstString(row, "id", "slug", "name"), (row) => {
    mapDeclared(row, "display_name", row.name);
    mapDeclared(row, "context_window", row.context_length);
    mapDeclared(row, "max_input_tokens", row.input_token_limit);
    mapDeclared(row, "max_output_tokens", nested(row, "top_provider", "max_completion_tokens"));
    mapDeclared(row, "input_modalities", nested(row, "architecture", "input_modalities"));
    mapDeclared(row, "output_modalities", nested(row, "architecture", "output_modalities"));
  });
}

export function parseClaude(body: unknown): Map<string, Row> {
  return collect(rowsOf(body, "data"), (row) => firstString(row, "id"), (row) => mapDeclared(row, "display_name", row.name));
}

export function parseGemini(body: unknown): Map<string, Row> {
  return collect(rowsOf(body, "models"), (row) => firstString(row, "name", "id").replace(/^models\//, ""), (row) => {
    mapDeclared(row, "display_name", row.displayName);
    mapDeclared(row, "max_input_tokens", row.inputTokenLimit);
    mapDeclared(row, "max_output_tokens", row.outputTokenLimit);
  });
}

/** Ollama /api/show: keeps the public details and maps only an explicit `*.context_length`. */
export function parseOllamaShow(body: unknown): Row {
  if (!isRecord(body)) throw new InvalidResponseError("invalid /api/show body");
  const info = body.model_info;
  if (isRecord(info)) {
    for (const key of Object.keys(info).sort()) {
      if (key.endsWith(".context_length")) mapDeclared(body, "context_window", info[key]);
    }
  }
  return body;
}

/** Joins a channel base URL and path, dropping a version segment both already carry. */
export function joinURL(base: string, path: string): string {
  const trimmed = base.replace(/\/+$/, "");
  let tail = path.startsWith("/") ? path : `/${path}`;
  for (const segment of ["/v1beta", "/v1"]) {
    if (trimmed.endsWith(segment) && tail.startsWith(`${segment}/`)) {
      tail = tail.slice(segment.length);
      break;
    }
  }
  return trimmed + tail;
}

export interface UpstreamChannelRead {
  channel: Channel;
  /** Configured `channels.<prefix>.path`, replacing the adapter default. */
  path?: string;
  /** Ollama native base URL: rows come from /api/show for `lookupIDs` instead of the model list. */
  ollamaBaseUrl?: string;
  /** Unprefixed model IDs needing a row (Ollama only); duplicates are read once. */
  lookupIDs?: Iterable<string>;
}

export interface UpstreamResult {
  /** Upstream model ID (without the CPA channel prefix) → row. */
  readonly rows: ReadonlyMap<string, Row>;
  /** At least one read failed without a retained response. */
  readonly failed: boolean;
}

export interface UpstreamOptions extends ReadOptions {
  clientVersion: string;
  /** Per-channel budget, combined with `signal`. */
  channelTimeoutMs: number;
}

export async function readCpaUpstream(cpa: CpaClient, read: UpstreamChannelRead, options: UpstreamOptions): Promise<UpstreamResult> {
  const { channel } = read;
  const adapter = adapterFor(channel.type);
  const timeout = AbortSignal.timeout(options.channelTimeoutMs);
  const readOptions: ReadOptions = { ...options, signal: options.signal ? AbortSignal.any([options.signal, timeout]) : timeout };
  if (read.ollamaBaseUrl !== undefined) {
    const url = joinURL(read.ollamaBaseUrl, "/api/show");
    const headers = { "Content-Type": "application/json", ...adapter.headers };
    const rows = new Map<string, Row>();
    let failed = false;
    await Promise.all(
      [...new Set(read.lookupIDs ?? [])].map(async (id) => {
        try {
          rows.set(id, await cpa.apiCall(channel, { method: "POST", url, headers, data: JSON.stringify({ model: id }) }, parseOllamaShow, readOptions));
        } catch (error) {
          failed = true;
          cpa.log.warn("ollama /api/show miss", { prefix: channel.prefix, model: id, err: errorMessage(error) });
        }
      }),
    );
    return { rows, failed };
  }
  const url = joinURL(channel.baseUrl, read.path || adapter.path(options.clientVersion));
  try {
    return { rows: await cpa.apiCall(channel, { method: "GET", url, headers: adapter.headers }, adapter.parse, readOptions), failed: false };
  } catch (error) {
    cpa.log.warn("channel fetch failed", { prefix: channel.prefix, name: channel.name, type: channel.type, err: errorMessage(error) });
    return { rows: new Map(), failed: true };
  }
}

/** Reads the requested channels concurrently (the HTTP pool bounds in-flight requests). */
export async function readCpaUpstreamChannels(cpa: CpaClient, reads: readonly UpstreamChannelRead[], options: UpstreamOptions): Promise<Map<string, UpstreamResult>> {
  const results = await Promise.all(reads.map((read) => readCpaUpstream(cpa, read, options)));
  return new Map(reads.map((read, index) => [read.channel.prefix, results[index]!]));
}
