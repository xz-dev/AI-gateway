// The membership stage end to end: script decisions, edited scripts, failure fallback, the empty
// catalog and the models table (design D2, D4, D8, D10).
import assert from "node:assert/strict";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { test } from "node:test";
import type { Row } from "../src/json.ts";
import { bySlug, channels, codex, harness, replyText, type Harness } from "./catalog-harness.ts";
import { json, serve, tempDir } from "./helpers.ts";
import { script } from "./scripts.ts";

const native = (slug: string, extra: Row = {}): Row => ({ slug, display_name: `CPA ${slug}`, context_window: 1000, ...extra });

async function stage(t: Parameters<typeof harness>[0], options: { script?: string; extraCpa?: string[]; aisix?: string[] } = {}): Promise<Harness> {
  const aisix = await serve((_req, res) => json(res, { data: (options.aisix ?? ["gpt-6.1-sol", "codex/gpt-5.3-codex-spark", "router/only"]).map((id) => ({ id })) }));
  t.after(() => aisix.close());
  return harness(t, {
    channels: channels([{ prefix: "codex", models: ["gpt-6.1-sol", "gpt-5.3-codex-spark"] }]),
    native: [native("codex/gpt-6.1-sol"), native("gpt-6.1-sol"), native("codex/gpt-5.3-codex-spark"), native("deepseek/deepseek-v4.1-flash"), native("devin/claude-opus-5-5"), native("my-alias")],
    upstream: { "https://codex.example/v1/models?client_version=1.0.0": { data: [{ id: "m-new" }] } },
    aisix: aisix.url,
    yaml: `metadata:\n  chain: ["@self"]\nstatic_models:\n  - slug: alias/a\n    inherit: [codex/gpt-6.1-sol]\n`,
    script: options.script,
    extraCpa: options.extraCpa,
  });
}

const standardIDs = async (h: Harness) => (JSON.parse(await replyText(await h.service.catalog(""))) as { data: Array<{ id: string }> }).data.map((entry) => entry.id);

test("default script: prefixed and extra-listed CPA IDs, every AISIX ID and statics; bare and unprefixed originals rejected", async (t) => {
  const h = await stage(t, { extraCpa: ["devin/*", "my-alias"] });
  assert.deepEqual(await standardIDs(h), ["gpt-6.1-sol", "my-alias", "alias/a", "codex/gpt-5.3-codex-spark", "codex/gpt-6.1-sol", "devin/claude-opus-5-5", "router/only"]);
  const generation = h.owner.current();
  const origin = new Map(generation.members.map((member) => [member.id, member.origin]));
  assert.equal(origin.get("gpt-6.1-sol"), "aisix", "a bare AISIX ID is an AISIX member, not CPA's bare row");
  assert.equal(origin.get("codex/gpt-5.3-codex-spark"), "cpa", "CPA wins on a shared ID");
  assert.equal(origin.get("my-alias"), "cpa");
  assert.deepEqual(generation.rejected.map((r) => [r.id, r.reason]), [["deepseek/deepseek-v4.1-flash", "CPA ID without a CPA prefix"]]);
  // Channel-only models are not candidates: the script never sees them.
  assert.ok(![...generation.members, ...generation.rejected].some((entry) => entry.id.endsWith("m-new")));
  const models = bySlug(await codex(h));
  assert.equal(models.get("gpt-6.1-sol")!.context_window, undefined, "no CPA @self for the AISIX member");
  assert.equal(models.get("my-alias")!.context_window, 1000, "chain-selected metadata from the CPA row");
});

test("edited scripts: admit every candidate; prefer AISIX on shared IDs", async (t) => {
  const all = await stage(t, { script: "function select(m) return true end" });
  assert.deepEqual((await standardIDs(all)).length, 8);
  assert.equal(all.owner.current().members.find((m) => m.id === "gpt-6.1-sol")!.origin, "cpa", "the CPA row is used for an ID CPA lists");
  assert.equal(bySlug(await codex(all)).get("gpt-6.1-sol")!.context_window, 1000);

  const aisixFirst = await stage(t, { script: 'function select(m) if m.sources.aisix then return true, {source = "aisix"} end return true end' });
  await aisixFirst.owner.refresh();
  const member = aisixFirst.owner.current().members.find((m) => m.id === "codex/gpt-5.3-codex-spark")!;
  assert.deepEqual([member.origin, member.preserveNative], ["aisix", false]);
  const models = await codex(aisixFirst);
  assert.equal(bySlug(models).get("codex/gpt-5.3-codex-spark")!.context_window, undefined, "no CPA @self record");
  assert.deepEqual(models.map((m) => m.slug), [...models.map((m) => m.slug as string)].sort((a, b) => (a.includes("/") === b.includes("/") ? (a < b ? -1 : 1) : a.includes("/") ? 1 : -1)), "output order is the catalog order");
});

