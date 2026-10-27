import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { writeFile } from "node:fs/promises";
import type { AddressInfo } from "node:net";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import type { Row } from "../src/json.ts";
import { createHTTPServer } from "../src/server.ts";
import { channels, harness, type Harness, type HarnessOptions } from "./catalog-harness.ts";
import { sleep, tempDir } from "./helpers.ts";

const native = (slug: string, extra: Row = {}): Row => ({ slug, display_name: slug, ...extra });

async function http(t: { after(fn: () => unknown): void }, options: Partial<HarnessOptions> = {}) {
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["a", "b", "image-1"] }]),
    native: [native("codex/a", { context_window: 1000 })],
    yaml: 'metadata:\n  chain: ["@self"]\n',
    ...options,
  });
  const server = createHTTPServer(h.service, h.log);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  t.after(() => new Promise((resolve) => server.close(resolve)));
  const base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  const call = async (path: string, init?: RequestInit) => {
    const res = await fetch(base + path, init);
    return { status: res.status, type: res.headers.get("content-type") ?? "", text: await res.text() };
  };
  return { h, call, base };
}

const nativeFetches = (h: Harness) => h.fake.calls.filter((call) => call.startsWith("GET /v1/models")).length;

test("routes: readiness, catalogs, routing-index removal and method rules", async (t) => {
  const { h, call } = await http(t);
  assert.equal((await call("/readyz")).status, 503, "not ready before the first authority generation");
  assert.equal((await call("/healthz")).text, "ok");
  const codex = await call("/v1/models?client_version=1.0.0");
  assert.equal(codex.status, 200);
  assert.equal(codex.type, "application/json");
  assert.deepEqual(JSON.parse(codex.text).models.map((m: Row) => m.slug), ["codex/a"]);
  assert.deepEqual(JSON.parse((await call("/v1/models")).text), { object: "list", data: [{ id: "codex/a", object: "model" }] });
  assert.deepEqual(JSON.parse((await call("/readyz")).text), { status: "ready", generation: 1 });
  assert.equal((await call("/routing-index?model=codex/a")).status, 404);
  assert.equal((await call("/v1/models", { method: "POST" })).status, 405);
  const before = h.fake.calls.length;
  assert.equal((await call("/models-table/refresh")).status, 405, "GET never starts refresh work");
  assert.equal(h.fake.calls.length, before);
});

test("models table: loading page, background refresh, POST result and coalesced manual refreshes", async (t) => {
  const { h, call } = await http(t);
  const first = await call("/models-table");
  assert.equal(first.status, 200);
  assert.match(first.text, /加载中/);
  assert.match(first.text, /<form id="refresh" method="post" action="\/models-table\/refresh">/);
  assert.doesNotMatch(first.text, /<script/);

  const [one, two] = await Promise.all([call("/models-table/refresh", { method: "POST" }), call("/models-table/refresh", { method: "POST" })]);
  assert.equal(one.status, 200);
  assert.match(one.text, /刷新成功/);
  assert.match(two.text, /codex\/a/);
  assert.ok(nativeFetches(h) <= 2, "the entry refresh and concurrent POSTs share collections");
  assert.match(one.text, /@opencode-ai\/models 未使用/);

  const inside = nativeFetches(h);
  const page = await call("/models-table");
  assert.match(page.text, /最后成功刷新：\d{4}-/);
  assert.match(page.text, /下次定时刷新：未启动或已停止/, "start() was not called");
  await sleep(50);
  assert.equal(nativeFetches(h), inside, "inside the entry window GET does not refresh");
  h.advance(11 * 60_000);
  await call("/models-table");
  await sleep(100);
  assert.ok(nativeFetches(h) > inside, "outside the entry window GET starts a background refresh");
});

test("table shows hidden rules, referenced-only models and bundled data versions", async (t) => {
  const { h, call } = await http(t, {
    native: [native("codex/a"), native("codex/image-1"), native("codex/b", { context_window: 42 })],
    providers: { openai: { a: { limit: { context: 9 } } } },
    yaml: `metadata:
  chain: ["@self", "models.dev/openai/api_key"]
  include_models: ["**", "!**/image-*", "!codex/b", "alias/**"]
static_models:
  - slug: alias/x
    inherit: [codex/b]
`,
  });
  const page = await call("/models-table/refresh", { method: "POST" });
  assert.equal(page.status, 200);
  assert.match(page.text, /hidden by include_models\[1\] !\*\*\/image-\*<\/td><td colspan="9">not processed/);
  assert.match(page.text, /hidden by include_models\[2\] !codex\/b · referenced by alias\/x<\/td><td>codex\/b<\/td>/);
  assert.match(page.text, /@opencode-ai\/models 0\.0\.test \(snapshot 2026-10-01T00:00:00\.000Z\) · modelparams 0\.0\.mp/);
  assert.match(page.text, /kept 2 · hidden 2 · referenced-only 1/);
  const catalog = JSON.parse((await call("/v1/models?client_version=1")).text).models.map((m: Row) => m.slug);
  assert.deepEqual(catalog, ["codex/a", "alias/x"], "table markings never change JSON contents");
  void h;
});

