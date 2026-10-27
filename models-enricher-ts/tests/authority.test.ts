import assert from "node:assert/strict";
import { test } from "node:test";
import {
  AisixClient,
  AuthorityOwner,
  admitModels,
  decodeAISIXModels,
  runtimeConflicts,
  supplementalIDs,
  type AuthorityGeneration,
  type Identities,
} from "../src/authority.ts";
import type { Channel } from "../src/cpa.ts";
import { HttpPool } from "../src/http.ts";
import { ReadCache } from "../src/read-cache.ts";
import { cpaClient, fakeCPA, serveCPA, type FakeCPA } from "./fake-cpa.ts";
import { captureLog, json, serve, sleep, type CapturedLog } from "./helpers.ts";

interface Harness {
  owner: AuthorityOwner;
  log: CapturedLog;
  advance(ms: number): void;
}

async function harness(t: { after(fn: () => unknown): void }, fake: FakeCPA, options: { aisix?: string; staticSlugs?: string[]; concurrency?: number; aisixTimeoutMs?: number } = {}): Promise<Harness> {
  const server = await serveCPA(fake);
  t.after(() => server.close());
  const log = captureLog();
  let now = Date.now();
  const cache = new ReadCache(undefined, log, () => now);
  const { client } = cpaClient(server.url, { cache, log, concurrency: options.concurrency });
  const aisix = options.aisix ? new AisixClient(options.aisix, "aisix-token", client.pool, cache) : undefined;
  const owner = new AuthorityOwner({ cpa: client, aisix, staticSlugs: new Set(options.staticSlugs), cpaTimeoutMs: 5_000, aisixTimeoutMs: options.aisixTimeoutMs ?? 5_000, log });
  return { owner, log, advance: (ms) => (now += ms) };
}

const ids = (generation: AuthorityGeneration) => generation.admitted.map((model) => model.id).sort();
const preserved = (generation: AuthorityGeneration) => generation.admitted.filter((m) => m.preserveNative).map((m) => m.id).sort();

function oauth(routes: Record<string, unknown>) {
  return (req: { url?: string }, res: Parameters<NonNullable<FakeCPA["identity"]>>[1]) => {
    const url = new URL(req.url!, "http://cpa");
    const key = url.pathname === "/v0/management/auth-files/models" ? `models?${url.searchParams.get("name")}` : url.pathname.replace("/v0/management/", "");
    if (!(key in routes)) return json(res, {}, 404);
    json(res, routes[key]);
  };
}

test("OAuth routes need a positive identity match; unprefixed and namespaced originals are admitted", async (t) => {
  const h = await harness(t, fakeCPA({
    native: `{"models":[{"slug":"gpt-5.6-sol"},{"slug":"quick"},{"slug":"private/gpt-5.6-sol","unknown":null},{"slug":"vendor/gpt-5.6-sol"},{"slug":"new-dynamic","id":"unchanged"},{"slug":"other/unmatched"}]}`,
    identity: oauth({
      "auth-files": { files: [{ name: "fixture.json", provider: "codex" }] },
      "models?fixture.json": { models: [{ id: "gpt-5.6-sol" }, { id: "quick" }, { id: "private/gpt-5.6-sol" }, { id: "vendor/gpt-5.6-sol" }, { id: "new-dynamic" }, { id: "private/unavailable" }] },
      "model-definitions/codex": { models: [{ id: "gpt-5.6-sol" }, { id: "vendor/gpt-5.6-sol" }] },
      "oauth-model-alias": { "oauth-model-alias": { codex: [{ name: "gpt-5.6-sol", alias: "quick", fork: true }, { name: "quick", alias: "new-dynamic" }] } },
    }),
  }));
  const { generation, errors } = await h.owner.refresh();
  assert.deepEqual(errors, []);
  assert.deepEqual(ids(generation), ["gpt-5.6-sol", "new-dynamic", "private/gpt-5.6-sol", "quick", "vendor/gpt-5.6-sol"]);
  // The OAuth-only route keeps its exact CPA record.
  assert.deepEqual(preserved(generation), ["private/gpt-5.6-sol"]);
  assert.equal(generation.identityFallbackCount, 0);
  assert.doesNotMatch(h.log.text(), /identity unavailable/);
});

