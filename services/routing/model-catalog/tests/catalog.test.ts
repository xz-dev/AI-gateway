import assert from "node:assert/strict";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { test } from "node:test";
import type { Row } from "../src/json.ts";
import { nativeRows } from "../src/authority.ts";
import { compareCatalogIDs } from "../src/build.ts";
import { bySlug, channels, codex, harness, replyText } from "./catalog-harness.ts";
import { json, serve, tempDir } from "./helpers.ts";

const levels = (...efforts: string[]) => efforts.map((effort) => ({ effort }));
const native = (slug: string, extra: Row = {}): Row => ({ slug, display_name: `CPA ${slug}`, ...extra });

test("sources ahead of @self override CPA values and display_name (Go parity)", async (t) => {
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["gpt-5"] }]),
    native: [native("codex/gpt-5", { context_window: 200000 })],
    providers: { openai: { "gpt-5": { name: "GPT-5", limit: { context: 400000, output: 128000 } } } },
    yaml: `metadata:
  chain: ["models.dev/openai/api_key", "@self"]
`,
  });
  const five = bySlug(await codex(h)).get("codex/gpt-5")!;
  assert.equal(five.context_window, 400000);
  assert.equal(five.display_name, "GPT-5");
});

test("@self first keeps CPA values; later entries fill gaps; rule overrides win; max_tokens is filled", async (t) => {
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["gpt-5", "gpt-6"] }]),
    native: [native("codex/gpt-5", { context_window: 200000, extra: { keep: true } }), native("codex/gpt-6")],
    providers: { openai: { "gpt-5": { name: "GPT-5", limit: { context: 400000, output: 128000 } }, "gpt-6": { limit: { context: 1000000, output: 64000 } } } },
    yaml: `metadata:
  chain: ["@self", "models.dev/openai/api_key"]
  models:
    - match: "codex/gpt-6"
      overrides: {max_tokens: 1000000, context_window: 300000}
`,
  });
  const models = bySlug(await codex(h));
  const five = models.get("codex/gpt-5")!;
  assert.equal(five.context_window, 200000);
  assert.equal(five.max_tokens, 128000);
  assert.equal(five.max_output_tokens, 128000);
  assert.equal(five.display_name, "CPA codex/gpt-5", "@self first keeps CPA's display name");
  assert.deepEqual(five.extra, { keep: true });
  const six = models.get("codex/gpt-6")!;
  assert.equal(six.max_tokens, 1000000);
  assert.equal(six.context_window, 300000);
  assert.equal(six.max_output_tokens, 64000);
  // The authority snapshot is never mutated by projections.
  const authority = h.owner.current();
  const row = nativeRows(authority)[authority.admitted.find((model) => model.id === "codex/gpt-6")!.index]!;
  assert.equal(row.max_tokens, undefined);
});

test("include_models: */** hides unprefixed models from both catalogs; hidden models are not processed", async (t) => {
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["gpt-5.6-sol", "gpt-image-1"] }]),
    native: [native("gpt-5.6-sol"), native("codex/gpt-5.6-sol"), native("codex/gpt-image-1")],
    upstream: { "https://codex.example/v1/models?client_version=1.0.0": { data: [{ id: "gpt-5.6-sol", context_length: 1 }] } },
    yaml: `metadata:
  chain: ["@self"]
  include_models: ["*/**", "!**/gpt-image-*"]
  models:
    - match: "**/gpt-image-*"
      chain: ["@cpa-upstream", "@self"]
`,
  });
  assert.deepEqual([...bySlug(await codex(h)).keys()], ["codex/gpt-5.6-sol"]);
  const standard = JSON.parse(await replyText(await h.service.catalog("")));
  assert.deepEqual(standard, { object: "list", data: [{ id: "codex/gpt-5.6-sol", object: "model" }] });
  assert.deepEqual(h.apiCalls, [], "the hidden image model's @cpa-upstream chain triggers no api-call");
});

