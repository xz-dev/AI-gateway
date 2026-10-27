import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";
import { test } from "node:test";
import { DiskCache } from "../src/disk-cache.ts";
import { HttpPool, ReadStatusError, isCancellation } from "../src/http.ts";
import { MemoryCache, ReadCache, readJSON, requestKey, type CachePolicy } from "../src/read-cache.ts";
import { captureLog, serve, sleep, tempDir } from "./helpers.ts";

const MINUTE = 60_000;

async function setup(dir?: string) {
  const log = captureLog();
  const root = dir ?? (await tempDir());
  const disk = await DiskCache.open(root, log);
  let now = Date.now();
  const cache = new ReadCache(disk, log, () => now);
  return { root, log, disk, cache, pool: new HttpPool(2, 5_000), advance: (ms: number) => (now += ms) };
}

const text = (s: string) => s;

test("authority reads: fresh reuse, stale only on 503, denial is never resurrected, memory only", async (t) => {
  let status = 200;
  let calls = 0;
  const server = await serve((_req, res) => {
    calls++;
    res.writeHead(status);
    res.end(`{"value":12345678901234567890,"empty":[],"false":false}`);
  });
  t.after(() => server.close());
  const env = await setup();
  const get = () => readJSON(env.pool, env.cache, { request: { url: server.url, limit: 1024 }, policy: "authority", decode: text });
  const original = (await get()).text;
  env.advance(5 * MINUTE - 1);
  assert.equal((await get()).text, original);
  assert.equal(calls, 1);
  status = 503;
  for (const age of [1, 365 * 24 * 60 * MINUTE]) {
    env.advance(age);
    assert.equal((await get()).text, original);
  }
  assert.equal(calls, 3, "expired cache must attempt refresh");
  status = 403;
  await assert.rejects(get(), ReadStatusError);
  status = 503;
  await assert.rejects(get(), ReadStatusError, "denied cache resurrected");
  status = 200;
  await get();
  const before = calls;
  // Restart: authority reads are not restored from disk.
  const restarted = await setup(env.root);
  await readJSON(restarted.pool, restarted.cache, { request: { url: server.url, limit: 1024 }, policy: "authority", decode: text });
  assert.equal(calls, before + 1);
  assert.deepEqual(restarted.disk.keys(), []);
});

test("request identity isolates credentials, target and body; files never hold credentials", async () => {
  const env = await setup();
  const keys = new Set<string>();
  for (const spec of [
    { url: "http://cpa/api-call?v=1", auth: "secret-a", body: `{"url":"https://one/models"}` },
    { url: "http://cpa/api-call?v=1", auth: "secret-b", body: `{"url":"https://one/models"}` },
    { url: "http://cpa/api-call?v=1", auth: "secret-a", body: `{"url":"https://two/models"}` },
    { url: "http://cpa/api-call?v=2", auth: "secret-a", body: `{"url":"https://one/models"}` },
  ]) {
    const key = requestKey({ url: spec.url, method: "POST", headers: { Authorization: spec.auth }, body: spec.body, limit: 1 });
    assert.match(key, /^[0-9a-f]{64}$/);
    assert.ok(!keys.has(key), "identity collision");
    keys.add(key);
    await env.cache.load(key, "metadata", async () => "{}");
  }
  assert.equal(env.disk.keys().length, 4);
  for (const name of await readdir(join(env.root, "v2"))) {
    assert.ok(!(await readFile(join(env.root, "v2", name), "utf8")).includes("secret-a"));
  }
});

test("concurrent loads share one fetch; a cancelled waiter returns cancellation", async () => {
  const env = await setup();
  let calls = 0;
  let release!: () => void;
  const gate = new Promise<void>((resolve) => (release = resolve));
  const fetch = async () => {
    calls++;
    await gate;
    return `{"ok":true}`;
  };
  const leader = env.cache.load("key", "authority", fetch);
  const controller = new AbortController();
  controller.abort();
  await assert.rejects(env.cache.load("key", "authority", fetch, { signal: controller.signal }), (error) => isCancellation(error));
  const waiter = new AbortController();
  const abandoned = env.cache.load("key", "authority", fetch, { signal: waiter.signal });
  const followers = Array.from({ length: 12 }, () => env.cache.load("key", "authority", fetch));
  waiter.abort();
  await assert.rejects(abandoned, (error) => isCancellation(error));
  release();
  assert.ok((await Promise.all([leader, ...followers])).every((body) => body === `{"ok":true}`));
  assert.equal(calls, 1);
});

test("freshness is rechecked when claiming a flight", async () => {
  const env = await setup();
  let calls = 0;
  const fetch = async () => {
    calls++;
    await sleep(5);
    return `{"n":${calls}}`;
  };
  await Promise.all(Array.from({ length: 64 }, async (_, i) => {
    await sleep(i % 8);
    await env.cache.load(requestKey({ url: "http://cpa/k", limit: 1 }), "metadata", fetch);
  }));
  assert.equal(calls, 1);
});

