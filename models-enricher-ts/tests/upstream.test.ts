import assert from "node:assert/strict";
import { test } from "node:test";
import type { Channel } from "../src/cpa.ts";
import { DiskCache } from "../src/disk-cache.ts";
import { ReadCache } from "../src/read-cache.ts";
import { joinURL, parseClaude, parseGemini, parseOllamaShow, parseOpenAI, readCpaUpstream, readCpaUpstreamChannels } from "../src/upstream.ts";
import { MANAGEMENT_KEY, cpaClient, fakeCPA, serveCPA, type FakeCPA } from "./fake-cpa.ts";
import { captureLog, json, tempDir } from "./helpers.ts";

const channel = (prefix: string, type: Channel["type"] = "openai-compatibility", baseUrl = `https://${prefix}/v1`): Channel => ({ type, name: `${prefix}-name`, prefix, baseUrl, authIndex: `${prefix}-auth`, models: [] });

async function setup(t: { after(fn: () => unknown): void }, apiCall: NonNullable<FakeCPA["apiCall"]>) {
  const server = await serveCPA(fakeCPA({ native: `{"models":[]}`, apiCall }));
  t.after(() => server.close());
  const log = captureLog();
  let now = Date.now();
  const cache = new ReadCache(await DiskCache.open(await tempDir(), log), log, () => now);
  const { client } = cpaClient(server.url, { cache, log });
  return { client, log, advance: (ms: number) => (now += ms) };
}

const options = { clientVersion: "1.3.0", channelTimeoutMs: 5_000 };

test("adapters map declared upstream fields to catalog names without overriding or dropping zero values", () => {
  const rows = parseOpenAI({
    data: [
      { id: "a", name: "Model A", context_length: 128000, top_provider: { max_completion_tokens: 0 }, architecture: { input_modalities: ["text", "image"], output_modalities: ["text"] } },
      { id: "b", context_window: 999, context_length: 1 },
      { name: "no-id" },
      { id: "a", name: "duplicate" },
    ],
  });
  assert.deepEqual([...rows.keys()], ["a", "b", "no-id"]);
  const a = rows.get("a")!;
  assert.equal(a.display_name, "Model A");
  assert.equal(a.context_window, 128000);
  assert.equal(a.max_output_tokens, 0);
  assert.deepEqual(a.input_modalities, ["text", "image"]);
  assert.equal(rows.get("b")!.context_window, 999);
  assert.deepEqual([...parseOpenAI({ data: [], models: [{ slug: "codex-style" }] }).keys()], ["codex-style"]);
  assert.equal(parseClaude({ data: [{ id: "claude-x", name: "Claude X" }] }).get("claude-x")!.display_name, "Claude X");
  const gemini = parseGemini({ models: [{ name: "models/gemini-x", displayName: "G", inputTokenLimit: 1048576, outputTokenLimit: 65536 }] }).get("gemini-x")!;
  assert.deepEqual([gemini.display_name, gemini.max_input_tokens, gemini.max_output_tokens], ["G", 1048576, 65536]);
  assert.throws(() => parseOpenAI({ data: "broken" }));
  assert.throws(() => parseOpenAI({ data: [1] }));
  assert.throws(() => parseOpenAI([]));
  const show = parseOllamaShow({ model_info: { "b.context_length": 2, "a.context_length": 1 }, details: { family: "x" } });
  assert.equal(show.context_window, 1);
  assert.deepEqual(show.details, { family: "x" });
});

test("joinURL drops a version segment the base already carries", () => {
  assert.equal(joinURL("https://openrouter.ai/api/v1", "/v1/models"), "https://openrouter.ai/api/v1/models");
  assert.equal(joinURL("https://g/v1beta/", "/v1beta/models"), "https://g/v1beta/models");
  assert.equal(joinURL("https://x", "v1/models"), "https://x/v1/models");
});

