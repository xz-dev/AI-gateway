import assert from "node:assert/strict";
import { test } from "node:test";
import {
  AisixClient,
  AuthorityOwner,
  applyDecisions,
  inferredPrefixes,
  membershipCandidates,
  nativeRows,
  decodeAISIXModels,
  runtimeConflicts,
  uniqueIDs,
  type AuthorityGeneration,
  type Prefixes,
} from "../src/authority.ts";
import { runMembershipScript } from "../src/membership.ts";
import { CHANNEL_KINDS, type Channel } from "../src/cpa.ts";
import { HttpPool } from "../src/http.ts";
import { ReadCache } from "../src/read-cache.ts";
import { cpaClient, fakeCPA, serveCPA, type FakeCPA } from "./fake-cpa.ts";
import { captureLog, json, serve, sleep, type CapturedLog } from "./helpers.ts";
import { defaultScript, script } from "./scripts.ts";

interface Harness {
  owner: AuthorityOwner;
  fake: FakeCPA;
  log: CapturedLog;
  advance(ms: number): void;
}

interface HarnessOptions {
  aisix?: string;
  staticSlugs?: string[];
  /** The default script's EXTRA_CPA list. */
  extraCpa?: string[];
  /** A script text replacing the default one. */
  script?: string;
  concurrency?: number;
  aisixTimeoutMs?: number;
}

async function harness(t: { after(fn: () => unknown): void }, fake: FakeCPA, options: HarnessOptions = {}): Promise<Harness> {
  const server = await serveCPA(fake);
  t.after(() => server.close());
  const log = captureLog();
  let now = Date.now();
  const cache = new ReadCache(undefined, log, () => now);
  const { client } = cpaClient(server.url, { cache, log, concurrency: options.concurrency });
  const aisix = options.aisix ? new AisixClient(options.aisix, "aisix-token", client.pool, cache) : undefined;
  const owner = new AuthorityOwner({
    cpa: client,
    aisix,
    staticSlugs: options.staticSlugs ?? [],
    script: options.script === undefined ? defaultScript(options.extraCpa) : script(options.script),
    cpaTimeoutMs: 5_000,
    aisixTimeoutMs: options.aisixTimeoutMs ?? 5_000,
    log,
    clock: () => now,
  });
  return { owner, fake, log, advance: (ms) => (now += ms) };
}

const cpaMembers = (generation: AuthorityGeneration) => generation.members.filter((member) => member.origin === "cpa");
const ids = (generation: AuthorityGeneration) => cpaMembers(generation).map((model) => model.id).sort();
const preserved = (generation: AuthorityGeneration) => cpaMembers(generation).filter((m) => m.preserveNative).map((m) => m.id).sort();

function oauth(routes: Record<string, unknown>) {
  return (req: { url?: string }, res: Parameters<NonNullable<FakeCPA["identity"]>>[1]) => {
    const url = new URL(req.url!, "http://cpa");
    const key = url.pathname === "/v0/management/auth-files/models" ? `models?${url.searchParams.get("name")}` : url.pathname.replace("/v0/management/", "");
    if (!(key in routes)) return json(res, {}, 404);
    json(res, routes[key]);
  };
}

const models = (...ids: string[]) => ({ models: ids.map((id) => ({ id })) });
const manifest = (...ids: string[]) => JSON.stringify({ models: ids.map((slug) => ({ slug })) });

// Shape of CPA's default listing: a prefixed account or channel registers `x` and `P/x`.
const DEFAULT_IDS = [
  "codex/gpt-6.1-sol", "gpt-6.1-sol",
  "supergrok/grok-4.7", "grok-4.7",
  "xl/claude-opus-5-5", "claude-opus-5-5",
  "xl/deepseek/deepseek-v4.1-flash", "deepseek/deepseek-v4.1-flash",
  "openrouter/openrouter/auto", "openrouter/auto",
  "devin/claude-opus-5-5", "devin/glm-5-3",
];
const PREFIX_CHANNELS = JSON.stringify({ "openai-compatibility": [
  { prefix: "xl", "base-url": "https://x", "api-key-entries": [{ "auth-index": "a" }], models: [{ name: "claude-opus-5-5" }, { name: "deepseek/deepseek-v4.1-flash" }] },
  { prefix: "openrouter", "base-url": "https://o", "api-key-entries": [{ "auth-index": "o" }], models: [{ name: "openrouter/auto" }] },
] });
const accounts = (forced: boolean) => oauth({
  "force-model-prefix": { "force-model-prefix": forced },
  "auth-files": { files: [{ name: "codex.json", provider: "codex" }, { name: "grok.json", provider: "xai" }, { name: "devin.json", provider: "devin" }, { name: "off.json", provider: "codex", disabled: true }] },
  "models?codex.json": forced ? models("codex/gpt-6.1-sol") : models("codex/gpt-6.1-sol", "gpt-6.1-sol"),
  "models?grok.json": forced ? models("supergrok/grok-4.7") : models("supergrok/grok-4.7", "grok-4.7"),
  "models?devin.json": models("devin/claude-opus-5-5", "devin/glm-5-3"),
});