test("metadata reads: every refresh failure reuses the last success; empty success replaces; deadline vs cancel", async (t) => {
  let status = 200;
  let body = `{"data":[{"id":"m"}]}`;
  let calls = 0;
  const server = await serve((_req, res) => {
    calls++;
    res.writeHead(status);
    res.end(body);
  });
  t.after(() => server.close());
  const env = await setup();
  const decode = (s: string) => {
    const value = JSON.parse(s);
    if (value.broken) throw new Error("shape");
    return value;
  };
  const read = (signal?: AbortSignal) =>
    readJSON(env.pool, env.cache, { request: { url: server.url, limit: 1024 }, policy: "metadata", decode }, { signal }).then((r) => r.text);
  const original = await read();
  for (const failure of [
    { status: 401, body: "{}" },
    { status: 403, body: "{}" },
    { status: 500, body: "{}" },
    { status: 200, body: "not-json" },
    { status: 200, body: `{"broken":true}` },
  ]) {
    env.advance(60 * MINUTE);
    ({ status, body } = failure);
    assert.equal(await read(), original, `${failure.status} ${failure.body}`);
  }
  const before = calls;
  const expired = AbortSignal.timeout(0);
  await sleep(5);
  assert.equal(await read(expired), original, "exhausted deadline must reuse retained metadata");
  assert.equal(calls, before);
  const cancelled = new AbortController();
  cancelled.abort();
  await assert.rejects(read(cancelled.signal), (error) => isCancellation(error));
  status = 200;
  body = `{"data":[]}`;
  assert.equal(await read(), `{"data":[]}`);
  status = 503;
  env.advance(60 * MINUTE);
  assert.equal(await read(), `{"data":[]}`, "old metadata resurrected after empty replacement");
});

test("sources are isolated; invalid JSON is never cached; cold failure stores nothing", async (t) => {
  let broken = false;
  let otherCalls = 0;
  const server = await serve((req, res) => {
    if (req.url === "/one" && broken) return void res.writeHead(503).end();
    if (req.url === "/invalid") return void res.end("not-json");
    if (req.url === "/two") otherCalls++;
    res.end(JSON.stringify({ source: req.url }));
  });
  t.after(() => server.close());
  const env = await setup();
  const read = (path: string) =>
    readJSON(env.pool, env.cache, { request: { url: server.url + path, limit: 1024 }, policy: "metadata", decode: (s) => JSON.parse(s) }).then((r) => r.text);
  await read("/one");
  env.advance(4 * MINUTE);
  await read("/two");
  env.advance(2 * MINUTE);
  broken = true;
  assert.equal(await read("/one"), `{"source":"/one"}`);
  assert.equal(await read("/two"), `{"source":"/two"}`);
  assert.equal(otherCalls, 1);
  await assert.rejects(read("/invalid"));
  assert.equal(env.disk.keys().length, 2);
  await assert.rejects(env.cache.load("missing", "metadata", async () => {
    throw new TypeError("fetch failed");
  }));
  assert.equal(env.disk.keys().length, 2);
});

test("forced refresh skips freshness; authority stale reuse is disabled, metadata fallback is not", async () => {
  const env = await setup();
  let calls = 0;
  const ok = async () => {
    calls++;
    return "{}";
  };
  const down = async () => {
    calls++;
    throw new ReadStatusError(503);
  };
  for (const policy of ["authority", "metadata"] as const satisfies readonly CachePolicy[]) {
    const key = requestKey({ url: `http://cpa/${policy}`, limit: 1 });
    await env.cache.load(key, policy, ok);
    await env.cache.load(key, policy, ok, { bypass: true });
    if (policy === "authority") {
      await assert.rejects(env.cache.load(key, policy, down, { bypass: true }));
      // A forced failure dropped the entry: no later stale reuse.
      await assert.rejects(env.cache.load(key, policy, down));
    } else {
      let stale = 0;
      assert.equal(await env.cache.load(key, policy, down, { bypass: true, onStale: () => stale++ }), "{}");
      assert.equal(stale, 1);
    }
  }
  assert.equal(calls, 2 + 2 + 1 + 2);
});

test("authority memory cache: 32 MiB bound, replacement keeps FIFO position", () => {
  const cache = new MemoryCache();
  assert.equal(cache.budget, 32 << 20);
  const mib = (n: number) => "x".repeat(n << 20);
  for (const key of ["a", "b", "c"]) cache.put(key, mib(8), 0);
  cache.put("a", mib(20), 0); // 8+8+20 > 32: evict b
  assert.deepEqual(cache.keys(), ["a", "c"]);
  assert.equal(cache.bytes, 28 << 20);
  cache.put("d", mib(8), 0); // 28+8 > 32: evict the oldest, a
  assert.deepEqual(cache.keys(), ["c", "d"]);
  assert.throws(() => cache.put("huge", mib(33), 0));
  assert.deepEqual(cache.keys(), ["c", "d"]);
});
