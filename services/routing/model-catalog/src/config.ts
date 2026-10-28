// Gateway YAML: the shared `metadata` block (parsed by the extension core) plus gateway-only
// settings. Unknown and legacy keys fail closed so a partially understood file is never served.
import { parseDocument } from "yaml";
import { compileModelPattern, MetadataConfigError, parseMetadataConfigValue, selectModels, type MetadataConfig } from "./core.ts";
import { isRecord, type Row } from "./json.ts";
import type { StaticModel } from "./postprocess.ts";

export const RECORD_SOURCES = ["@self", "@cpa-upstream"] as const;

export class ConfigError extends Error {}

export interface ChannelSettings {
  /** Replaces the adapter's default upstream models path for `@cpa-upstream` reads. */
  readonly path?: string;
  /** `@cpa-upstream` rows come from Ollama `/api/show` under this base URL. */
  readonly ollamaNativeBaseUrl?: string;
}

/** `include_models`-style rules: `!` excludes, the last match decides, unmatched = not selected. */
export type SyncRules = NonNullable<MetadataConfig["includeModels"]>;

export interface GatewayConfig {
  readonly metadata: MetadataConfig;
  readonly cpaBaseUrl: string;
  readonly httpConcurrency: number;
  readonly channelTimeoutMs: number;
  readonly overallDeadlineMs: number;
  readonly channels: ReadonlyMap<string, ChannelSettings>;
  /** Hidden inheritance pools: key → member names, compiled as `<key>/<name>`, never emitted. */
  readonly customChannels: ReadonlyMap<string, readonly string[]>;
  readonly staticModels: readonly StaticModel[];
  /** Full output slug → full output slug whose metadata it copies. */
  readonly metadataFrom: ReadonlyMap<string, string>;
  /** true = every non-native entry, false = none, or the ordered glob rules selecting models. */
  readonly syncMaxContextWindow: boolean | SyncRules;
  readonly aisixModelsUrl: string;
  readonly aisixTimeoutMs: number;
  readonly scheduledRefreshMs: number;
  readonly entryRefreshWindowMs: number;
}

const LEGACY: Readonly<Record<string, string>> = {
  source_priority: "use metadata.chain or metadata.models[].chain",
  provider_prefix_map: "use literal provider ids or @creator in metadata chains",
  lookup_ids: "use a literal 4th chain segment, e.g. models.dev/<provider>/api_key/<query-id>",
  global_fallback: "write the rule's chain explicitly; @default inserts the root chain in place",
  exclude_models: 'use metadata.include_models, e.g. ["**", "!<model-id>"]',
  fetch_models: "remove it; channel upstream rows are read only when a chain contains @cpa-upstream",
  bare_models_takeover: 'remove it; admit CPA IDs without a CPA prefix through EXTRA_CPA in catalog.lua (hide members with metadata.include_models: ["*/**"])',
};
// Root-only removed fields.
const LEGACY_ROOT: Readonly<Record<string, string>> = {
  membership: "membership is decided by the profile's catalog.lua; move cpa_include entries to EXTRA_CPA in catalog.lua",
};
const LEGACY_CHANNEL: Readonly<Record<string, string>> = {
  ...LEGACY,
  include: "use metadata.include_models globs",
  exclude: "use metadata.include_models globs with a leading !",
  overrides: 'use metadata.models rules, e.g. {match: "<prefix>/<model>", overrides: {...}}',
  models: "use metadata.models[].match rules; move metadata_from to the root metadata_from map",
  sync_max_context_window: 'use the root sync_max_context_window glob list, e.g. ["<prefix>/**"]',
};
const SHARED_KEYS: Readonly<Record<string, string>> = {
  chain: "move it under metadata.chain",
  models: "move it under metadata.models",
  include_models: "move it under metadata.include_models",
};
const ROOT_KEYS = new Set([
  "metadata",
  "cpa_base_url",
  "http_concurrency",
  "channel_timeout",
  "overall_deadline",
  "channels",
  "custom_channels",
  "static_models",
  "metadata_from",
  "sync_max_context_window",
  "aisix_models_url",
  "aisix_timeout",
  "scheduled_refresh_interval",
  "entry_refresh_window",
]);
const CHANNEL_KEYS = new Set(["path", "ollama_native_base_url"]);
const STATIC_KEYS = new Set(["slug", "inherit", "overrides"]);
const PROTECTED_OVERRIDE_KEYS = ["slug", "id"];

const fail = (path: string, reason: string): never => {
  throw new ConfigError(`${path}: ${reason}`);
};

function checkKeys(value: Row, path: string, known: ReadonlySet<string>, legacy: Readonly<Record<string, string>>): void {
  for (const key of Object.keys(value)) {
    const at = path ? `${path}.${key}` : key;
    if (Object.hasOwn(legacy, key)) fail(at, `removed field; ${legacy[key]}`);
    if (!known.has(key)) fail(at, "unknown field");
  }
}