test("prefix rule: CPA-prefixed IDs and EXTRA_CPA matches are admitted; bare and unprefixed slash originals are not", async (t) => {
  const h = await harness(t, fakeCPA({ native: manifest(...DEFAULT_IDS), channels: PREFIX_CHANNELS, identity: accounts(false) }), { extraCpa: ["devin/*"] });
  const { generation, errors } = await h.owner.refresh();
  assert.deepEqual(errors, []);
  assert.equal(generation.prefixMode, "prefix");
  assert.deepEqual(cpaMembers(generation).map((m) => m.id), [
    "codex/gpt-6.1-sol",
    "supergrok/grok-4.7",
    "xl/claude-opus-5-5",
    "xl/deepseek/deepseek-v4.1-flash",
    // The openrouter channel's own original starts with its prefix: admitted bare too (accepted edge).
    "openrouter/openrouter/auto",
    "openrouter/auto",
    "devin/claude-opus-5-5",
    "devin/glm-5-3",
  ]);
  // OAuth registrations under an inferred prefix pass through; key-channel and EXTRA_CPA IDs compile.
  assert.deepEqual(preserved(generation), ["codex/gpt-6.1-sol", "supergrok/grok-4.7"]);
  assert.equal(generation.prefixFallbackCount, 0);
  assert.match(h.log.text(), /follows the CPA prefix rule/);
  const reads = new Set(h.fake.calls.filter((call) => call.includes("/v0/management/")).map((call) => call.split(" ")[1]!.split("?")[0]!.slice("/v0/management/".length)));
  for (const kind of CHANNEL_KINDS) reads.delete(kind);
  assert.deepEqual([...reads].sort(), ["auth-files", "auth-files/models", "force-model-prefix"], "only channel, setting and prefix reads");
  assert.ok(!h.fake.calls.some((call) => call.includes("off.json")), "disabled accounts are not read");
});

test("without EXTRA_CPA an account without a prefix contributes nothing", async (t) => {
  const h = await harness(t, fakeCPA({ native: manifest(...DEFAULT_IDS), channels: PREFIX_CHANNELS, identity: accounts(false) }));
  const { generation } = await h.owner.refresh();
  assert.ok(!ids(generation).some((id) => id.startsWith("devin/")));
  assert.ok(!ids(generation).includes("deepseek/deepseek-v4.1-flash"));
  assert.ok(!ids(generation).includes("gpt-6.1-sol"));
});

test("OAuth prefixes are inferred only from paired P/x and x registrations", () => {
  assert.deepEqual([...inferredPrefixes(["supergrok/grok-4.7", "grok-4.7", "devin/claude-opus-5-5", "p/vendor/m", "vendor/m"])].sort(), ["p", "supergrok"]);
  assert.deepEqual([...inferredPrefixes(["devin/claude-opus-5-5", "devin/glm-5-3"])], []);
});

test("EXTRA_CPA: the last match decides and cannot hide prefixed IDs; static slugs opt out of passthrough", async () => {
  const rows = ["codex/gpt-6.1-sol", "devin/model_private_2", "devin/glm-5-3", "bare", "codex/gpt-6.1-sol"].map((slug) => ({ slug }));
  const prefixes: Prefixes = { mode: "prefix", prefixes: new Set(["codex"]), oauth: new Set(["codex/gpt-6.1-sol"]), texts: [] };
  const decide = async (extra: string[], statics: string[]) => {
    const input = membershipCandidates(rows, prefixes, [], statics);
    return applyDecisions(input, (await runMembershipScript(defaultScript(extra), input.candidates)).decisions);
  };
  const { members, rejected } = await decide(["devin/*", "!devin/model_private_*", "!codex/*"], []);
  assert.deepEqual(members.map((m) => [m.id, m.preserveNative]), [["codex/gpt-6.1-sol", true], ["devin/glm-5-3", false]]);
  assert.deepEqual(rejected.map((r) => [r.id, r.reason]), [["devin/model_private_2", "CPA ID without a CPA prefix"], ["bare", "CPA ID without a CPA prefix"]]);
  const statics = (await decide([], ["codex/gpt-6.1-sol"])).members;
  assert.deepEqual(statics.map((m) => [m.id, m.origin, m.preserveNative]), [["codex/gpt-6.1-sol", "cpa", false]]);
});

