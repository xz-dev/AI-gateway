// Gateway-only catalog post-processing, run after shared chain selection and rule overrides:
// metadata_from references, hidden custom pools, static models with ordered inherit lists,
// reasoning-default conflict removal and max_context_window synchronization.
import { cloneJSON, isRecord, mergePatch, overlay, toInt, type Row } from "./json.ts";
import type { Logger } from "./log.ts";

const PROTECTED = new Set(["slug", "id"]);

export interface StaticModel {
  readonly slug: string;
  /** Earlier entries win; each names a compiled catalog or pool slug. */
  readonly inherit: readonly string[];
  readonly overrides?: Row;
}

export interface PostprocessInput {
  /** Compile-set entries (kept plus referenced) in output order; owned by this build and updated in place. */
  entries: Map<string, Row>;
  /** Slugs emitted exactly as CPA returned them; nothing here touches them. */
  preserveNative: ReadonlySet<string>;
  /** Merged rule overrides per slug, as already applied by chain resolution. */
  overrides: ReadonlyMap<string, Row>;
  /** Full output slug → full output slug whose metadata it copies. */
  metadataFrom: ReadonlyMap<string, string>;
  /** Compiled custom_channels pool members; readable by static inherit only, never emitted. */
  pool: Map<string, Row>;
  staticModels: readonly StaticModel[];
  syncMaxContextWindow(slug: string): boolean;
  log?: Logger;
}

/** Removes a default reasoning level only when the declared levels prove it unsupported. */
export function omitConflictingReasoningDefault(entry: Row, overrides: Row | undefined): void {
  if (overrides && "default_reasoning_level" in overrides) return;
  const level = entry.default_reasoning_level;
  if (typeof level !== "string" || level === "") return;
  const levels = entry.supported_reasoning_levels;
  if (!Array.isArray(levels)) return;
  for (const option of levels) {
    if (!isRecord(option)) return;
    const effort = option.effort;
    if (typeof effort !== "string" || effort === "" || effort === level) return;
  }
  delete entry.default_reasoning_level;
}

/** With sync, max_context_window equals context_window; otherwise it is only raised to it. */
export function raiseMaxContextWindow(entry: Row, sync: boolean): void {
  const context = toInt(entry.context_window);
  if (context > 0 && (sync || toInt(entry.max_context_window) < context)) entry.max_context_window = entry.context_window;
}

export function postprocess(input: PostprocessInput): Map<string, Row> {
  const { entries, preserveNative, overrides, pool, log } = input;
  const explicitMax = new Set<string>();
  for (const [slug, patch] of overrides) if (patch.max_context_window !== undefined && patch.max_context_window !== null) explicitMax.add(slug);

  // References read a fixed snapshot: chained references never see each other's results.
  const referenced: Array<[string, Row]> = [];
  for (const [slug, ref] of input.metadataFrom) {
    const entry = entries.get(slug);
    if (!entry || preserveNative.has(slug)) continue;
    const next = cloneJSON(entry);
    const source = entries.get(ref);
    if (source) overlay(next, source);
    else log?.warn("metadata reference missing", { slug, source: ref });
    const patch = overrides.get(slug);
    if (patch) mergePatch(next, patch, PROTECTED);
    referenced.push([slug, next]);
  }
  for (const [slug, next] of referenced) entries.set(slug, next);

  for (const [slug, entry] of entries) if (!preserveNative.has(slug)) omitConflictingReasoningDefault(entry, overrides.get(slug));
  for (const [slug, entry] of pool) omitConflictingReasoningDefault(entry, overrides.get(slug));

  // Statics materialize in configuration order and may inherit from earlier statics.
  for (const model of input.staticModels) {
    if (!model.slug || preserveNative.has(model.slug)) continue;
    const entry = entries.get(model.slug) ?? {};
    for (let index = model.inherit.length - 1; index >= 0; index--) {
      const ref = model.inherit[index]!;
      const source = entries.get(ref) ?? pool.get(ref);
      if (!source) {
        log?.warn("static inherit target missing", { slug: model.slug, target: ref });
        continue;
      }
      if (source !== entry) overlay(entry, source);
    }
    if (model.overrides) {
      mergePatch(entry, model.overrides, PROTECTED);
      if (model.overrides.max_context_window !== undefined && model.overrides.max_context_window !== null) explicitMax.add(model.slug);
    }
    omitConflictingReasoningDefault(entry, model.overrides);
    entries.set(model.slug, entry);
  }

  for (const [slug, entry] of entries) {
    if (preserveNative.has(slug)) continue;
    entry.slug = slug;
    if ("id" in entry) entry.id = slug;
    // An explicit max_context_window override wins over both synchronization and raising.
    if (!explicitMax.has(slug)) raiseMaxContextWindow(entry, input.syncMaxContextWindow(slug));
  }
  return entries;
}