test("a referenced hidden model is compiled for its referrer but absent from the catalogs", async (t) => {
  const h = await harness(t, {
    channels: channels([{ prefix: "vendor", models: ["y"] }]),
    native: [native("vendor/y", { context_window: 777 })],
    yaml: `metadata:
  chain: ["@self"]
  include_models: ["pool/**"]
static_models:
  - slug: pool/x
metadata_from:
  pool/x: vendor/y
`,
  });
  const models = bySlug(await codex(h));
  assert.deepEqual([...models.keys()], ["pool/x"]);
  assert.equal(models.get("pool/x")!.context_window, 777);
});

test("static inherit lists, custom pools, gpt-image override rule and reasoning-default cleanup", async (t) => {
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["gpt-5.6-sol", "gpt-image-2", "conflict"] }]),
    native: [
      native("codex/gpt-5.6-sol", { context_window: 400000, supported_reasoning_levels: levels("low", "high"), default_reasoning_level: "high" }),
      native("codex/gpt-image-2"),
      native("codex/conflict", { supported_reasoning_levels: levels("low", "high"), default_reasoning_level: "xhigh" }),
    ],
    providers: { anthropic: { "claude-x": { limit: { context: 1000000, output: 64000 } } } },
    yaml: `metadata:
  chain: ["@self"]
  models:
    - match: "**/gpt-image-*"
      overrides: {output_modalities: [image]}
    - match: "hidden/claude-x"
      chain: ["models.dev/anthropic/api_key"]
sync_max_context_window: true
custom_channels:
  hidden:
    models: [claude-x]
static_models:
  - slug: alias/a
    inherit: [codex/gpt-5.6-sol]
    overrides: {display_name: Alias A}
  - slug: claude
    inherit: [hidden/claude-x]
    overrides: {context_window: 500000, max_context_window: 1000000}
`,
  });
  const models = bySlug(await codex(h));
  assert.deepEqual([...models.keys()], ["claude", "alias/a", "codex/conflict", "codex/gpt-5.6-sol", "codex/gpt-image-2"]);
  assert.deepEqual(models.get("codex/gpt-image-2")!.output_modalities, ["image"]);
  assert.equal(models.get("codex/gpt-5.6-sol")!.output_modalities, undefined, "no built-in image special case");
  assert.equal("default_reasoning_level" in models.get("codex/conflict")!, false);
  const alias = models.get("alias/a")!;
  assert.equal(alias.display_name, "Alias A");
  assert.equal(alias.context_window, 400000);
  assert.equal(alias.max_context_window, 400000);
  assert.equal(alias.default_reasoning_level, "high");
  const claude = models.get("claude")!;
  assert.equal(claude.context_window, 500000);
  assert.equal(claude.max_context_window, 1000000, "explicit max_context_window is not synchronized");
  assert.equal(claude.max_output_tokens, 64000);
  assert.ok(!models.has("hidden/claude-x"), "pool entries are never emitted");
});