test("compatibility mode: forced prefixes admit every slash ID; OAuth IDs outside EXTRA_CPA pass through", async (t) => {
  const forced = ["codex/gpt-6.1-sol", "supergrok/grok-4.7", "xl/claude-opus-5-5", "xl/deepseek/deepseek-v4.1-flash", "openrouter/openrouter/auto", "devin/claude-opus-5-5", "devin/glm-5-3"];
  const h = await harness(t, fakeCPA({ native: manifest(...forced), channels: PREFIX_CHANNELS, identity: accounts(true) }), { extraCpa: ["devin/*"] });
  const { generation } = await h.owner.refresh();
  assert.equal(generation.prefixMode, "forced");
  assert.deepEqual(cpaMembers(generation).map((m) => m.id), forced);
  assert.deepEqual(preserved(generation), ["codex/gpt-6.1-sol", "supergrok/grok-4.7"]);
  assert.match(h.log.text(), /compatibility mode/);
});

test("fallback: a failed prefix read admits slash IDs as CPA returned them and no bare IDs", async (t) => {
  for (const failing of ["force-model-prefix", "auth-files", "models?grok.json"]) {
    const routes = accounts(false);
    const h = await harness(t, fakeCPA({
      native: `{"models":[{"slug":"vendor/m","id":"unchanged","null":null},{"slug":"bare"},{"slug":"devin/glm-5-3"},{"slug":"codex/gpt-6.1-sol"}]}`,
      identity: (req, res) => {
        const url = new URL(req.url!, "http://cpa");
        const key = url.pathname === "/v0/management/auth-files/models" ? `models?${url.searchParams.get("name")}` : url.pathname.replace("/v0/management/", "");
        if (key === failing) return json(res, {}, 403);
        routes(req, res);
      },
    }), { extraCpa: ["devin/*"] });
    const { generation } = await h.owner.refresh();
    assert.equal(generation.prefixMode, "fallback", failing);
    assert.deepEqual(ids(generation), ["codex/gpt-6.1-sol", "devin/glm-5-3", "vendor/m"], failing);
    assert.deepEqual(preserved(generation), ["codex/gpt-6.1-sol", "devin/glm-5-3", "vendor/m"], failing);
    assert.equal(generation.prefixFallbackCount, 3);
    assert.equal(JSON.stringify(nativeRows(generation)[cpaMembers(generation).find((m) => m.id === "vendor/m")!.index!]), `{"slug":"vendor/m","id":"unchanged","null":null}`);
    assert.match(h.log.text(), /prefix discovery failed/);
  }
});

test("prefix reads: 503 reuses the last success; denial falls back and is not resurrected by a later 503", async (t) => {
  let status = 200;
  let calls = 0;
  const routes = accounts(false);
  const h = await harness(t, fakeCPA({
    native: manifest("codex/gpt-6.1-sol", "gpt-6.1-sol", "vendor/m"),
    identity: (req, res) => {
      if (req.url!.startsWith("/v0/management/force-model-prefix")) {
        calls++;
        if (status !== 200) return json(res, {}, status);
      }
      routes(req, res);
    },
  }));
  assert.deepEqual(ids((await h.owner.refresh()).generation), ["codex/gpt-6.1-sol"]);
  await h.owner.refresh();
  assert.equal(calls, 1, "fresh prefix input not reused");
  status = 503;
  h.advance(365 * 24 * 3_600_000);
  assert.equal((await h.owner.refresh()).generation.prefixMode, "prefix", "eligible stale input lost");
  status = 401;
  h.advance(10 * 60_000);
  assert.deepEqual(ids((await h.owner.refresh()).generation), ["codex/gpt-6.1-sol", "vendor/m"]);
  status = 503;
  h.advance(10 * 60_000);
  assert.equal((await h.owner.refresh()).generation.prefixMode, "fallback", "denied input resurrected on a later 503");
});

