// Catalog build pipeline (design D5).
// prepare(): per authority generation — membership, include_models selection, the compile set
//   (kept plus their inherit/metadata_from closure) and one compile in a short-lived worker.
// project(): per client version — on-demand @cpa-upstream reads, record-source resolution,
//   rule overrides and gateway post-processing. Intermediates are function-local.
import type { ResourceLimits } from "node:worker_threads";
import { nativeRows, runtimeConflicts, type AuthorityGeneration } from "./authority.ts";
import type { GatewayConfig } from "./config.ts";
import { syncedSlugs } from "./config.ts";
import { makeResolver, matchRules, neededSites, runCompileWorker, selectModels, type CompiledCatalog, type HiddenModel, type MetadataFields } from "./core.ts";
import type { Channel, CpaClient } from "./cpa.ts";
import { exactModelID, mergePatch, type Row } from "./json.ts";
import type { Logger } from "./log.ts";
import { postprocess } from "./postprocess.ts";
import type { ReadOptions } from "./read-cache.ts";
import { readCpaUpstreamChannels, type UpstreamChannelRead } from "./upstream.ts";

const PROTECTED = new Set(["slug", "id"]);

export class CatalogUnavailableError extends Error {}
export class CatalogConfigError extends Error {}

export type MemberOrigin = "cpa" | "aisix" | "static";

export interface Member {
  readonly id: string;
  readonly origin: MemberOrigin;
  /** The CPA native row (the `@self` record); absent for AISIX-only and static-only members. */
  readonly row?: Row;
  /** Emitted exactly as CPA returned it: never compiled, resolved or post-processed. */
  readonly preserveNative: boolean;
}

export interface PreparedGeneration {
  readonly authority: AuthorityGeneration;
  /** Catalog members in output order. */
  readonly members: readonly Member[];
  readonly kept: ReadonlySet<string>;
  readonly hidden: ReadonlyMap<string, HiddenModel>;
  /** Hidden members (or pool entries) the compile set needs → the kept slugs referencing them. */
  readonly referencedBy: ReadonlyMap<string, readonly string[]>;
  /** Compile-set IDs in output order (members, then referenced pool entries). */
  readonly compileSet: readonly string[];
  readonly catalog: CompiledCatalog;
}

export interface BuildDeps {
  readonly config: GatewayConfig;
  readonly cpa: CpaClient;
  readonly workerFile: string | URL;
  /** Build-time SourceTables JSON. */
  readonly tablesPath: string;
  readonly resourceLimits?: ResourceLimits;
  readonly log: Logger;
}

/** Unicode code-point order; UTF-16 surrogates (U+10000 and up) sort after U+E000–U+FFFF. */
function compareCodePoints(a: string, b: string): number {
  const length = Math.min(a.length, b.length);
  for (let index = 0; index < length; index++) {
    let x = a.charCodeAt(index);
    let y = b.charCodeAt(index);
    if (x === y) continue;
    if (x >= 0xd800) x += x < 0xe000 ? 0x2000 : -0x800;
    if (y >= 0xd800) y += y < 0xe000 ? 0x2000 : -0x800;
    return x - y;
  }
  return a.length - b.length;
}

/** Catalog output order: IDs without `/` first, then all others; each group in code-point order. */
export function compareCatalogIDs(a: string, b: string): number {
  const aBare = !a.includes("/");
  return aBare !== !b.includes("/") ? (aBare ? -1 : 1) : compareCodePoints(a, b);
}

/** Codex `priority` for the member at output position `index`: 1-based, ascending sorts first. */
export const codexPriority = (index: number) => index + 1;

/** Hidden pool IDs: `<custom key>/<name>`. */
function poolIDs(config: GatewayConfig): Set<string> {
  const out = new Set<string>();
  for (const [key, names] of config.customChannels) for (const name of names) out.add(`${key}/${name}`);
  return out;
}

