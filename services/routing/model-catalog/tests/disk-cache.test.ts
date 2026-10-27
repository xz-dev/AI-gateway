import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readdir, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { test } from "node:test";
import { DISK_PAYLOAD_BUDGET, DiskCache } from "../src/disk-cache.ts";
import { HttpPool } from "../src/http.ts";
import { ReadCache, readJSON } from "../src/read-cache.ts";
import { captureLog, serve, tempDir } from "./helpers.ts";

const k = (name: string) => createHash("sha256").update(name).digest("hex");
const names = (cache: DiskCache) => cache.keys().map((key) => ["a", "b", "c", "d", "huge"].find((name) => k(name) === key));

test("production payload budget is exactly 100,000,000 bytes", async () => {
  assert.equal(DISK_PAYLOAD_BUDGET, 100_000_000);
  assert.equal((await DiskCache.open(await tempDir(), captureLog())).budget, 100_000_000);
});

test("FIFO budget: exact fit, replacement keeps position, survives restart, oversized never evicts", async () => {
  const dir = await tempDir();
  let cache = await DiskCache.open(dir, captureLog(), 10);
  const put = (name: string, size: number) => cache.put(k(name), "x".repeat(size), 1);
  await put("a", 4);
  await put("b", 4);
  assert.ok(await cache.get(k("a")));
  await put("a", 3); // replacement keeps its admission position
  await put("c", 2); // 3+4+2 = 9 fits without eviction
  assert.deepEqual(names(cache), ["a", "b", "c"]);
  assert.equal(cache.bytes, 9);

  cache = await DiskCache.open(dir, captureLog(), 10); // restart keeps admission order
  await put("d", 2); // evicts the oldest (a), though it was read and replaced
  assert.deepEqual(names(cache), ["b", "c", "d"]);
  assert.equal(cache.bytes, 8);
  await put("b", 7); // growth evicts the oldest other entry (c); b keeps its position
  assert.deepEqual(names(cache), ["b", "d"]);
  assert.equal(cache.bytes, 9);
  await assert.rejects(put("huge", 11));
  assert.deepEqual(names(cache), ["b", "d"]);
  assert.equal(cache.bytes, 9);
  await put("a", 1); // a removed key re-enters at a new position
  assert.deepEqual(names(cache), ["b", "d", "a"]);
  assert.equal((await cache.get(k("b")))?.body, "x".repeat(7));
});

test("private files; old-format, stray and corrupt files are ignored or dropped", async () => {
  const dir = await tempDir();
  await writeFile(join(dir, "inputs.db"), "bbolt bytes from the Go sidecar");
  const first = await DiskCache.open(dir, captureLog());
  await first.put(k("a"), "{}", 5);
  assert.equal((await stat(join(dir, "v2"))).mode & 0o777, 0o700);
  assert.equal((await stat(join(dir, "v2", k("a")))).mode & 0o777, 0o600);
  await writeFile(join(dir, "v2", k("corrupt")), "short");
  await writeFile(join(dir, "v2", `${k("b")}.123.tmp`), "partial");
  await writeFile(join(dir, "v2", "README"), "not a cache entry");
  const reopened = await DiskCache.open(dir, captureLog());
  assert.deepEqual(reopened.keys(), [k("a")]);
  assert.deepEqual(await reopened.get(k("a")), { body: "{}", stored: 5 });
  assert.deepEqual((await readdir(join(dir, "v2"))).sort(), ["README", k("a")].sort());
});

test("startup trims a store that exceeds a lowered budget, oldest first", async () => {
  const dir = await tempDir();
  const cache = await DiskCache.open(dir, captureLog(), 100);
  for (const name of ["a", "b", "c"]) await cache.put(k(name), "x".repeat(4), 1);
  const smaller = await DiskCache.open(dir, captureLog(), 8);
  assert.deepEqual(names(smaller), ["b", "c"]);
});

test("restart during an outage reuses the persisted response without resetting its age", async (t) => {
  let status = 200;
  const server = await serve((_req, res) => {
    res.writeHead(status);
    res.end(`{"data":[{"id":"m","context_window":4096}]}`);
  });
  t.after(() => server.close());
  const dir = await tempDir();
  let now = 1_000_000;
  const open = async () => new ReadCache(await DiskCache.open(dir, captureLog()), captureLog(), () => now);
  const read = (cache: ReadCache, onStale?: () => void) =>
    readJSON(new HttpPool(1, 5_000), cache, { request: { url: server.url, limit: 1024 }, policy: "metadata", decode: (s) => JSON.parse(s) }, { onStale });
  await read(await open());
  now += 60 * 60_000;
  status = 503;
  let stale = 0;
  const restarted = await open();
  assert.equal((await read(restarted, () => stale++)).value.data[0].context_window, 4096);
  assert.equal(stale, 1);
  assert.equal((await restarted.disk!.get(restarted.disk!.keys()[0]!))?.stored, 1_000_000, "recovery must keep the original freshness age");
});