test("a forced refresh bypasses the prefix and setting reads once", async (t) => {
  const fake = fakeCPA({ native: manifest("codex/gpt-6.1-sol", "gpt-6.1-sol"), identity: accounts(false) });
  const h = await harness(t, fake);
  await h.owner.refresh();
  const reads = () => fake.calls.filter((call) => /force-model-prefix|auth-files/.test(call)).length;
  const before = reads();
  assert.equal(before, 5, "setting, list and three enabled accounts");
  await h.owner.refresh();
  assert.equal(reads(), before, "fresh prefix inputs are reused");
  await h.owner.refresh({ bypass: true });
  assert.equal(reads(), 2 * before);
});

test("incomplete channel discovery falls back, also from cache", async (t) => {
  const h = await harness(t, fakeCPA({
    native: `{"models":[{"slug":"vendor/m","id":"unchanged","null":null},{"slug":"p/vendor/m"},{"slug":"bare"}]}`,
    channels: `{"openai-compatibility":[{"prefix":"p","base-url":"https://x","api-key-entries":[{}],"models":[{"name":"vendor/m"}]},{"prefix":"unresolved","models":[{"name":"m"}]}]}`,
  }));
  for (let round = 0; round < 2; round++) {
    const { generation } = await h.owner.refresh({ bypass: round === 1 });
    assert.deepEqual(ids(generation), ["p/vendor/m", "vendor/m"]);
    assert.equal(generation.discoveryIncomplete, true);
    assert.equal(generation.prefixMode, "fallback");
  }
  assert.match(h.log.text(), /discovery incomplete/);
});

test("AISIX IDs are deduplicated by exact bytes in first-seen order", () => {
  assert.deepEqual(uniqueIDs(["a", "A", "a", "x/y", " b "]), ["a", "A", "x/y", " b "]);
  assert.deepEqual(decodeAISIXModels(`{"object":"list","data":[]}`), []);
  for (const body of [`{}`, `{"data":null}`, `{"data":[{"id":"  "}]}`, `{"data":[{"id":1}]}`, `not-json`]) assert.throws(() => decodeAISIXModels(body), body);
});

test("AISIX phase: authenticated read of the complete inventory; optional failures keep CPA output", async (t) => {
  let status = 200;
  let body = `{"object":"list","data":[{"id":"cpa-filtered"},{"id":"router-only"},{"id":"router-only"}]}`;
  let auth = "";
  const aisix = await serve((req, res) => {
    auth = req.headers.authorization ?? "";
    json(res, body, status);
  });
  t.after(() => aisix.close());
  const h = await harness(t, fakeCPA({ native: `{"models":[{"slug":"cpa-filtered/x"},{"slug":"p/m"},{"slug":"cpa-filtered"}]}`, forceModelPrefix: true }), { aisix: aisix.url });
  let { generation } = await h.owner.refresh();
  assert.equal(auth, "Bearer aisix-token");
  // Every AISIX ID, including one CPA also lists bare (no longer subtracted).
  assert.deepEqual(generation.aisixIDs, ["cpa-filtered", "router-only"]);
  assert.equal(generation.aisix, "usable");

  // Valid empty inventory replaces the old one.
  body = `{"object":"list","data":[]}`;
  h.advance(10 * 60_000);
  ({ generation } = await h.owner.refresh());
  assert.deepEqual(generation.aisixIDs, []);
  assert.equal(generation.aisix, "empty");

  // 503 reuses the last valid inventory.
  body = `{"object":"list","data":[{"id":"router-only"}]}`;
  h.advance(10 * 60_000);
  await h.owner.refresh();
  h.advance(10 * 60_000);
  status = 503;
  ({ generation } = await h.owner.refresh());
  assert.deepEqual(generation.aisixIDs, ["router-only"]);

  // 401 fails the optional phase only and is not resurrected by a later 503.
  h.advance(10 * 60_000);
  status = 401;
  let outcome = await h.owner.refresh();
  assert.equal(outcome.generation.aisix, "failed");
  assert.equal(outcome.generation.cpa, "usable");
  assert.deepEqual(outcome.generation.aisixIDs, []);
  assert.deepEqual(ids(outcome.generation), ["cpa-filtered/x", "p/m"], "an AISIX failure keeps CPA membership");
  status = 503;
  outcome = await h.owner.refresh();
  assert.equal(outcome.generation.aisix, "failed");
});