test("a registered namespaced OAuth original is unprefixed; an unregistered one is dropped", async (t) => {
  const h = await harness(t, fakeCPA({
    native: `{"models":[{"slug":"devin/swe-2","context_window":1000},{"slug":"devin/unregistered"}]}`,
    identity: oauth({
      "auth-files": { files: [{ name: "devin.json", provider: "devin" }] },
      "models?devin.json": { models: [{ id: "devin/swe-2" }] },
      "model-definitions/devin": { models: [{ id: "devin/swe-2" }, { id: "devin/unregistered" }] },
      "oauth-model-alias": { "oauth-model-alias": {} },
    }),
  }));
  const { generation } = await h.owner.refresh();
  assert.deepEqual(ids(generation), ["devin/swe-2"]);
  assert.deepEqual(preserved(generation), []);
});

test("channel declarations qualify exact routes, keep repeated namespaces and never revive undeclared models", async (t) => {
  const h = await harness(t, fakeCPA({
    native: `{"models":[{"slug":"nvidia/nemotron"},{"slug":"nvidia/nvidia/nemotron"},{"slug":"vendor/named"},{"slug":"hub/vendor/named"},{"slug":"vendor/original"},{"slug":"hub/missing"},{"slug":"opaque","id":"original-id"}]}`,
    channels: JSON.stringify({ "openai-compatibility": [
      { prefix: "nvidia", "base-url": "https://x/v1", "api-key-entries": [{ "auth-index": "k" }], models: [{ name: "nvidia/nemotron" }] },
      { prefix: "hub", "base-url": "https://x", "api-key-entries": [{}], models: [{ name: "vendor/original", alias: "vendor/named" }, { name: "unavailable" }] },
      { prefix: "vendor", "base-url": "https://x", "api-key-entries": [{}], models: [{ name: "named" }] },
    ] }),
  }));
  const { generation } = await h.owner.refresh();
  assert.deepEqual(ids(generation), ["hub/vendor/named", "nvidia/nvidia/nemotron", "opaque", "vendor/named"]);
  assert.equal(generation.channels.length, 3);
});

test("force-model-prefix OAuth registrations match definitions and aliases without bare copies", async (t) => {
  const h = await harness(t, fakeCPA({
    native: `{"models":[{"slug":"vendor/model"},{"slug":"named"},{"slug":"session/vendor/model","n":9007199254740993},{"slug":"session/named"},{"slug":"session/alien"},{"slug":"other/vendor/model"},{"slug":"session/unknown"},{"slug":"beta/alien"}]}`,
    identity: oauth({
      "auth-files": { files: [{ name: "a.json", provider: "codex" }, { name: "b.json", provider: "codex" }] },
      "models?a.json": { models: [{ id: "session/vendor/model" }, { id: "session/named" }, { id: "session/alien" }, { id: "session/unknown" }, { id: "session/not-public" }] },
      "models?b.json": { models: [{ id: "beta/alien" }] },
      "model-definitions/codex": { models: [{ id: "vendor/model" }, { id: "alien" }, { id: "not-public" }] },
      "oauth-model-alias": { "oauth-model-alias": { codex: [{ name: "vendor/model", alias: "named" }] } },
    }),
  }));
  const { generation } = await h.owner.refresh();
  assert.deepEqual(ids(generation), ["beta/alien", "named", "session/alien", "session/named", "session/vendor/model"]);
  const row = generation.admitted.find((m) => m.id === "session/vendor/model")!.row;
  assert.equal(JSON.stringify(row), `{"slug":"session/vendor/model","n":9007199254740993}`);
});

