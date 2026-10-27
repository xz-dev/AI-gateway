import assert from "node:assert/strict";
import { test } from "node:test";
import { decodeNativeManifest, parseAPICall, parseChannels } from "../src/cpa.ts";
import { DiskCache } from "../src/disk-cache.ts";
import { ReadStatusError } from "../src/http.ts";
import { ReadCache } from "../src/read-cache.ts";
import { CLIENT_KEY, MANAGEMENT_KEY, cpaClient, fakeCPA, serveCPA } from "./fake-cpa.ts";
import { captureLog, json, tempDir } from "./helpers.ts";

test("native manifest is pinned to client_version=1 and rejects invalid authority shapes", async (t) => {
  for (const body of ["{}", "null", `{"models":null}`, `{"models":[{"display_name":"not an ID"}]}`, `{"models":[" "]}`]) {
    assert.throws(() => decodeNativeManifest(body), body);
  }
  const fake = fakeCPA({ native: `{"models":[{"slug":"p/m","n":9007199254740993}]}` });
  const server = await serveCPA(fake);
  t.after(() => server.close());
  const { client } = cpaClient(server.url);
  const result = await client.nativeManifest();
  assert.equal(JSON.stringify(result.value), `[{"slug":"p/m","n":9007199254740993}]`);
  assert.deepEqual(fake.calls, ["GET /v1/models?client_version=1"]);
});

test("channel parsing: disabled entries, alias preference, coverage gaps and invalid shapes", () => {
  const { channels, incomplete } = parseChannels(
    "openai-compatibility",
    JSON.stringify({
      "openai-compatibility": [
        { name: "A", prefix: "/hub/", "base-url": "https://a/v1", "api-key-entries": [{ "auth-index": "k1" }], models: [{ name: "orig", alias: "nice" }, { name: "plain", alias: "" }] },
        { name: "off", prefix: "off", disabled: true, "api-key-entries": [{}] },
        { name: "keyless", prefix: "gap", models: [{ name: "m" }] },
        { name: "entry-auth", prefix: "e", base_url: "https://e", auth_index: "k2" },
      ],
    }),
  );
  assert.equal(incomplete, true);
  assert.deepEqual(
    channels.map((c) => [c.prefix, c.authIndex, c.baseUrl, c.models]),
    [
      ["hub", "k1", "https://a/v1", ["nice", "plain"]],
      ["e", "k2", "https://e", []],
    ],
  );
  assert.throws(() => parseChannels("claude-api-key", `{"claude-api-key":[{"auth-index":"x","models":[{"name":"m","alias":1}]}]}`));
  assert.throws(() => parseChannels("claude-api-key", `{"claude-api-key":[{"auth-index":"x","models":[{"alias":"m"}]}]}`));
  assert.throws(() => parseChannels("claude-api-key", `{}`));
  assert.deepEqual(parseChannels("claude-api-key", `{"claude-api-key":[{"prefix":"c","auth-index":"x"}]}`).incomplete, false);
});

test("discovery tolerates one failing kind and reports incomplete coverage", async (t) => {
  const fake = fakeCPA({ native: `{"models":[]}`, channels: `{"openai-compatibility":"broken"}` });
  const server = await serveCPA(fake);
  t.after(() => server.close());
  const { client, log } = cpaClient(server.url);
  const discovery = await client.discover();
  assert.equal(discovery.incomplete, true);
  assert.equal(discovery.channels.length, 0);
  assert.match(log.text(), /channel list failed/);
});

test("api-call envelope: inner status, string or object bodies", () => {
  assert.deepEqual(parseAPICall(`{"status_code":200,"body":"{\\"models\\":[]}"}`), { models: [] });
  assert.deepEqual(parseAPICall(`{"status_code":200,"body":{"models":[]}}`), { models: [] });
  assert.throws(() => parseAPICall(`{"status_code":403,"body":"denied"}`), (error) => error instanceof ReadStatusError && error.status === 403);
  assert.throws(() => parseAPICall(`{"status_code":200,"body":"not json"}`));
  assert.throws(() => parseAPICall(`{"status_code":200}`));
});

test("api-call forwards $TOKEN$ headers and auth index, caches only read-only inner requests, leaks no key", async (t) => {
  let inner = 200;
  let calls = 0;
  const payloads: unknown[] = [];
  const fake = fakeCPA({
    native: `{"models":[]}`,
    apiCall: (payload, res) => {
      calls++;
      payloads.push(payload);
      json(res, inner === 200 ? { status_code: 200, body: `{"models":[]}` } : { status_code: inner, body: "denied" });
    },
  });
  const server = await serveCPA(fake);
  t.after(() => server.close());
  const log = captureLog();
  let now = Date.now();
  const cache = new ReadCache(await DiskCache.open(await tempDir(), log), log, () => now);
  const { client } = cpaClient(server.url, { cache, log });
  const channel = { type: "openai-compatibility", name: "n", prefix: "p", baseUrl: "https://up", authIndex: "one", models: [] } as const;
  const call = (method: "GET" | "POST", url: string) =>
    client.apiCall(channel, { method, url, headers: { Authorization: "Bearer $TOKEN$" }, data: method === "POST" ? `{"model":"m"}` : undefined }, (v) => v);

  inner = 403;
  const error = await call("GET", "https://upstream/cold").catch((e: Error) => e);
  assert.ok(error instanceof ReadStatusError);
  assert.ok(!String(error.message).includes(MANAGEMENT_KEY) && !String(error.message).includes(CLIENT_KEY));
  await assert.rejects(call("GET", "https://upstream/cold"));
  assert.equal(calls, 2, "inner failure cached");
  inner = 200;
  await call("GET", "https://upstream/models");
  await call("GET", "https://upstream/models");
  assert.equal(calls, 3, "api-call read not cached");
  // Channel lists are metadata inputs: after expiry an inner 403 reuses the last success.
  now += 60 * 60_000;
  inner = 403;
  assert.deepEqual(await call("GET", "https://upstream/models"), { models: [] });
  assert.equal(calls, 4);
  inner = 200;
  await call("POST", "https://upstream/api/show");
  await call("POST", "https://upstream/api/show");
  assert.equal(calls, 5, "read-only POST not cached");
  await call("POST", "https://upstream/change");
  await call("POST", "https://upstream/change");
  assert.equal(calls, 7, "write POST cached");
  assert.deepEqual(payloads[0], { method: "GET", url: "https://upstream/cold", header: { Authorization: "Bearer $TOKEN$" }, auth_index: "one" });
  assert.ok(!log.text().includes(MANAGEMENT_KEY));
});
