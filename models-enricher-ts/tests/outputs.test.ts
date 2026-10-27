import assert from "node:assert/strict";
import { mkdir, readdir, readFile, stat, writeFile } from "node:fs/promises";
import { request } from "node:http";
import type { AddressInfo } from "node:net";
import { join } from "node:path";
import { test } from "node:test";
import { OUTPUT_MAX_ENTRIES, OutputStore } from "../src/outputs.ts";
import { createHTTPServer } from "../src/server.ts";
import { PINNED_KEYS } from "../src/service.ts";
import { channels, harness } from "./catalog-harness.ts";
import { sleep, tempDir } from "./helpers.ts";

const until = async (check: () => boolean | Promise<boolean>, what: string) => {
  for (let i = 0; i < 200; i++) {
    if (await check()) return;
    await sleep(10);
  }
  assert.fail(`timed out waiting for ${what}`);
};

test("store: tmp+rename publication, private files, leased files outlive replacement, generations never regress", async () => {
  const dir = join(await tempDir(), "out");
  await mkdir(dir);
  await writeFile(join(dir, "stale-from-previous-process"), "x");
  const store = await OutputStore.open(dir, PINNED_KEYS);
  assert.deepEqual(await readdir(dir), [], "leftover files are removed at startup");

  assert.equal(await store.publish("codex:1", 1, "first", 10), true);
  const first = store.get("codex:1")!;
  assert.equal(await readFile(first.path, "utf8"), "first");
  assert.equal((await stat(first.path)).mode & 0o777, 0o600);
  assert.equal(first.size, 5);

  const lease = store.lease("codex:1", 11)!;
  await store.publish("codex:1", 2, "second", 12);
  assert.equal(await readFile(lease.path, "utf8"), "first", "a streaming reader keeps its file");
  lease.release();
  lease.release();
  await until(async () => (await readdir(dir)).length === 1, "the replaced file to be unlinked");
  assert.equal(await readFile(store.get("codex:1")!.path, "utf8"), "second");

  assert.equal(await store.publish("codex:1", 1, "late", 13), false, "a late old-generation build never overwrites");
  assert.equal(store.get("codex:1")!.generation, 2);
  assert.equal((await readdir(dir)).length, 1);
});

test("store: bounded entries evict the least recently read, never pinned keys; older generations retire", async () => {
  const store = await OutputStore.open(join(await tempDir(), "out"), PINNED_KEYS);
  await store.publish("standard", 1, "s", 0);
  await store.publish("table", 1, "t", 0, { rowCount: 1 });
  for (let i = 0; i < OUTPUT_MAX_ENTRIES + 3; i++) await store.publish(`codex:${i}`, 1, "c", i + 1);
  assert.equal(store.size, OUTPUT_MAX_ENTRIES);
  assert.ok(store.get("standard") && store.get("table"), "pinned keys survive the bound");
  assert.equal(store.get("codex:0"), undefined, "least recently read evicted first");
  store.retireOlder(2, new Set(["table"]));
  assert.deepEqual([...store.entries()].map((entry) => entry.key), ["table"]);
  await until(async () => (await readdir(store.dir)).length === 1, "retired files to be unlinked");
});

async function bigCatalog(t: { after(fn: () => unknown): void }) {
  const names = Array.from({ length: 300 }, (_, i) => `m${i}`);
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: names }]),
    native: names.map((name) => ({ slug: `codex/${name}`, display_name: name, context_window: 1000, notes: "n".repeat(1024) })),
    yaml: 'metadata:\n  chain: ["@self"]\n',
  });
  const server = createHTTPServer(h.service, h.log);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  t.after(() => new Promise((resolve) => server.close(resolve)));
  return { h, port: (server.address() as AddressInfo).port };
}