test("identity reads: 503 reuses the last success, denial or bad shape preserves the CPA baseline and is not resurrected", async (t) => {
  for (const code of [503, 401, 403, 200]) {
    let status = 200;
    let badShape = false;
    let calls = 0;
    const h = await harness(t, fakeCPA({
      native: `{"models":[{"slug":"vendor/m","id":"unchanged","null":null},{"slug":"p/vendor/m"}]}`,
      channels: `{"openai-compatibility":[{"prefix":"p","base-url":"https://x","api-key-entries":[{}],"models":[{"name":"vendor/m"}]}]}`,
      identity: (_req, res) => {
        calls++;
        json(res, badShape ? `{"files":"invalid"}` : `{"files":[]}`, status);
      },
    }));
    assert.deepEqual(ids((await h.owner.refresh()).generation), ["p/vendor/m"]);
    await h.owner.refresh();
    assert.equal(calls, 1, "fresh identity input not reused");
    status = code;
    badShape = code === 200;
    h.advance(365 * 24 * 3_600_000);
    const after = (await h.owner.refresh()).generation;
    if (code === 503) {
      assert.deepEqual(ids(after), ["p/vendor/m"], "eligible stale input lost");
      continue;
    }
    assert.deepEqual(ids(after), ["p/vendor/m", "vendor/m"], `${code}: denied/invalid identity must preserve the CPA baseline`);
    assert.deepEqual(preserved(after), ["vendor/m"]);
    assert.equal(JSON.stringify(after.admitted.find((m) => m.id === "vendor/m")!.row), `{"slug":"vendor/m","id":"unchanged","null":null}`);
    assert.match(h.log.text(), /identity read failed/);
    status = 503;
    assert.deepEqual(ids((await h.owner.refresh()).generation), ["p/vendor/m", "vendor/m"], "denied input resurrected on a later 503");
  }
});

test("incomplete discovery preserves the baseline, also from cache", async (t) => {
  const h = await harness(t, fakeCPA({
    native: `{"models":[{"slug":"vendor/m","id":"unchanged","null":null},{"slug":"p/vendor/m"}]}`,
    channels: `{"openai-compatibility":[{"prefix":"p","base-url":"https://x","api-key-entries":[{}],"models":[{"name":"vendor/m"}]},{"prefix":"unresolved","models":[{"name":"m"}]}]}`,
  }));
  for (let round = 0; round < 2; round++) {
    const { generation } = await h.owner.refresh({ bypass: round === 1 });
    assert.deepEqual(ids(generation), ["p/vendor/m", "vendor/m"]);
    assert.equal(generation.discoveryIncomplete, true);
  }
  assert.match(h.log.text(), /discovery incomplete/);
});

test("unprefixed models are always admitted and never preserved; static slugs opt out of native preservation", () => {
  const none: Identities = { qualified: new Set(["zcode/glm-4.6"]), unavailable: new Set(), nativeOnly: new Set(), bare: new Set(), incomplete: false, texts: [] };
  const { admitted } = admitModels([{ slug: "glm-4.6" }, { slug: "zcode/glm-4.6" }, { slug: "glm-4.6", dup: true }], none, new Set());
  assert.deepEqual(admitted.map((m) => [m.id, m.preserveNative]), [["glm-4.6", false], ["zcode/glm-4.6", false]]);
  const failing: Identities = { ...none, qualified: new Set(), nativeOnly: new Set(), incomplete: true };
  const { admitted: kept, fallbackCount } = admitModels([{ slug: "a/x" }, { slug: "a/y" }], failing, new Set(["a/y"]));
  assert.deepEqual(kept.map((m) => [m.id, m.preserveNative]), [["a/x", true], ["a/y", false]]);
  assert.equal(fallbackCount, 2);
});

test("AISIX supplement is an exact, case-sensitive difference against the raw CPA inventory", () => {
  assert.deepEqual(supplementalIDs(["a", "A", "a", "cpa-filtered", "x/y", " b "], new Set(["cpa-filtered"])), ["a", "A", "x/y", " b "]);
  assert.deepEqual(decodeAISIXModels(`{"object":"list","data":[]}`), []);
  for (const body of [`{}`, `{"data":null}`, `{"data":[{"id":"  "}]}`, `{"data":[{"id":1}]}`, `not-json`]) assert.throws(() => decodeAISIXModels(body), body);
});