test("list reads use the adapter's credential template, default path, path override and auth index", async (t) => {
  const seen: Array<{ url: string; header: Record<string, string>; auth_index?: string }> = [];
  const { client } = await setup(t, (payload, res) => {
    seen.push(payload);
    json(res, { status_code: 200, body: { data: [{ id: "m", context_length: 4096 }], models: [{ name: "models/g" }] } });
  });
  const results = await readCpaUpstreamChannels(client, [
    { channel: channel("codex", "codex-api-key") },
    { channel: channel("over"), path: "/v1/models" },
    { channel: channel("claude", "claude-api-key", "https://claude") },
    { channel: channel("gem", "gemini-api-key", "https://gem/v1beta") },
  ], options);
  assert.deepEqual(seen.map((call) => call.url), [
    "https://codex/v1/models?client_version=1.3.0",
    "https://over/v1/models",
    "https://claude/v1/models",
    "https://gem/v1beta/models",
  ]);
  assert.deepEqual(seen[0]!.header, { Authorization: "Bearer $TOKEN$" });
  assert.deepEqual(seen[2]!.header, { "x-api-key": "$TOKEN$", "anthropic-version": "2023-06-01" });
  assert.equal(seen[0]!.auth_index, "codex-auth");
  assert.equal(results.get("codex")!.rows.get("m")!.context_window, 4096);
  assert.ok(results.get("gem")!.rows.has("g"));
});

test("a channel failure removes only that channel's rows and never logs credentials", async (t) => {
  const { client, log } = await setup(t, (payload, res) => {
    if (payload.url.startsWith("https://bad")) return json(res, { status_code: 500, body: "boom" });
    json(res, { status_code: 200, body: { data: [{ id: "m" }] } });
  });
  const results = await readCpaUpstreamChannels(client, [{ channel: channel("bad") }, { channel: channel("good") }], options);
  assert.equal(results.get("bad")!.failed, true);
  assert.equal(results.get("bad")!.rows.size, 0);
  assert.equal(results.get("good")!.failed, false);
  assert.match(log.text(), /channel fetch failed.*"prefix":"bad"/);
  assert.ok(!log.text().includes(MANAGEMENT_KEY));
});

test("cached rows: five-minute reuse, last-good fallback on failure or bad shape, valid empty replaces", async (t) => {
  let calls = 0;
  let reply: unknown = { status_code: 200, body: { data: [{ id: "m", context_length: 1 }] } };
  const env = await setup(t, (_payload, res) => {
    calls++;
    json(res, reply);
  });
  const read = () => readCpaUpstream(env.client, { channel: channel("p") }, options);
  await read();
  env.advance(5 * 60_000 - 1);
  await read();
  assert.equal(calls, 1);
  for (const failure of [{ status_code: 503 }, { status_code: 401, body: "denied" }, { status_code: 200, body: { data: "broken" } }]) {
    env.advance(10 * 60_000);
    reply = failure;
    const result = await read();
    assert.equal(result.failed, false);
    assert.equal(result.rows.get("m")!.context_window, 1);
  }
  env.advance(10 * 60_000);
  reply = { status_code: 200, body: { data: [] } };
  assert.equal((await read()).rows.size, 0);
  env.advance(10 * 60_000);
  reply = { status_code: 503 };
  assert.equal((await read()).rows.size, 0, "old rows resurrected after a valid empty response");
});

test("Ollama rows come from /api/show per lookup ID: deduplicated, per-model failure keeps other successes cached", async (t) => {
  const counts = new Map<string, number>();
  const { client } = await setup(t, (payload, res) => {
    assert.equal(payload.url, "https://p/api/show");
    assert.equal(payload.header["Content-Type"], "application/json");
    const model = JSON.parse(payload.data!).model as string;
    counts.set(model, (counts.get(model) ?? 0) + 1);
    if (model === "b") return json(res, { status_code: 503 });
    json(res, { status_code: 200, body: { model_info: { "family.context_length": 999 } } });
  });
  for (let round = 0; round < 2; round++) {
    const result = await readCpaUpstream(client, { channel: channel("p"), ollamaBaseUrl: "https://p", lookupIDs: ["a", "b", "a"] }, options);
    assert.equal(result.failed, true);
    assert.equal(result.rows.get("a")!.context_window, 999);
    assert.equal(result.rows.has("b"), false);
  }
  assert.deepEqual(Object.fromEntries(counts), { a: 1, b: 2 });
});