const UNITS: Readonly<Record<string, number>> = { ns: 1e-6, us: 1e-3, "µs": 1e-3, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };

/** Go time.Duration syntax ("30m", "1h30m", "5s"); must be positive. */
export function parseDuration(value: unknown, path: string): number {
  if (typeof value !== "string" || !/^(?:\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m|h))+$/.test(value)) fail(path, 'expected a duration such as "30s" or "10m"');
  let ms = 0;
  for (const [, amount, unit] of (value as string).matchAll(/(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/g)) ms += Number(amount) * UNITS[unit!]!;
  if (!(ms > 0)) fail(path, "must be positive");
  return ms;
}

function optionalDuration(root: Row, key: string, fallback: number): number {
  return root[key] === undefined ? fallback : parseDuration(root[key], key);
}

function nonEmptyString(value: unknown, path: string): string {
  if (typeof value !== "string" || value.trim() === "") fail(path, "expected a non-empty string");
  return value as string;
}

/** An ordered `include_models`-style glob list; an empty list is rejected with `emptyReason`. */
function readGlobList(value: unknown, path: string, emptyReason: string): SyncRules {
  if (!Array.isArray(value)) fail(path, "expected a list of model ID globs");
  if ((value as unknown[]).length === 0) fail(path, emptyReason);
  return (value as unknown[]).map((item, index) => {
    const at = `${path}[${index}]`;
    if (typeof item !== "string" || item === "") fail(at, "expected a non-empty glob string");
    const raw = item as string;
    const exclude = raw.startsWith("!");
    const glob = exclude ? raw.slice(1) : raw;
    if (glob === "") fail(at, "`!` must be followed by a glob");
    try {
      compileModelPattern(glob, at);
    } catch (error) {
      if (error instanceof MetadataConfigError) throw new ConfigError(error.message);
      throw error;
    }
    return { raw, exclude, glob };
  });
}

function readSyncMaxContextWindow(value: unknown): boolean | SyncRules {
  const path = "sync_max_context_window";
  if (value === undefined || typeof value === "boolean") return value ?? false;
  if (!Array.isArray(value)) fail(path, "expected true, false or a list of model ID globs");
  return readGlobList(value, path, "an empty list selects no model; use false or omit the field");
}

function readOverrides(value: unknown, path: string): Row | undefined {
  if (value === undefined) return undefined;
  if (!isRecord(value)) fail(path, "expected a mapping");
  for (const key of PROTECTED_OVERRIDE_KEYS) if (Object.hasOwn(value as Row, key)) fail(`${path}.${key}`, `overrides may not set ${key}`);
  return value as Row;
}

function mapping(value: unknown, path: string): Row {
  if (value === undefined || value === null) return {};
  if (!isRecord(value)) fail(path, "expected a mapping");
  return value as Row;
}

/** Fails on any reference cycle through static inherit lists and metadata_from. */
function checkCycles(statics: readonly StaticModel[], metadataFrom: ReadonlyMap<string, string>): void {
  const edges = new Map<string, string[]>();
  const add = (from: string, to: string) => edges.set(from, [...(edges.get(from) ?? []), to]);
  for (const model of statics) for (const ref of model.inherit) add(model.slug, ref);
  for (const [slug, ref] of metadataFrom) add(slug, ref);
  const state = new Map<string, "active" | "done">();
  const visit = (node: string, path: string[]): void => {
    if (state.get(node) === "done") return;
    if (state.get(node) === "active") fail("static_models/metadata_from", `reference cycle ${[...path.slice(path.indexOf(node)), node].join(" -> ")}`);
    state.set(node, "active");
    for (const next of edges.get(node) ?? []) visit(next, [...path, node]);
    state.set(node, "done");
  };
  for (const node of edges.keys()) visit(node, []);
}