test("AISIX phase: authenticated read, filtered CPA IDs not resurrected, optional failures keep CPA output", async (t) => {
  let status = 200;
  let body = `{"object":"list","data":[{"id":"cpa-filtered"},{"id":"router-only"},{"id":"router-only"}]}`;
  let auth = "";
  const aisix = await serve((req, res) => {
    auth = req.headers.authorization ?? "";
    json(res, body, status);
  });
  t.after(() => aisix.close());
  const h = await harness(t, fakeCPA({ native: `{"models":[{"slug":"cpa-filtered/x"},{"slug":"p/m"},{"slug":"cpa-filtered"}]}` }), { aisix: aisix.url });
  let { generation } = await h.owner.refresh();
  assert.equal(auth, "Bearer aisix-token");
  assert.deepEqual(generation.supplemental, ["router-only"]);
  assert.equal(generation.aisix, "usable");

  // Valid empty inventory replaces the old one.
  body = `{"object":"list","data":[]}`;
  h.advance(10 * 60_000);
  ({ generation } = await h.owner.refresh());
  assert.deepEqual(generation.supplemental, []);
  assert.equal(generation.aisix, "empty");

  // 503 reuses the last valid inventory.
  body = `{"object":"list","data":[{"id":"router-only"}]}`;
  h.advance(10 * 60_000);
  await h.owner.refresh();
  h.advance(10 * 60_000);
  status = 503;
  ({ generation } = await h.owner.refresh());
  assert.deepEqual(generation.supplemental, ["router-only"]);

  // 401 fails the optional phase only and is not resurrected by a later 503.
  h.advance(10 * 60_000);
  status = 401;
  let outcome = await h.owner.refresh();
  assert.equal(outcome.generation.aisix, "failed");
  assert.equal(outcome.generation.cpa, "usable");
  assert.deepEqual(outcome.generation.supplemental, []);
  assert.ok(outcome.generation.admitted.length > 0);
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
  const h = await harness(t, fakeCPA({ native: `{"models":[{"slug":"m"}]}` }), { aisix: slow.url, concurrency: 1, aisixTimeoutMs: 50 });
  const outcome = await h.owner.refresh();
  assert.equal(outcome.generation.cpa, "usable");
  assert.deepEqual(ids(outcome.generation), ["m"]);
  assert.equal(outcome.generation.aisix, "failed");
  assert.deepEqual(outcome.generation.supplemental, []);
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
  const fake = fakeCPA({ native: `{"models":[{"slug":"old"}]}` });
  const h = await harness(t, fake, { aisix: aisix.url });
  assert.equal(h.owner.current().generation, 0);
  const first = await h.owner.refresh();
  assert.equal(first.generation.generation, 1);
  h.advance(10 * 60_000);
  const same = await h.owner.refresh();
  assert.equal(same.published, false);
  assert.equal(same.generation, first.generation);

  fake.native = `{"models":[{"slug":"new"}]}`;
  block = true;
  h.advance(10 * 60_000);
  const pending = h.owner.refresh();
  while (!release) await sleep(5);
  assert.deepEqual(ids(h.owner.current()), ["old"], "partial refresh escaped before publication");
  release();
  assert.deepEqual(ids((await pending).generation), ["new"]);
  assert.equal(h.owner.current().generation, 2);

  fake.native = `{"models":null}`;
  h.advance(10 * 60_000);
  let outcome = await h.owner.refresh();
  assert.equal(outcome.generation.cpa, "failed");
  assert.deepEqual(outcome.generation.admitted, []);
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
  assert.deepEqual(runtimeConflicts([ch("a")], ["a", "native", "free"], [{ id: "native/m", row: {}, preserveNative: false }]), [
    `custom channel "a" conflicts with CPA channel prefix`,
    `custom channel "native" conflicts with native manifest provider prefix`,
  ]);
});