test("AISIX timeout without cache omits the supplement; pool capacity 1 cannot starve CPA", async (t) => {
  const slow = await serve(async (_req, res) => {
    await sleep(300);
    json(res, `{"data":[{"id":"late"}]}`);
  });
  t.after(() => slow.close());
  const h = await harness(t, fakeCPA({ native: `{"models":[{"slug":"p/m"}]}`, forceModelPrefix: true }), { aisix: slow.url, concurrency: 1, aisixTimeoutMs: 50 });
  const outcome = await h.owner.refresh();
  assert.equal(outcome.generation.cpa, "usable");
  assert.deepEqual(ids(outcome.generation), ["p/m"]);
  assert.equal(outcome.generation.aisix, "failed");
  assert.deepEqual(outcome.generation.aisixIDs, []);
});

test("disabled AISIX makes no request", async (t) => {
  const h = await harness(t, fakeCPA({ native: `{"models":[{"slug":"m"}]}` }));
  const { generation, errors } = await h.owner.refresh();
  assert.equal(generation.aisix, "disabled");
  assert.deepEqual(errors, []);
});

test("generations: unchanged inputs keep the generation, changes publish atomically, invalid CPA is not resurrected", async (t) => {
  let block = false;
  let release: (() => void) | undefined;
  const aisix = await serve(async (_req, res) => {
    if (release === undefined && block) await new Promise<void>((resolve) => (release = resolve));
    json(res, `{"data":[]}`);
  });
  t.after(() => aisix.close());
  const fake = fakeCPA({ native: `{"models":[{"slug":"p/old"}]}`, forceModelPrefix: true });
  const h = await harness(t, fake, { aisix: aisix.url });
  assert.equal(h.owner.current().generation, 0);
  const first = await h.owner.refresh();
  assert.equal(first.generation.generation, 1);
  h.advance(10 * 60_000);
  const same = await h.owner.refresh();
  assert.equal(same.published, false);
  // Same generation and members; only the held text is swapped for the read cache's current copy.
  assert.equal(same.generation.generation, first.generation.generation);
  assert.equal(same.generation.members, first.generation.members);
  assert.equal(h.owner.current(), same.generation);

  fake.native = `{"models":[{"slug":"p/new"}]}`;
  block = true;
  h.advance(10 * 60_000);
  const pending = h.owner.refresh();
  while (!release) await sleep(5);
  assert.deepEqual(ids(h.owner.current()), ["p/old"], "partial refresh escaped before publication");
  release();
  assert.deepEqual(ids((await pending).generation), ["p/new"]);
  assert.equal(h.owner.current().generation, 2);

  fake.native = `{"models":null}`;
  h.advance(10 * 60_000);
  let outcome = await h.owner.refresh();
  assert.equal(outcome.generation.cpa, "failed");
  assert.deepEqual(outcome.generation.members, []);
  fake.nativeStatus = 502;
  h.advance(10 * 60_000);
  outcome = await h.owner.refresh();
  assert.equal(outcome.generation.cpa, "failed", "transient failure resurrected invalidated authority");
});

test("refreshes coalesce; a forced refresh bypasses freshness once", async (t) => {
  const fake = fakeCPA({ native: `{"models":[{"slug":"m"}]}` });
  const h = await harness(t, fake);
  await Promise.all([h.owner.refresh(), h.owner.refresh(), h.owner.refresh()]);
  const nativeReads = () => fake.calls.filter((call) => call.startsWith("GET /v1/models")).length;
  assert.equal(nativeReads(), 1);
  await h.owner.refresh();
  assert.equal(nativeReads(), 1, "fresh authority must be reused");
  await Promise.all([h.owner.refresh({ bypass: true }), h.owner.refresh({ bypass: true }), h.owner.refresh()]);
  assert.equal(nativeReads(), 2);
});

test("runtime conflicts: empty and duplicate prefixes, custom pool collisions", () => {
  const ch = (prefix: string, type: Channel["type"] = "openai-compatibility"): Channel => ({ type, name: prefix, prefix, baseUrl: "", authIndex: "", models: [] });
  assert.equal(runtimeConflicts([ch(""), ch("a"), ch("a", "claude-api-key")], [], []).length, 2);
  assert.deepEqual(runtimeConflicts([ch("a")], ["a", "native", "free"], [{ id: "native/m", origin: "cpa", index: 0, preserveNative: false }]), [
    `custom channel "a" conflicts with CPA channel prefix`,
    `custom channel "native" conflicts with native manifest provider prefix`,
  ]);
});
