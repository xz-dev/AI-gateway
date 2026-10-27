#!/usr/bin/env node
// Diffs two models-enricher catalog captures (Go vs TS shadow) for the cutover gate.
// Usage: node scripts/enricher-shadow-diff.mjs <baseDir> <candidateDir>
// Each dir holds codex.json (?client_version=…) and standard.json (no client_version).
// Output: membership differences first (cutover blockers unless explained), then
// per-field value differences grouped by field with counts and examples.
import { readFileSync } from "node:fs";
import { join } from "node:path";

const [baseDir, candDir] = process.argv.slice(2);
if (!baseDir || !candDir) {
  console.error("usage: enricher-shadow-diff.mjs <baseDir> <candidateDir>");
  process.exit(2);
}
const load = (dir, file) => JSON.parse(readFileSync(join(dir, file), "utf8"));
const canon = (value) => JSON.stringify(value, (_k, v) => (v && typeof v === "object" && !Array.isArray(v) ? Object.fromEntries(Object.entries(v).sort(([a], [b]) => (a < b ? -1 : 1))) : v));

function membership(label, baseIds, candIds) {
  const base = new Set(baseIds);
  const cand = new Set(candIds);
  const onlyBase = [...base].filter((id) => !cand.has(id)).sort();
  const onlyCand = [...cand].filter((id) => !base.has(id)).sort();
  console.log(`## ${label} membership: base ${base.size}, candidate ${cand.size}`);
  console.log(`- only in base (${onlyBase.length}): ${onlyBase.join(", ") || "-"}`);
  console.log(`- only in candidate (${onlyCand.length}): ${onlyCand.join(", ") || "-"}`);
  return onlyBase.length + onlyCand.length;
}

const baseCodex = load(baseDir, "codex.json").models;
const candCodex = load(candDir, "codex.json").models;
let blockers = membership("codex", baseCodex.map((m) => m.slug), candCodex.map((m) => m.slug));
blockers += membership("standard", load(baseDir, "standard.json").data.map((m) => m.id), load(candDir, "standard.json").data.map((m) => m.id));

const candBySlug = new Map(candCodex.map((m) => [m.slug, m]));
const fields = new Map(); // field -> { count, examples[] }
let identical = 0;
for (const base of baseCodex) {
  const cand = candBySlug.get(base.slug);
  if (!cand) continue;
  let same = true;
  for (const key of new Set([...Object.keys(base), ...Object.keys(cand)])) {
    const a = canon(base[key]);
    const b = canon(cand[key]);
    if (a === b) continue;
    same = false;
    const entry = fields.get(key) ?? { count: 0, examples: [] };
    entry.count++;
    if (entry.examples.length < 5) entry.examples.push(`${base.slug}: ${(a ?? "∅").slice(0, 120)} → ${(b ?? "∅").slice(0, 120)}`);
    fields.set(key, entry);
  }
  if (same) identical++;
}
console.log(`\n## codex field differences (${identical} shared models identical)`);
for (const [key, { count, examples }] of [...fields].sort((a, b) => b[1].count - a[1].count)) {
  console.log(`### ${key}: ${count} models`);
  for (const line of examples) console.log(`- ${line}`);
}
process.exitCode = blockers > 0 ? 1 : 0;