const FAILURES: Array<[string, string, RegExp]> = [
  ["load", "function select(m) return true end x = (", /class":"load"/],
  ["runtime", 'function select(m) if m.id == "router/only" then error("boom") end return true end', /"candidate":"router\/only"/],
  ["invalid_return", "function select(m) return 1 end", /class":"invalid_return"/],
  ["instruction_limit", "function select(m) while true do end end", /class":"instruction_limit"/],
];

test("script failures keep the previous generation byte-identical, log one WARN and mark the table attempt failed", async (t) => {
  for (const [errorClass, text, logged] of FAILURES) {
    const h = await stage(t);
    await h.service.refreshPage();
    const standard = await replyText(await h.service.catalog(""));
    const manifest = await replyText(await h.service.catalog("1.0.0"));
    const generation = h.owner.current().generation;
    h.owner.options.script = script(`-- MARKER_SCRIPT_TEXT\n${text}`);
    h.fake.native = JSON.stringify({ models: [native("codex/gpt-6.1-sol"), native("codex/new")] });
    h.advance(10 * 60_000);
    const page = await replyText(await h.service.refreshPage());
    assert.equal(h.owner.current().generation, generation, errorClass);
    assert.equal(await replyText(await h.service.catalog("")), standard, `${errorClass}: standard list unchanged`);
    assert.equal(await replyText(await h.service.catalog("1.0.0")), manifest, `${errorClass}: Codex manifest unchanged`);
    assert.match(page, new RegExp(`刷新失败[\\s\\S]*membership script ${errorClass}`), errorClass);
    const table = await replyText(h.service.tablePage());
    assert.match(table, /<td>codex\/gpt-6\.1-sol<\/td>/, "the table of the generation still served stays");
    assert.match(table, new RegExp(`上次刷新失败[^<]*membership script ${errorClass}`));
    const warns = h.log.lines.filter((line) => line.includes("membership script failed"));
    assert.equal(warns.length, 1, errorClass);
    assert.match(warns[0]!, logged);
    assert.match(warns[0]!, /"level":"WARN"/);
    assert.doesNotMatch(h.log.text(), /MARKER_SCRIPT_TEXT/);
    // A scheduled collection failure (no table job) is shown too.
    h.advance(10 * 60_000);
    h.fake.native = JSON.stringify({ models: [native("codex/gpt-6.1-sol"), native("codex/newer")] });
    assert.equal((await h.owner.refresh()).scriptError?.errorClass, errorClass);
    assert.match(await replyText(h.service.tablePage()), new RegExp(`catalog\\.lua 执行失败[^<]*membership script ${errorClass}[^<]*仍在提供上一代目录`));
  }
});

test("a worker exit keeps the previous generation; a failure before the first generation keeps /readyz not ready and retries", async (t) => {
  const dir = await tempDir();
  const exiting = join(dir, "exit-worker.mjs");
  await writeFile(exiting, "process.exit(3);\n");
  const h = await stage(t);
  assert.equal((await h.owner.refresh()).published, true);
  h.owner.options.membership = { workerFile: exiting };
  const outcome = await h.owner.refresh({ bypass: true });
  assert.equal(outcome.scriptError?.errorClass, "worker_exit");
  assert.equal(outcome.generation.generation, 1);

  const cold = await stage(t, { script: "function select(m) error('x') end" });
  const first = await cold.owner.refresh();
  assert.equal(first.scriptError?.errorClass, "runtime");
  assert.deepEqual(cold.service.ready(), { ready: false, generation: 0 });
  cold.owner.options.script = script("function select(m) return true end");
  cold.advance(10 * 60_000);
  assert.equal((await cold.owner.refresh()).published, true, "the next cadence collection retries the script");
  assert.equal(cold.service.ready().ready, true);
  assert.equal(cold.owner.scriptFailure(), undefined);
});

test("a script that rejects every candidate publishes an empty catalog; the table lists every candidate hidden", async (t) => {
  const h = await stage(t, { script: 'function select(m) return false, "operator: none" end' });
  const page = await replyText(await h.service.refreshPage());
  assert.deepEqual(JSON.parse(await replyText(await h.service.catalog(""))), { object: "list", data: [] });
  assert.deepEqual(JSON.parse(await replyText(await h.service.catalog("1.0.0"))), { models: [] });
  assert.equal(h.service.ready().ready, true);
  assert.match(h.log.text(), /"level":"INFO","msg":"catalog.lua kept no candidate; publishing an empty catalog","candidates":8,"rejected":8/);
  assert.match(page, /刷新成功/);
  assert.doesNotMatch(page, /刷新失败/);
  assert.match(page, /kept 0 · hidden 0 · rejected by catalog\.lua 8/);
  assert.equal((page.match(/hidden by catalog\.lua: operator: none<\/td><td colspan="9">not processed/g) ?? []).length, 8);
});

test("models table: script-rejected rows show the reason, no metadata, a separate count, in catalog order", async (t) => {
  const h = await stage(t, { extraCpa: ["devin/*"] });
  const page = await replyText(await h.service.refreshPage());
  assert.match(page, /<td>my-alias<\/td><td>hidden by catalog\.lua: CPA ID without a CPA prefix<\/td><td colspan="9">not processed<\/td>/);
  assert.match(page, /rejected by catalog\.lua 2 ·/, h.log.text());
  const slugs = [...page.matchAll(/<tr[^>]*><td>([^<]+)<\/td>/g)].map((match) => match[1]!);
  assert.deepEqual(slugs, ["gpt-6.1-sol", "my-alias", "alias/a", "codex/gpt-5.3-codex-spark", "codex/gpt-6.1-sol", "deepseek/deepseek-v4.1-flash", "devin/claude-opus-5-5", "router/only"]);
  // The spec scenario: the bare CPA ID gpt-6.1-sol without an AISIX route.
  const bare = await stage(t, { aisix: [] });
  const barePage = await replyText(await bare.service.refreshPage());
  assert.match(barePage, /<td>gpt-6\.1-sol<\/td><td>hidden by catalog\.lua: CPA ID without a CPA prefix<\/td><td colspan="9">not processed<\/td>/);
  assert.match(barePage, /rejected by catalog\.lua 4 ·/);
  // The markings never change JSON contents: the standard list holds kept members only.
  assert.deepEqual(await standardIDs(h), ["gpt-6.1-sol", "alias/a", "codex/gpt-5.3-codex-spark", "codex/gpt-6.1-sol", "devin/claude-opus-5-5", "router/only"]);
});
