// Output projections: Codex manifest and standard list bodies, the generation-keyed projection
// cache, and the server-rendered models table (no JavaScript, refresh is a plain POST form).
import type { PreparedGeneration, Projection } from "./build.ts";
import type { Row } from "./json.ts";

export const PROJECTION_TTL_MS = 5 * 60_000;
export const PROJECTION_MAX_ENTRIES = 24;
export const PROJECTION_MAX_BYTES = 64 << 20;

export function codexBody(projection: Projection): string {
  return JSON.stringify({ models: projection.models });
}

/** Kept member IDs only; membership needs no metadata, so no projection build is involved. */
export function standardBody(prepared: PreparedGeneration): string {
  const data: Array<{ id: string; object: "model" }> = [];
  for (const member of prepared.members) if (prepared.kept.has(member.id)) data.push({ id: member.id, object: "model" });
  return JSON.stringify({ object: "list", data });
}

type Entry = { generation: number; body?: string; stored: number; flight?: Promise<string> };

/**
 * Bodies keyed by generation + format + client_version. Entries of other generations are dropped
 * whenever a newer generation is looked up, so a late old-generation build never becomes current.
 * Bounded by entry count and total body bytes; concurrent builds of one key share a flight.
 */
export class ProjectionCache {
  readonly #entries = new Map<string, Entry>();
  #bytes = 0;
  clock: () => number;

  constructor(clock: () => number = Date.now) {
    this.clock = clock;
  }

  get size(): number {
    return this.#entries.size;
  }