test("catalogs stream byte-identical from the published file with an exact Content-Length", async (t) => {
  const { h, port } = await bigCatalog(t);
  const res = await fetch(`http://127.0.0.1:${port}/v1/models?client_version=1.0.0`);
  const body = Buffer.from(await res.arrayBuffer());
  const entry = h.service.outputs.get("codex:1.0.0")!;
  assert.ok(body.length > 4 * 65536, "body spans several 64 KiB chunks");
  assert.deepEqual(body, await readFile(entry.path));
  assert.equal(Number(res.headers.get("content-length")), entry.size);
  assert.equal(JSON.parse(body.toString("utf8")).models.length, 300);
  const table = await fetch(`http://127.0.0.1:${port}/models-table/refresh`, { method: "POST" });
  const html = await table.text();
  assert.equal(Number(table.headers.get("content-length")), Buffer.byteLength(html));
  assert.match(html, /^<!DOCTYPE html>[\s\S]*<td>codex\/m299<\/td>[\s\S]*<\/html>\n$/);
  assert.equal(entry.readers, 0);
});

test("a client disconnecting mid-stream releases the file; replaced files are then deleted", async (t) => {
  const { h, port } = await bigCatalog(t);
  assert.equal((await fetch(`http://127.0.0.1:${port}/v1/models?client_version=1.0.0`)).status, 200);
  const entry = h.service.outputs.get("codex:1.0.0")!;
  await new Promise<void>((resolve, reject) => {
    const req = request({ port, path: "/v1/models?client_version=1.0.0", agent: false }, (res) => {
      res.once("data", () => {
        req.destroy();
        resolve();
      });
    });
    req.on("error", () => undefined);
    req.end();
    setTimeout(() => reject(new Error("no first chunk")), 5_000);
  });
  await until(() => entry.readers === 0, "the lease to be released after the disconnect");
  // A forced refresh republishes the catalog; the old file disappears once unread.
  assert.equal((await fetch(`http://127.0.0.1:${port}/models-table/refresh`, { method: "POST" })).status, 200);
  assert.notEqual(h.service.outputs.get("codex:1.0.0")!.path, entry.path);
  await until(async () => !(await readdir(h.service.outputs.dir)).includes(entry.path.slice(h.service.outputs.dir.length + 1)), "the replaced file to be deleted");
  assert.equal((await fetch(`http://127.0.0.1:${port}/v1/models?client_version=1.0.0`)).status, 200, "the server keeps serving");
});

test("idle state holds the index only: no bodies, no parsed rows", async (t) => {
  const { h, port } = await bigCatalog(t);
  for (const path of ["/v1/models?client_version=1.0.0", "/v1/models?client_version=2.0.0", "/v1/models"]) await (await fetch(`http://127.0.0.1:${port}${path}`)).arrayBuffer();
  await (await fetch(`http://127.0.0.1:${port}/models-table/refresh`, { method: "POST" })).text();
  // Every published value is a short scalar or the small table summary.
  const longest = (value: unknown): number =>
    typeof value === "string" ? value.length : value && typeof value === "object" ? Math.max(0, ...Object.values(value).map(longest)) : 0;
  const entries = [...h.service.outputs.entries()];
  assert.deepEqual(entries.map((entry) => entry.key).sort(), ["codex:1.0.0", "codex:2.0.0", "standard", "table"]);
  for (const entry of entries) {
    assert.ok(longest(entry) < 512, `${entry.key} holds no body`);
    assert.ok(entry.size > 0);
  }
  assert.deepEqual(Object.keys((entries.find((entry) => entry.key === "table")!.meta as object)).sort(), ["clientVersion", "dataVersions", "generation", "hidden", "kept", "referencedOnly", "rowCount"]);
  // The authority keeps the validated text, not parsed rows.
  for (const model of h.owner.current().admitted) assert.deepEqual(Object.keys(model).sort(), ["id", "index", "preserveNative"]);
});

test("an expired catalog is served immediately and rebuilt in the background", async (t) => {
  const { h, port } = await bigCatalog(t);
  await (await fetch(`http://127.0.0.1:${port}/v1/models?client_version=1.0.0`)).arrayBuffer();
  const first = h.service.outputs.get("codex:1.0.0")!;
  h.advance(6 * 60_000);
  assert.equal((await fetch(`http://127.0.0.1:${port}/v1/models?client_version=1.0.0`)).status, 200);
  await until(() => h.service.outputs.get("codex:1.0.0") !== first, "the background rebuild");
  assert.equal(h.service.outputs.get("codex:1.0.0")!.generation, first.generation);
});