export async function prepare(deps: BuildDeps, authority: AuthorityGeneration, signal?: AbortSignal): Promise<PreparedGeneration> {
  const { config } = deps;
  if (authority.cpa === "failed") throw new CatalogUnavailableError("CPA native manifest unavailable");
  const conflicts = runtimeConflicts(authority.channels, config.customChannels.keys(), authority.admitted);
  if (conflicts.length > 0) throw new CatalogConfigError(conflicts.join("; "));

  // Parsed here and owned by this build only; the authority keeps just the validated text.
  const rows = nativeRows(authority);
  const members: Member[] = [];
  const seen = new Set<string>();
  for (const model of authority.admitted) {
    seen.add(model.id);
    members.push({ id: model.id, origin: "cpa", row: rows[model.index], preserveNative: model.preserveNative });
  }
  for (const id of authority.aisixIDs) {
    if (seen.has(id)) continue;
    seen.add(id);
    members.push({ id, origin: "aisix", preserveNative: false });
  }
  for (const model of config.staticModels) {
    if (seen.has(model.slug)) continue;
    seen.add(model.slug);
    members.push({ id: model.slug, origin: "static", preserveNative: false });
  }
  // Source precedence is settled by `seen` above; output order is fixed once per generation.
  members.sort((a, b) => compareCatalogIDs(a.id, b.id));

  const selection = selectModels(config.metadata, members.map((member) => member.id));
  const kept = new Set(selection.kept);
  const hidden = new Map(selection.hidden.map((entry) => [entry.id, entry]));

  // Closure of references from kept models; the cycle check at config load bounds it.
  const pool = poolIDs(config);
  const refs = new Map<string, string[]>();
  for (const model of config.staticModels) refs.set(model.slug, [...model.inherit]);
  for (const [slug, ref] of config.metadataFrom) refs.set(slug, [...(refs.get(slug) ?? []), ref]);
  const needed = new Set(kept);
  const referencedBy = new Map<string, string[]>();
  const queue = [...kept];
  while (queue.length > 0) {
    const slug = queue.pop()!;
    for (const ref of refs.get(slug) ?? []) {
      if (!kept.has(ref) && (seen.has(ref) || pool.has(ref))) {
        const by = referencedBy.get(ref) ?? [];
        if (!by.includes(slug)) by.push(slug);
        referencedBy.set(ref, by);
      }
      if (!needed.has(ref) && (seen.has(ref) || pool.has(ref))) {
        needed.add(ref);
        queue.push(ref);
      }
    }
  }
  const preserved = new Set(members.filter((member) => member.preserveNative).map((member) => member.id));
  const compileSet = [...members.map((member) => member.id), ...pool].filter((id) => needed.has(id));
  const compileIDs = compileSet.filter((id) => !preserved.has(id));

  const ruleMatches = matchRules(config.metadata, compileIDs);
  const sites = neededSites(config.metadata, compileIDs, ruleMatches);
  const { catalog } = await runCompileWorker({
    workerFile: deps.workerFile,
    config: config.metadata,
    publicIds: compileIDs,
    ruleMatches,
    neededSites: sites,
    // Without a needed site the core compiles in-process and no worker is spawned.
    tablesPath: sites.size > 0 ? deps.tablesPath : undefined,
    resourceLimits: deps.resourceLimits,
    signal,
  });
  // Only plans and override patches are needed afterwards; drop the inspection-only candidate lists.
  for (const model of catalog.models) {
    model.candidates = [];
    delete model.skippedEntries;
  }
  return { authority, members, kept, hidden, referencedBy, compileSet, catalog };
}

// Normalized core field → Codex manifest field. `name` becomes display_name in chain order (Go
// parity: sources ahead of @self rename the entry); without one CPA's name stays. max_output_tokens is also emitted
// as max_tokens. Every other mapped field takes the chain result, or is removed when the chain
// yields none (strict replacement; @self in the chain keeps CPA's value as a candidate).
const FIELD_MAP: ReadonlyArray<readonly [string, readonly string[]]> = [
  ["context_window", ["context_window"]],
  ["max_input_tokens", ["max_input_tokens"]],
  ["max_output_tokens", ["max_output_tokens", "max_tokens"]],
  ["input_modalities", ["input_modalities"]],
  ["output_modalities", ["output_modalities"]],
  ["supported_reasoning_levels", ["supported_reasoning_levels"]],
  ["default_reasoning_level", ["default_reasoning_level"]],
];

export function applyFields(entry: Row, fields: MetadataFields): void {
  for (const [source, targets] of FIELD_MAP) {
    const value = fields[source];
    for (const target of targets) {
      if (value === undefined) delete entry[target];
      else entry[target] = value;
    }
  }
  if (typeof fields.name === "string") entry.display_name = fields.name;
}

/** Longest CPA channel prefix owning a public ID, with the upstream model name. */
function channelOf(channels: readonly Channel[], id: string): { channel: Channel; name: string } | undefined {
  let best: Channel | undefined;
  for (const channel of channels) {
    if (channel.prefix && id.startsWith(`${channel.prefix}/`) && (!best || channel.prefix.length > best.prefix.length)) best = channel;
  }
  return best ? { channel: best, name: id.slice(best.prefix.length + 1) } : undefined;
}