  clear(): void {
    for (const [key, entry] of this.#entries) if (!entry.flight) this.#drop(key);
  }

  load(generation: number, key: string, build: () => Promise<string>): Promise<string> {
    for (const [other, entry] of this.#entries) if (entry.generation !== generation && !entry.flight) this.#drop(other);
    const full = `${generation}:${key}`;
    const existing = this.#entries.get(full);
    if (existing?.flight) return existing.flight;
    if (existing?.body !== undefined && this.clock() - existing.stored < PROJECTION_TTL_MS) return Promise.resolve(existing.body);
    if (existing) this.#drop(full);
    const entry: Entry = { generation, stored: 0 };
    entry.flight = build().then(
      (body) => {
        entry.flight = undefined;
        // Only store when still the registered flight of a current-generation key.
        if (this.#entries.get(full) === entry && this.#current(generation)) {
          entry.body = body;
          entry.stored = this.clock();
          this.#bytes += Buffer.byteLength(body);
          this.#evict();
        } else if (this.#entries.get(full) === entry) {
          this.#entries.delete(full);
        }
        return body;
      },
      (error: unknown) => {
        if (this.#entries.get(full) === entry) this.#entries.delete(full);
        throw error;
      },
    );
    this.#entries.set(full, entry);
    return entry.flight;
  }

  #current(generation: number): boolean {
    for (const entry of this.#entries.values()) if (entry.generation > generation) return false;
    return true;
  }

  #drop(key: string): void {
    const entry = this.#entries.get(key);
    if (!entry) return;
    if (entry.body !== undefined) this.#bytes -= Buffer.byteLength(entry.body);
    this.#entries.delete(key);
  }

  #evict(): void {
    for (const [key, entry] of this.#entries) {
      if (this.#entries.size <= PROJECTION_MAX_ENTRIES && this.#bytes <= PROJECTION_MAX_BYTES) return;
      if (!entry.flight) this.#drop(key);
    }
  }
}

// ---------------------------------------------------------------------------
// Models table.

const COLUMNS: ReadonlyArray<readonly [label: string, key: string]> = [
  ["display_name", "display_name"],
  ["input_modalities", "input_modalities"],
  ["output_modalities", "output_modalities"],
  ["context_window", "context_window"],
  ["max_context_window", "max_context_window"],
  ["max_input_tokens", "max_input_tokens"],
  ["max_output_tokens", "max_output_tokens"],
  ["reasoning_levels", "supported_reasoning_levels"],
  ["default_reasoning", "default_reasoning_level"],
];

export interface TableRow {
  readonly slug: string;
  readonly status: string;
  readonly hidden: boolean;
  /** One cell per column; undefined = not processed (hidden and unreferenced). */
  readonly cells?: ReadonlyArray<{ text: string; missing: boolean }>;
}

export interface TableData {
  readonly generation: number;
  readonly rows: readonly TableRow[];
  readonly kept: number;
  readonly hidden: number;
  readonly referencedOnly: number;
  readonly dataVersions: PreparedGeneration["catalog"]["dataVersions"];
  readonly clientVersion: string;
}

function cell(entry: Row, key: string): { text: string; missing: boolean } {
  if (!(key in entry)) return { text: "未知", missing: true };
  const value = entry[key];
  if (value === null) return { text: "null", missing: true };
  if (typeof value === "string" && value !== "") return { text: value, missing: false };
  return { text: JSON.stringify(value), missing: false };
}

function hiddenReason(prepared: PreparedGeneration, id: string): string {
  const hidden = prepared.hidden.get(id);
  if (!hidden || hidden.ruleIndex === null) return "hidden · no include rule matched";
  return `hidden by include_models[${hidden.ruleIndex}] ${hidden.pattern ?? ""}`.trimEnd();
}

// Unprefixed models first, then slug order.
const slugOrder = (a: TableRow, b: TableRow) => {
  const aBare = !a.slug.includes("/");
  const bBare = !b.slug.includes("/");
  return aBare !== bBare ? (aBare ? -1 : 1) : a.slug < b.slug ? -1 : a.slug > b.slug ? 1 : 0;
};

/** Compact table rows: kept, hidden (with the deciding rule) and referenced-only entries. */
export function tableData(prepared: PreparedGeneration, projection: Projection, clientVersion: string): TableData {
  const rows: TableRow[] = [];
  let referencedOnly = 0;
  const ids = prepared.members.map((member) => member.id);
  const memberIDs = new Set(ids);
  for (const id of prepared.referencedBy.keys()) if (!memberIDs.has(id)) ids.push(id);
  for (const id of ids) {
    const kept = prepared.kept.has(id);
    const by = prepared.referencedBy.get(id);
    const entry = projection.entries.get(id);
    const processed = (kept || by !== undefined) && entry !== undefined;
    let status = kept ? "kept" : prepared.hidden.has(id) ? hiddenReason(prepared, id) : "pool";
    if (!kept && by) {
      status += ` · referenced by ${by.join(", ")}`;
      referencedOnly++;
    }
    rows.push({ slug: id, status, hidden: !kept, cells: processed ? COLUMNS.map(([, key]) => cell(entry, key)) : undefined });
  }
  rows.sort(slugOrder);
  return {
    generation: prepared.authority.generation,
    rows,
    kept: prepared.kept.size,
    hidden: prepared.hidden.size,
    referencedOnly,
    dataVersions: prepared.catalog.dataVersions,
    clientVersion,
  };
}

const ESCAPES: Readonly<Record<string, string>> = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
export const escapeHTML = (text: string) => text.replace(/[&<>"']/g, (char) => ESCAPES[char]!);

export interface TablePage {
  table?: TableData;
  /** Error text shown instead of the table. */
  error?: string;
  notice?: { level: "success" | "warning"; text: string };
  lastSuccess?: number;
  nextScheduled?: number;
  renderedAt: number;
}

const iso = (ms: number) => new Date(ms).toISOString().replace(/\.\d{3}Z$/, "Z");

export function renderTable(page: TablePage): string {
  const parts: string[] = [];
  const { table } = page;
  const versions = table?.dataVersions;
  const meta = table
    ? `${table.rows.length} models · kept ${table.kept} · hidden ${table.hidden} · referenced-only ${table.referencedOnly} · generation ${table.generation} · client_version=${table.clientVersion} · @opencode-ai/models ${versions?.modelsDevVersion ?? "未使用"} (snapshot ${versions?.modelsDevGeneratedAt ?? "未知"}) · modelparams ${versions?.modelparamsVersion ?? "未使用"} · ${iso(page.renderedAt)}`
    : iso(page.renderedAt);
  parts.push(`<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="utf-8">
<title>models-table · catalog diagnostics</title>
<style>
  body { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; margin: 16px; }
  h1 { font-size: 18px; margin: 0 0 8px; }
  #meta { color: #555; font-size: 12px; margin-bottom: 12px; }
  #refresh { margin: 0 0 12px; }
  #notice { border: 1px solid; margin-bottom: 12px; padding: 8px; white-space: pre-wrap; }
  #notice.success { background: #edf7ed; border-color: #2e7d32; color: #1b5e20; }
  #notice.warning { background: #fff4e5; border-color: #ed6c02; color: #8a4200; }
  table { border-collapse: collapse; font-size: 12px; }
  th, td { border: 1px solid #888; padding: 4px 8px; text-align: left; vertical-align: top; }
  th { background: #eee; position: sticky; top: 0; }
  td.null { color: #b00020; font-style: italic; }
  tr.hidden td { color: #777; }
  tr:nth-child(even) td { background: #fafafa; }
  #err { color: #b00020; white-space: pre-wrap; }
</style>
</head>
<body>
<h1>models-table</h1>
<form id="refresh" method="post" action="/models-table/refresh"><button type="submit">强制刷新</button></form>
<div id="meta">${escapeHTML(meta)}</div>
<div id="refresh-times">最后成功刷新：${page.lastSuccess ? iso(page.lastSuccess) : "尚无成功刷新"} · 下次定时刷新：${page.nextScheduled ? iso(page.nextScheduled) : "未启动或已停止"}</div>
`);
  if (page.notice) parts.push(`<div id="notice" class="${page.notice.level}">${escapeHTML(page.notice.text)}</div>\n`);
  if (page.error !== undefined) {
    parts.push(`<div id="err">${escapeHTML(page.error)}</div>\n`);
  } else if (table) {
    parts.push(`<table id="tbl">\n  <thead><tr><th>slug</th><th>status</th>${COLUMNS.map(([label]) => `<th>${label}</th>`).join("")}</tr></thead>\n  <tbody>\n`);
    for (const row of table.rows) {
      const cells = row.cells
        ? row.cells.map((c) => `<td${c.missing ? ' class="null"' : ""}>${escapeHTML(c.text)}</td>`).join("")
        : `<td colspan="${COLUMNS.length}">not processed</td>`;
      parts.push(`  <tr${row.hidden ? ' class="hidden"' : ""}><td>${escapeHTML(row.slug)}</td><td>${escapeHTML(row.status)}</td>${cells}</tr>\n`);
    }
    parts.push("  </tbody>\n</table>\n");
  }
  parts.push("</body>\n</html>\n");
  return parts.join("");
}
