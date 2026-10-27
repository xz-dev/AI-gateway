import assert from "node:assert/strict";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { test } from "node:test";
import type { Row } from "../src/json.ts";
import { bySlug, channels, codex, harness } from "./catalog-harness.ts";
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
  // Native rows in the published generation are never mutated by projections.
  const row = h.owner.current().admitted.find((model) => model.id === "codex/gpt-6")!.row;
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
  const standard = JSON.parse((await h.service.catalog("")).body);
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
  assert.deepEqual([...models.keys()], ["codex/gpt-5.6-sol", "codex/gpt-image-2", "codex/conflict", "alias/a", "claude"]);
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

test("membership comes from authority only; preserveNative rows are emitted unchanged", async (t) => {
  const conflictRow = { slug: "oauth/route", default_reasoning_level: "xhigh", supported_reasoning_levels: levels("low"), unknown: null };
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["m"] }]),
    native: [native("codex/m"), native("my-alias"), conflictRow],
    upstream: { "https://codex.example/v1/models?client_version=1.0.0": { data: [{ id: "m" }, { id: "m-new", context_length: 5 }] } },
    yaml: `metadata:\n  chain: ["@cpa-upstream", "@self"]\n`,
  });
  // An unreadable identity list keeps unmatched prefixed rows as unchanged CPA records.
  h.fake.identity = (_req, res) => json(res, {}, 503);
  const models = await codex(h);
  const slugs = models.map((model) => model.slug);
  assert.ok(slugs.includes("codex/m"));
  assert.ok(slugs.includes("my-alias"), "admitted unprefixed models are listed");
  assert.ok(!slugs.includes("codex/m-new"), "channel-only models never appear");
  const preserved = models.find((model) => model.slug === "oauth/route");
  assert.deepEqual(preserved, conflictRow, "preserveNative rows keep CPA content as-is");
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

test("AISIX-only IDs are supplemented and enriched through the chain", async (t) => {
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
  assert.equal(h.service.cache.size, 1, "the old generation's projection was dropped");
});

test("CPA failure answers 502 without a catalog", async (t) => {
  const h = await harness(t, { channels: channels([]), native: [], yaml: "" });
  h.fake.nativeStatus = 500;
  const reply = await h.service.catalog("1.0.0");
  assert.equal(reply.status, 502);
  assert.match(reply.body, /native_manifest_failed/);
});
