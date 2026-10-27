import assert from "node:assert/strict";
import { test } from "node:test";
import { HttpPool, InvalidResponseError, isCancellation, isDeadline, isTransient, ReadStatusError } from "../src/http.ts";
import { serve, sleep } from "./helpers.ts";

test("pool bounds in-flight requests and frees slots after the body is read", async (t) => {
  let active = 0;
  let peak = 0;
  const server = await serve(async (_req, res) => {
    active++;
    peak = Math.max(peak, active);
    await sleep(20);
    active--;
    res.end("{}");
  });
  t.after(() => server.close());
  const pool = new HttpPool(2, 5_000);
  const results = await Promise.all(Array.from({ length: 6 }, () => pool.read({ url: server.url, limit: 10 })));
  assert.equal(peak, 2);
  assert.ok(results.every((result) => result.status === 200 && result.text === "{}"));
});

test("body limit is enforced by declared length and by streamed size", async (t) => {
  const server = await serve((req, res) => {
    if (req.url === "/declared") {
      res.writeHead(200, { "Content-Length": "11" });
      res.end("x".repeat(11));
    } else {
      res.writeHead(200);
      res.write("x".repeat(6));
      res.end("x".repeat(6));
    }
  });
  t.after(() => server.close());
  const pool = new HttpPool(1, 5_000);
  await assert.rejects(pool.read({ url: `${server.url}/declared`, limit: 10 }), InvalidResponseError);
  await assert.rejects(pool.read({ url: `${server.url}/chunked`, limit: 10 }), InvalidResponseError);
  // The rejected reads released their slots.
  assert.equal((await pool.read({ url: `${server.url}/chunked`, limit: 12 })).text.length, 12);
});

test("non-200 and redirects are returned as statuses, never followed", async (t) => {
  const server = await serve((req, res) => {
    if (req.url === "/redirect") {
      res.writeHead(302, { Location: "/target" });
      res.end();
    } else {
      res.writeHead(503);
      res.end("down");
    }
  });
  t.after(() => server.close());
  const pool = new HttpPool(1, 5_000);
  assert.deepEqual(await pool.read({ url: `${server.url}/redirect`, limit: 10 }), { status: 302, text: "" });
  assert.equal((await pool.read({ url: `${server.url}/x`, limit: 10 })).status, 503);
});

test("a queued request aborts without taking a slot; timeouts are deadlines", async (t) => {
  const server = await serve(async (_req, res) => {
    await sleep(100);
    res.end("{}");
  });
  t.after(() => server.close());
  const pool = new HttpPool(1, 30);
  const first = pool.read({ url: server.url, limit: 10 });
  const controller = new AbortController();
  const queued = pool.read({ url: server.url, limit: 10, signal: controller.signal });
  controller.abort();
  await assert.rejects(queued, (error) => isCancellation(error));
  await assert.rejects(first, (error) => isDeadline(error) && isTransient(error));
});

test("error classification", () => {
  for (const status of [502, 503, 504]) assert.ok(isTransient(new ReadStatusError(status)));
  for (const status of [401, 403, 500]) assert.ok(!isTransient(new ReadStatusError(status)));
  assert.ok(isTransient(new TypeError("fetch failed")));
  assert.ok(!isTransient(new InvalidResponseError("bad")));
  assert.ok(!isTransient(new DOMException("aborted", "AbortError")));
});