export interface Projection {
  /** Kept catalog entries in output order. */
  readonly models: readonly Row[];
  /** Compiled entries for kept and referenced IDs (for the models table). */
  readonly entries: ReadonlyMap<string, Row>;
  /** Inputs answered from a retained response, plus failed reads without one. */
  readonly staleInputs: number;
}

export interface ProjectOptions extends ReadOptions {
  clientVersion: string;
}

export async function project(deps: BuildDeps, prepared: PreparedGeneration, options: ProjectOptions): Promise<Projection> {
  const { config, cpa } = deps;
  const { catalog, authority } = prepared;
  let staleInputs = 0;
  const readOptions: ProjectOptions = { ...options, onStale: () => staleInputs++ };

  // @cpa-upstream: read only channels whose compiled models carry a @cpa-upstream node.
  const reads = new Map<string, UpstreamChannelRead & { lookupIDs: Set<string> }>();
  for (const model of catalog.models) {
    if (!model.plan.some((node) => node.kind === "record" && node.keyword === "@cpa-upstream")) continue;
    const owner = channelOf(authority.channels, model.publicId);
    if (!owner) continue;
    let read = reads.get(owner.channel.prefix);
    if (!read) {
      const settings = config.channels.get(owner.channel.prefix);
      read = { channel: owner.channel, path: settings?.path, ollamaBaseUrl: settings?.ollamaNativeBaseUrl, lookupIDs: new Set() };
      reads.set(owner.channel.prefix, read);
    }
    read.lookupIDs.add(owner.name);
  }
  const upstream = await readCpaUpstreamChannels(cpa, [...reads.values()], {
    ...readOptions,
    channelTimeoutMs: config.channelTimeoutMs,
  });
  for (const result of upstream.values()) if (result.failed) staleInputs++;

  const nativeRows = new Map<string, Row>();
  for (const member of prepared.members) if (member.row) nativeRows.set(member.id, member.row);
  const resolver = makeResolver(catalog, {
    "@self": (id) => nativeRows.get(id),
    "@cpa-upstream": (id) => {
      const owner = channelOf(authority.channels, id);
      return owner ? upstream.get(owner.channel.prefix)?.rows.get(owner.name) : undefined;
    },
  });

  const compiled = new Set(prepared.compileSet);
  const pool = new Set<string>();
  for (const [key, names] of config.customChannels) for (const name of names) pool.add(`${key}/${name}`);
  const entries = new Map<string, Row>();
  const poolEntries = new Map<string, Row>();
  const preserveNative = new Set<string>();
  const overrides = new Map<string, Row>();
  const byMember = new Map(prepared.members.map((member) => [member.id, member]));
  for (const id of prepared.compileSet) {
    const member = byMember.get(id);
    if (member?.preserveNative && member.row) {
      // Native-only and identity-fallback rows are emitted by reference, unchanged.
      preserveNative.add(id);
      entries.set(id, member.row);
      continue;
    }
    // Shallow copy: nested values stay shared with the immutable generation and are only
    // replaced (never mutated) by the copy-on-write helpers below.
    const entry: Row = member?.row ? { ...member.row } : member?.origin === "aisix" ? { id, slug: id } : {};
    applyFields(entry, resolver.resolve(id) ?? {});
    const patches = resolver.overridePatches(id);
    const combined: Row = {};
    for (const patch of patches) {
      mergePatch(entry, patch, PROTECTED);
      Object.assign(combined, patch);
    }
    if (patches.length > 0) overrides.set(id, combined);
    (pool.has(id) && !byMember.has(id) ? poolEntries : entries).set(id, entry);
  }

  const synced = syncedSlugs(config, entries.keys());
  postprocess({
    entries,
    preserveNative,
    overrides,
    metadataFrom: new Map([...config.metadataFrom].filter(([slug]) => compiled.has(slug))),
    pool: poolEntries,
    staticModels: config.staticModels.filter((model) => compiled.has(model.slug)),
    syncMaxContextWindow: (slug) => synced.has(slug),
    log: deps.log,
  });
  // Members are already in output order; `priority` is the one field rewritten on passthrough rows,
  // which get a shallow copy so the generation's native row stays untouched.
  const models: Row[] = [];
  for (const member of prepared.members) {
    if (!prepared.kept.has(member.id)) continue;
    const entry = entries.get(member.id);
    if (!entry || exactModelID(entry) === "") continue;
    const priority = codexPriority(models.length);
    if (preserveNative.has(member.id)) models.push({ ...entry, priority });
    else {
      entry.priority = priority;
      models.push(entry);
    }
  }
  return { models, entries, staleInputs };
}