test("sync_max_context_window: enabled, per-model glob list, and off", async (t) => {
  const row = (slug: string) => native(slug, { context_window: 200000, max_context_window: 872000 });
  const setup = (sync: string, models = ["a", "keep-b"]) =>
    harness(t, {
      channels: channels([{ prefix: "xl", models }, { prefix: "aihub", models: ["c", "low", "none", "explicit"] }]),
      native: [
        row("xl/a"),
        row("xl/keep-b"),
        row("aihub/c"),
        native("aihub/low", { context_window: 500000, max_context_window: 128000 }),
        native("aihub/none", { context_window: 500000 }),
        row("aihub/explicit"),
      ],
      yaml: `metadata:\n  chain: ["@self"]\n  models:\n    - match: "aihub/explicit"\n      overrides: {max_context_window: 100}\n${sync}`,
    });
  const max = async (sync: string) => {
    const models = bySlug(await codex(await setup(sync)));
    return Object.fromEntries([...models].map(([slug, model]) => [slug, model.max_context_window]));
  };
  // Enabled: every entry equals context_window; an explicit override still wins.
  assert.deepEqual(await max("sync_max_context_window: true\n"), { "xl/a": 200000, "xl/keep-b": 200000, "aihub/c": 200000, "aihub/low": 500000, "aihub/none": 500000, "aihub/explicit": 100 });
  // Per model: only selected IDs are synchronized; the others keep larger upper bounds.
  assert.deepEqual(await max('sync_max_context_window: ["xl/**", "!xl/keep-*"]\n'), { "xl/a": 200000, "xl/keep-b": 872000, "aihub/c": 872000, "aihub/low": 500000, "aihub/none": 500000, "aihub/explicit": 100 });
  // Off (absent): upper bounds are only raised when missing or smaller.
  assert.deepEqual(await max(""), { "xl/a": 872000, "xl/keep-b": 872000, "aihub/c": 872000, "aihub/low": 500000, "aihub/none": 500000, "aihub/explicit": 100 });
});

test("membership comes from authority only; fallback rows are emitted unchanged and bare IDs are not admitted", async (t) => {
  const conflictRow = { slug: "oauth/route", default_reasoning_level: "xhigh", supported_reasoning_levels: levels("low"), unknown: null };
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["m"] }]),
    native: [native("codex/m"), native("my-alias"), conflictRow],
    upstream: { "https://codex.example/v1/models?client_version=1.0.0": { data: [{ id: "m" }, { id: "m-new", context_length: 5 }] } },
    yaml: `metadata:\n  chain: ["@cpa-upstream", "@self"]\n`,
  });
  // An unreadable prefix input keeps CPA's slash rows as unchanged records.
  h.fake.identity = (_req, res) => json(res, {}, 503);
  const models = await codex(h);
  const slugs = models.map((model) => model.slug);
  assert.ok(slugs.includes("codex/m"));
  assert.ok(!slugs.includes("my-alias"), "fallback admits no bare CPA ID");
  assert.ok(!slugs.includes("codex/m-new"), "channel-only models never appear");
  const preserved = models.find((model) => model.slug === "oauth/route");
  const priority = slugs.indexOf("oauth/route") + 1;
  assert.deepEqual(preserved, { ...conflictRow, priority }, "preserveNative rows keep CPA content as-is except priority");
  assert.deepEqual(Object.keys(preserved!), [...Object.keys(conflictRow), "priority"], "CPA field order is kept");
  // The generation's native row is shared by every projection and is never mutated.
  assert.equal((await codex(h, "2.0.0")).find((model) => model.slug === "oauth/route")!.priority, priority);
  assert.equal("priority" in nativeRows(h.owner.current())[2]!, false);
});

test("D1 order and D5: CPA rows win over AISIX; an AISIX bare ID never takes CPA's bare row", async (t) => {
  let status = 200;
  const aisix = await serve((_req, res) => json(res, { data: [{ id: "gpt-6.1-sol" }, { id: "codex/m" }, { id: "Router/Only" }, { id: "router/only" }, { id: "router/only" }] }, status));
  t.after(() => aisix.close());
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["m", "gpt-6.1-sol"] }]),
    native: [native("codex/m", { context_window: 1000 }), native("gpt-6.1-sol", { context_window: 999 }), native("codex/gpt-6.1-sol")],
    aisix: aisix.url,
    yaml: `metadata:\n  chain: ["@self"]\nstatic_models:\n  - slug: alias/a\n    inherit: [codex/m]\n  - slug: codex/m\n`,
  });
  const models = bySlug(await codex(h));
  assert.deepEqual([...models.keys()], ["gpt-6.1-sol", "Router/Only", "alias/a", "codex/gpt-6.1-sol", "codex/m", "router/only"]);
  assert.equal(models.get("codex/m")!.context_window, 1000, "the CPA-admitted row is the base for an ID in both sources");
  assert.equal(models.get("gpt-6.1-sol")!.context_window, undefined, "CPA's non-admitted bare row is never @self");
  assert.equal(models.get("gpt-6.1-sol")!.display_name, undefined);

  // An AISIX failure removes only AISIX members.
  status = 401;
  await h.owner.refresh({ bypass: true });
  assert.deepEqual([...bySlug(await codex(h)).keys()], ["alias/a", "codex/gpt-6.1-sol", "codex/m"]);
});

