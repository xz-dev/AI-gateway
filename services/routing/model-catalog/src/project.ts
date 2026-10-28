// Output projections: Codex manifest and standard list bodies, and the server-rendered models
// table (no JavaScript, refresh is a plain POST form). Bodies are published as files (outputs.ts);
// the table rows are one file, the page head and tail are rendered per request.
import { compareCatalogIDs, type PreparedGeneration, type Projection } from "./build.ts";
import type { Row } from "./json.ts";

export function codexBody(projection: Projection): string {
  return JSON.stringify({ models: projection.models });
}

/** Kept member IDs only; membership needs no metadata, so no projection build is involved. */
export function standardBody(prepared: PreparedGeneration): string {
  const data: Array<{ id: string; object: "model" }> = [];
  for (const member of prepared.members) if (prepared.kept.has(member.id)) data.push({ id: member.id, object: "model" });
  return JSON.stringify({ object: "list", data });
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

/** What the page head needs; kept in memory instead of the rows. */
export interface TableSummary {
  readonly generation: number;
  readonly rowCount: number;
  readonly kept: number;
  readonly hidden: number;
  readonly referencedOnly: number;
  readonly dataVersions: PreparedGeneration["catalog"]["dataVersions"];
  readonly clientVersion: string;
}

export interface TableData extends TableSummary {
  readonly rows: readonly TableRow[];
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

// Catalog output order; referenced-only pool rows are appended to the members, so the table re-sorts.
const slugOrder = (a: TableRow, b: TableRow) => compareCatalogIDs(a.slug, b.slug);

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
    rowCount: rows.length,
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
  table?: TableSummary;
  /** Error text shown instead of the table. */
  error?: string;
  notice?: { level: "success" | "warning"; text: string };
  lastSuccess?: number;
  nextScheduled?: number;
  renderedAt: number;
}

const iso = (ms: number) => new Date(ms).toISOString().replace(/\.\d{3}Z$/, "Z");

export const TABLE_TAIL = "</body>\n</html>\n";

/** Page up to the table; with `page.table` the published rows file follows, then TABLE_TAIL. */
export function renderTableHead(page: TablePage): string {
  const { table } = page;
  const versions = table?.dataVersions;
  const meta = table
    ? `${table.rowCount} models · kept ${table.kept} · hidden ${table.hidden} · referenced-only ${table.referencedOnly} · generation ${table.generation} · client_version=${table.clientVersion} · @opencode-ai/models ${versions?.modelsDevVersion ?? "未使用"} (snapshot ${versions?.modelsDevGeneratedAt ?? "未知"}) · modelparams ${versions?.modelparamsVersion ?? "未使用"} · ${iso(page.renderedAt)}`
    : iso(page.renderedAt);
  let head = `<!DOCTYPE html>
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
`;
  if (page.notice) head += `<div id="notice" class="${page.notice.level}">${escapeHTML(page.notice.text)}</div>\n`;
  if (page.error !== undefined) head += `<div id="err">${escapeHTML(page.error)}</div>\n`;
  return head;
}

/** The published table fragment (one file per table build). */
export function renderTableRows(table: TableData): string {
  const parts: string[] = [`<table id="tbl">\n  <thead><tr><th>slug</th><th>status</th>${COLUMNS.map(([label]) => `<th>${label}</th>`).join("")}</tr></thead>\n  <tbody>\n`];
  for (const row of table.rows) {
    const cells = row.cells
      ? row.cells.map((c) => `<td${c.missing ? ' class="null"' : ""}>${escapeHTML(c.text)}</td>`).join("")
      : `<td colspan="${COLUMNS.length}">not processed</td>`;
    parts.push(`  <tr${row.hidden ? ' class="hidden"' : ""}><td>${escapeHTML(row.slug)}</td><td>${escapeHTML(row.status)}</td>${cells}</tr>\n`);
  }
  parts.push("  </tbody>\n</table>\n");
  return parts.join("");
}

export function summarize(table: TableData): TableSummary {
  const { rows: _rows, ...summary } = table;
  return summary;
}