test("manual refresh without CPA authority is an error page; a failed background refresh keeps the table", async (t) => {
  const { h, call } = await http(t);
  assert.equal((await call("/models-table/refresh", { method: "POST" })).status, 200);
  h.fake.nativeStatus = 500;
  const failed = await call("/models-table/refresh", { method: "POST" });
  assert.equal(failed.status, 502);
  assert.match(failed.text, /刷新失败/);
  assert.doesNotMatch(failed.text, /<table/);
  const page = await call("/models-table");
  assert.match(page.text, /上次刷新失败/);
  assert.match(page.text, /codex\/a/, "the last published table stays visible");
});

test("metadata failure recomposes from current authority with a stale warning and keeps the success window", async (t) => {
  const { h, call } = await http(t, {
    native: [native("codex/a")],
    upstream: { "https://codex.example/v1/models?client_version=v0.65.0": { data: [{ id: "a", context_length: 777 }] } },
    yaml: 'metadata:\n  chain: ["@cpa-upstream", "@self"]\n',
  });
  const ok = await call("/models-table/refresh", { method: "POST" });
  assert.match(ok.text, /刷新成功/);
  assert.match(ok.text, /<td>777<\/td>/);
  const success = /最后成功刷新：([^ ]+)/.exec(ok.text)![1];
  h.advance(1000);
  h.fake.native = JSON.stringify({ models: [native("codex/a"), native("codex/b")] });
  h.fake.apiCall = (_payload, res) => {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ status_code: 503, body: "" }));
  };
  const stale = await call("/models-table/refresh", { method: "POST" });
  assert.equal(stale.status, 200);
  assert.match(stale.text, /刷新完成 .* 1 个输入接口刷新失败/);
  assert.match(stale.text, /codex\/b/, "the newly admitted model appears");
  assert.match(stale.text, /<td>777<\/td>/, "retained metadata is reused");
  assert.equal(/最后成功刷新：([^ ]+)/.exec(stale.text)![1], success);
});

test("a client disconnect stops only its wait; the shared build finishes and is cached", async (t) => {
  const { h, call, base } = await http(t);
  let release!: () => void;
  const gate = new Promise<void>((resolve) => (release = resolve));
  const controller = new AbortController();
  // Hold the first authority collection until the client has gone.
  const realRefresh = h.owner.refresh.bind(h.owner);
  h.owner.refresh = (options) => gate.then(() => realRefresh(options));
  const pending = fetch(`${base}/v1/models?client_version=2.0.0`, { signal: controller.signal }).catch((error: Error) => error.name);
  await sleep(50);
  controller.abort();
  assert.equal(await pending, "AbortError");
  release();
  for (let i = 0; i < 50 && !h.service.outputs.get("codex:2.0.0"); i++) await sleep(20);
  assert.ok(h.service.outputs.get("codex:2.0.0"), "the abandoned build still published its projection");
  const before = nativeFetches(h);
  assert.equal((await call("/v1/models?client_version=2.0.0")).status, 200);
  assert.equal(nativeFetches(h), before, "served from the shared result without another collection");
});

test("start/stop: startup job runs, schedule shows, shutdown cancels and later triggers do nothing", async (t) => {
  const { h, call } = await http(t);
  h.service.start();
  for (let i = 0; i < 100 && !(await call("/readyz")).text.includes("ready"); i++) await sleep(20);
  assert.match((await call("/models-table")).text, /下次定时刷新：\d{4}-/);
  await h.service.stop();
  assert.equal(h.service.startRefresh(false), undefined);
  assert.equal((await call("/models-table/refresh", { method: "POST" })).status, 503);
  assert.match((await call("/models-table")).text, /下次定时刷新：未启动或已停止/);
});

// ---------------------------------------------------------------------------
// CLI

const MAIN = fileURLToPath(new URL("../src/main.ts", import.meta.url));
const cli = (args: string[], input?: string) => spawnSync(process.execPath, [MAIN, ...args], { input, encoding: "utf8", timeout: 30_000, env: { ...process.env, NODE_OPTIONS: "" } });

test("validate-config prints only the version and input digest; exit codes 0/1/2", async () => {
  const dir = await tempDir();
  const valid = "cpa_base_url: http://cpa:8317\nmetadata:\n  chain: [\"@self\"]\n";
  const path = join(dir, "config.yaml");
  await writeFile(path, valid);
  const ok = cli(["validate-config", path]);
  assert.equal(ok.status, 0, ok.stderr);
  assert.equal(ok.stdout, `{"version":1,"config_digest":"${createHash("sha256").update(valid).digest("hex")}"}\n`);
  const stdin = cli(["validate-config", "-"], valid);
  assert.equal(stdin.status, 0);
  assert.equal(stdin.stdout, ok.stdout);

  const secret = "cpa_base_url: http://cpa:8317\nsource_priority: [sk-secret-value]\n";
  const bad = cli(["validate-config", "-"], secret);
  assert.equal(bad.status, 1);
  assert.equal(bad.stdout, "");
  assert.match(bad.stderr, /source_priority: removed field/);
  assert.doesNotMatch(bad.stderr, /sk-secret-value/);
  assert.equal(cli(["validate-config", join(dir, "missing.yaml")]).status, 1);
  assert.equal(cli(["validate-config"]).status, 2);
  assert.equal(cli(["bogus"]).status, 2);
});

test("healthcheck exits non-zero when nothing serves /readyz", () => {
  assert.equal(cli(["healthcheck"]).status, 1);
});