test("output order is bare IDs first, then the rest, each in code-point order", () => {
  const ids = ["z/a", "b", "\u{1F600}/x", "\uFFFD/x", "B", "A/b", "a", "a/b", "\u00E9"];
  assert.deepEqual(ids.sort(compareCatalogIDs), ["B", "a", "b", "\u00E9", "A/b", "a/b", "z/a", "\uFFFD/x", "\u{1F600}/x"]);
});

test("standard list and Codex manifest share the order; priority follows it on every row; members are unchanged", async (t) => {
  const aisix = await serve((_req, res) => json(res, { data: [{ id: "zeta" }, { id: "Alpha" }, { id: "codex/m" }] }));
  t.after(() => aisix.close());
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["m", "a"] }]),
    native: [native("codex/m", { priority: 7 }), native("devin/x", { priority: 343 }), native("codex/a")],
    aisix: aisix.url,
    yaml: `metadata:\n  chain: ["@self"]\nmembership:\n  cpa_include: ["devin/*"]\nstatic_models:\n  - slug: alias/a\n    inherit: [codex/m]\n  - slug: beta\n    inherit: [codex/a]\n`,
  });
  const expected = ["Alpha", "beta", "zeta", "alias/a", "codex/a", "codex/m", "devin/x"];
  const models = await codex(h);
  assert.deepEqual(models.map((model) => model.slug), expected);
  assert.deepEqual(models.map((model) => model.priority), [1, 2, 3, 4, 5, 6, 7], "CPA priorities are replaced, gaps-free");
  const reply = await h.service.catalog("");
  assert.deepEqual(JSON.parse(await replyText(reply)).data.map((entry: Row) => entry.id), expected);
});

test("@cpa-upstream reads only channels whose kept models need it; Ollama rows come from /api/show", async (t) => {
  const h = await harness(t, {
    channels: channels([{ prefix: "plain", models: ["a"] }, { prefix: "rich", models: ["b"] }, { prefix: "ollama-cloud", models: ["glm", "glm2"] }]),
    native: [native("plain/a"), native("rich/b"), native("ollama-cloud/glm"), native("ollama-cloud/glm2")],
    upstream: {
      "https://rich.example/v1/models?client_version=1.0.0": { data: [{ id: "b", context_length: 65536 }] },
      "https://ollama.com/api/show": { model_info: { "glm.context_length": 202752 } },
    },
    yaml: `metadata:
  chain: ["@self"]
  models:
    - match: "{rich,ollama-cloud}/**"
      chain: ["@cpa-upstream", "@self"]
channels:
  ollama-cloud:
    ollama_native_base_url: https://ollama.com
`,
  });
  const models = bySlug(await codex(h));
  assert.equal(models.get("rich/b")!.context_window, 65536);
  assert.equal(models.get("ollama-cloud/glm")!.context_window, 202752);
  assert.equal(models.get("plain/a")!.context_window, undefined);
  assert.deepEqual(h.apiCalls.sort(), [
    "GET https://rich.example/v1/models?client_version=1.0.0",
    'POST https://ollama.com/api/show {"model":"glm"}',
    'POST https://ollama.com/api/show {"model":"glm2"}',
  ]);
});

