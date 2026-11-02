// Parity and golden tests on the production-shaped fixture (tests/parity-fixture.ts).
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { test } from "node:test";
import { prepare } from "../src/build.ts";
import type { Row } from "../src/json.ts";
import { harness, replyText, type Harness } from "./catalog-harness.ts";
import { aisixIDs, bareLogical, devin, EXTRA_CPA, fixtureHarness, HIDES, OAUTH, prefixed, routes, YAML, type Mode } from "./parity-fixture.ts";
import { defaultScriptText } from "./scripts.ts";

async function variant(t: Parameters<typeof harness>[0], forced: boolean): Promise<{ h: Harness; members: string[]; codex: string; passthrough: string[] }> {
  const h = await fixtureHarness(t, forced ? "forced" : "prefix");
  const standard = JSON.parse(await replyText(await h.service.catalog(""))) as { data: Array<{ id: string }> };
  const codex = await replyText(await h.service.catalog("1.0.0"));
  const generation = h.owner.current();
  assert.equal(generation.prefixMode, forced ? "forced" : "prefix");
  const members = standard.data.map((model) => model.id);
  const kept = new Set(members);
  return { h, members, codex, passthrough: generation.members.filter((m) => m.preserveNative && kept.has(m.id)).map((m) => m.id) };
}

test("parity: forced-prefix and default CPA listings give the same members, passthrough and Codex rows", async (t) => {
  const forced = await variant(t, true);
  const fresh = await variant(t, false);

  const images = prefixed.filter((id) => id.includes("/gpt-image-")).length;
  const shared = new Set([...prefixed, ...devin]);
  const aisixOnly = aisixIDs.filter((id) => !shared.has(id)).length;
  assert.equal(forced.members.length, 274 - images + 57 + aisixOnly + 2);
  assert.deepEqual(fresh.members, forced.members, "member set and order");
  assert.deepEqual(fresh.passthrough, forced.passthrough, "passthrough set");
  assert.equal(forced.passthrough.length, prefixed.filter((id) => OAUTH.some((p) => id.startsWith(`${p}/`)) && !id.includes("/gpt-image-")).length);
  assert.equal(fresh.codex, forced.codex, "Codex manifest bytes");

  // The default listing admits no bare or unprefixed slash CPA ID outside EXTRA_CPA.
  const admitted = fresh.h.owner.current().members.filter((m) => m.origin === "cpa").map((m) => m.id);
  const prefixes = new Set(routes.map((route) => route.prefix));
  for (const id of admitted) assert.ok(prefixes.has(id.slice(0, id.indexOf("/"))) || id.startsWith("devin/"), id);
  assert.equal(admitted.length, 331);
  // Bare AISIX logical IDs that CPA now lists bare stay AISIX members without a CPA row (D5).
  const rows = new Map((JSON.parse(fresh.codex) as { models: Row[] }).models.map((model) => [model.slug as string, model]));
  for (const id of bareLogical.slice(0, 5)) assert.equal(rows.get(id)?.context_window, undefined, id);
});

// ---------------------------------------------------------------------------
// Golden membership output (lua-catalog-membership task 1.1): the kept members, the standard
// list bytes and the Codex manifest bytes of this fixture in all three prefix modes. Recorded from
// the union-catalog-membership TS rule; regenerate with UPDATE_GOLDEN=1.

interface Snapshot {
  members: Array<{ id: string; origin: string; preserveNative: boolean }>;
  standard: string;
  codex: string;
}

const GOLDEN = new URL("./fixtures/membership-golden.json", import.meta.url);

async function snapshot(t: Parameters<typeof harness>[0], mode: Mode, yaml: string, script?: string): Promise<Snapshot> {
  const h = await fixtureHarness(t, mode, { yaml, script });
  const standard = await replyText(await h.service.catalog(""));
  const codex = await replyText(await h.service.catalog("1.0.0"));
  assert.equal(h.owner.current().prefixMode, mode);
  const prepared = await prepare(h.deps, h.owner.current());
  const members = prepared.members.filter((m) => prepared.kept.has(m.id)).map((m) => ({ id: m.id, origin: m.origin, preserveNative: m.preserveNative }));
  return { members, standard, codex };
}

test("golden: membership output in all prefix modes", async (t) => {
  const actual: Record<string, Snapshot> = {};
  for (const mode of ["forced", "prefix", "fallback"] as const) actual[mode] = await snapshot(t, mode, YAML);
  if (process.env.UPDATE_GOLDEN === "1") {
    await mkdir(new URL("./fixtures/", import.meta.url), { recursive: true });
    await writeFile(GOLDEN, `{\n${Object.entries(actual).map(([mode, value]) => `${JSON.stringify(mode)}: ${JSON.stringify(value)}`).join(",\n")}\n}\n`);
  }
  const golden = JSON.parse(await readFile(GOLDEN, "utf8")) as Record<string, Snapshot>;
  for (const mode of ["forced", "prefix", "fallback"] as const) {
    assert.deepEqual(actual[mode]!.members, golden[mode]!.members, `${mode}: members`);
    assert.equal(actual[mode]!.standard, golden[mode]!.standard, `${mode}: standard list bytes`);
    assert.equal(actual[mode]!.codex, golden[mode]!.codex, `${mode}: Codex manifest bytes`);
  }
});

test("default-script parity: hides moved into catalog.lua give the golden output in all prefix modes", async (t) => {
  const golden = JSON.parse(await readFile(GOLDEN, "utf8")) as Record<string, Snapshot>;
  // The production profile shape: EXTRA_CPA plus the include_models hides, no include_models.
  const yaml = YAML.replace(`  include_models: ["**", "!**/gpt-image-*"]\n`, "");
  assert.notEqual(yaml, YAML);
  for (const mode of ["forced", "prefix", "fallback"] as const) {
    const actual = await snapshot(t, mode, yaml, defaultScriptText(EXTRA_CPA, HIDES));
    assert.deepEqual(actual.members, golden[mode]!.members, `${mode}: members`);
    assert.equal(actual.standard, golden[mode]!.standard, `${mode}: standard list bytes`);
    assert.equal(actual.codex, golden[mode]!.codex, `${mode}: Codex manifest bytes`);
  }
});