/** Parses and validates the complete gateway configuration from its raw YAML text. */
export function parseConfig(text: string): GatewayConfig {
  const doc = parseDocument(text, { uniqueKeys: true, prettyErrors: false });
  if (doc.errors.length > 0) fail("config", `invalid YAML: ${doc.errors[0]!.message.split("\n")[0]}`);
  const root = mapping(doc.toJS({ maxAliasCount: 100 }), "config");
  checkKeys(root, "", ROOT_KEYS, { ...LEGACY, ...LEGACY_ROOT, ...SHARED_KEYS });

  let metadata: MetadataConfig;
  try {
    metadata = parseMetadataConfigValue(root.metadata ?? {}, { recordSources: RECORD_SOURCES, pathPrefix: "metadata" });
  } catch (error) {
    if (error instanceof MetadataConfigError) throw new ConfigError(error.message);
    throw error;
  }
  if (metadata.warnings.length > 0) fail(metadata.warnings[0]!, "unknown field");
  metadata.models?.forEach((rule, index) => readOverrides(rule.overrides, `metadata.models[${index}].overrides`));

  const channels = new Map<string, ChannelSettings>();
  for (const [prefix, value] of Object.entries(mapping(root.channels, "channels"))) {
    const at = `channels.${prefix}`;
    const channel = mapping(value, at);
    checkKeys(channel, at, CHANNEL_KEYS, LEGACY_CHANNEL);
    channels.set(prefix, {
      path: channel.path === undefined ? undefined : nonEmptyString(channel.path, `${at}.path`),
      ollamaNativeBaseUrl: channel.ollama_native_base_url === undefined ? undefined : nonEmptyString(channel.ollama_native_base_url, `${at}.ollama_native_base_url`),
    });
  }

  const customChannels = new Map<string, string[]>();
  for (const [key, value] of Object.entries(mapping(root.custom_channels, "custom_channels"))) {
    const at = `custom_channels.${key}`;
    if (key === "" || key.includes("/")) fail(at, "expected a non-empty key without /");
    if (channels.has(key)) fail(at, `also defined under channels.${key}`);
    const pool = mapping(value, at);
    checkKeys(pool, at, new Set(["models"]), LEGACY);
    if (!Array.isArray(pool.models)) fail(`${at}.models`, 'expected a list of member names; put chains and overrides in metadata.models rules matching "<key>/<name>"');
    customChannels.set(key, (pool.models as unknown[]).map((name, index) => nonEmptyString(name, `${at}.models[${index}]`)));
  }

  if (root.static_models !== undefined && !Array.isArray(root.static_models)) fail("static_models", "expected a list");
  const seenStatic = new Set<string>();
  const staticModels: StaticModel[] = ((root.static_models as unknown[] | undefined) ?? []).map((value, index) => {
    const at = `static_models[${index}]`;
    const model = mapping(value, at);
    checkKeys(model, at, STATIC_KEYS, LEGACY);
    const slug = nonEmptyString(model.slug, `${at}.slug`);
    if (seenStatic.has(slug)) fail(`${at}.slug`, `duplicate static model ${JSON.stringify(slug)}`);
    seenStatic.add(slug);
    if (model.inherit !== undefined && !Array.isArray(model.inherit)) fail(`${at}.inherit`, "expected an ordered list of slugs");
    const inherit = ((model.inherit as unknown[] | undefined) ?? []).map((ref, i) => nonEmptyString(ref, `${at}.inherit[${i}]`));
    return { slug, inherit, overrides: readOverrides(model.overrides, `${at}.overrides`) };
  });

  const metadataFrom = new Map<string, string>();
  for (const [slug, ref] of Object.entries(mapping(root.metadata_from, "metadata_from"))) {
    metadataFrom.set(nonEmptyString(slug, "metadata_from"), nonEmptyString(ref, `metadata_from.${slug}`));
  }
  checkCycles(staticModels, metadataFrom);

  const channelTimeoutMs = optionalDuration(root, "channel_timeout", 8_000);
  const overallDeadlineMs = optionalDuration(root, "overall_deadline", 25_000);
  let httpConcurrency = 8;
  if (root.http_concurrency !== undefined) {
    if (!Number.isInteger(root.http_concurrency) || (root.http_concurrency as number) <= 0) fail("http_concurrency", "expected a positive integer");
    httpConcurrency = root.http_concurrency as number;
  }
  return {
    metadata,
    cpaBaseUrl: nonEmptyString(root.cpa_base_url, "cpa_base_url"),
    httpConcurrency,
    channelTimeoutMs,
    overallDeadlineMs,
    channels,
    customChannels,
    staticModels,
    metadataFrom,
    syncMaxContextWindow: readSyncMaxContextWindow(root.sync_max_context_window),
    aisixModelsUrl: root.aisix_models_url === undefined ? "" : nonEmptyString(root.aisix_models_url, "aisix_models_url"),
    aisixTimeoutMs: Math.min(optionalDuration(root, "aisix_timeout", channelTimeoutMs), overallDeadlineMs),
    scheduledRefreshMs: optionalDuration(root, "scheduled_refresh_interval", 30 * 60_000),
    entryRefreshWindowMs: optionalDuration(root, "entry_refresh_window", 10 * 60_000),
  };
}

/** The slugs whose max_context_window is set to context_window, chosen with the shared matcher. */
export function syncedSlugs(config: GatewayConfig, slugs: Iterable<string>): Set<string> {
  const rules = config.syncMaxContextWindow;
  if (rules === false) return new Set();
  if (rules === true) return new Set(slugs);
  return new Set(selectModels({ includeModels: rules, warnings: [] }, [...slugs]).kept);
}