test("AISIX-only IDs are members and enriched through the chain", async (t) => {
  const aisix = await serve((_req, res) => json(res, { data: [{ id: "codex/m" }, { id: "router/only" }] }));
  t.after(() => aisix.close());
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["m"] }]),
    native: [native("codex/m")],
    providers: { openai: { only: { limit: { context: 123 } } } },
    aisix: aisix.url,
    yaml: `metadata:\n  chain: ["@self", "models.dev/openai/api_key"]\n`,
  });
  const models = bySlug(await codex(h));
  assert.deepEqual([...models.keys()], ["codex/m", "router/only"]);
  assert.equal(models.get("router/only")!.context_window, 123);
  assert.equal(models.get("router/only")!.id, "router/only");
});

test("worker crash keeps serving the previous generation; a later good build recovers", async (t) => {
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["a", "b"] }]),
    native: [native("codex/a")],
    providers: { openai: { a: { limit: { context: 1 } } } },
    yaml: `metadata:\n  chain: ["@self", "models.dev/openai/api_key"]\n`,
  });
  assert.deepEqual([...bySlug(await codex(h)).keys()], ["codex/a"]);
  const crash = join(await tempDir(), "crash.mjs");
  await writeFile(crash, "process.exit(3);\n");
  const good = h.deps.workerFile;
  h.deps.workerFile = crash;
  h.fake.native = JSON.stringify({ models: [native("codex/a"), native("codex/b")] });
  await h.owner.refresh({ bypass: true });
  assert.equal(h.owner.current().generation, 2);
  assert.deepEqual([...bySlug(await codex(h)).keys()], ["codex/a"], "previous generation keeps serving");
  assert.match(h.log.text(), /catalog preparation failed/);
  h.deps.workerFile = good;
  assert.deepEqual([...bySlug(await codex(h)).keys()], ["codex/a", "codex/b"]);
});

test("the compile worker has exited after a generation build", async (t) => {
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["a"] }]),
    native: [native("codex/a")],
    providers: { openai: { a: { limit: { context: 1 } } } },
    yaml: `metadata:\n  chain: ["models.dev/openai/api_key"]\n`,
  });
  let created = 0;
  let exited = 0;
  const onWorker = (worker: import("node:worker_threads").Worker) => {
    created++;
    worker.once("exit", () => exited++);
  };
  process.on("worker", onWorker);
  t.after(() => process.off("worker", onWorker));
  assert.equal(bySlug(await codex(h)).get("codex/a")!.context_window, 1);
  for (let i = 0; i < 50 && exited < created; i++) await new Promise((r) => setTimeout(r, 20));
  assert.equal(created, 1);
  assert.equal(exited, 1);
});

test("generation pinning: a new generation never serves a previous projection", async (t) => {
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["a", "b"] }]),
    native: [native("codex/a")],
    yaml: `metadata:\n  chain: ["@self"]\n`,
  });
  assert.equal((await codex(h)).length, 1);
  h.fake.native = JSON.stringify({ models: [native("codex/a"), native("codex/b")] });
  await h.owner.refresh({ bypass: true });
  assert.equal((await codex(h)).length, 2);
  const entries = [...h.service.outputs.entries()];
  assert.deepEqual(entries.map((entry) => [entry.key, entry.generation]).sort(), [["codex:1.0.0", 2], ["standard", 2]], "the old generation's files were replaced");
  const { readdir } = await import("node:fs/promises");
  for (let i = 0; i < 50 && (await readdir(h.service.outputs.dir)).length > 2; i++) await new Promise((r) => setTimeout(r, 10));
  assert.deepEqual((await readdir(h.service.outputs.dir)).sort(), entries.map((entry) => entry.path.slice(h.service.outputs.dir.length + 1)).sort(), "old generation files deleted");
});

test("CPA failure answers 502 without a catalog", async (t) => {
  const h = await harness(t, { channels: channels([]), native: [], yaml: "" });
  h.fake.nativeStatus = 500;
  const reply = await h.service.catalog("1.0.0");
  assert.equal(reply.status, 502);
  assert.match(await replyText(reply), /native_manifest_failed/);
});
