import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { tempDir } from "./helpers.ts";

const ROOT = fileURLToPath(new URL("..", import.meta.url));
const run = (args: string[], env: NodeJS.ProcessEnv = {}) =>
  spawnSync(process.execPath, args, { cwd: ROOT, encoding: "utf8", timeout: 120_000, env: { ...process.env, NODE_OPTIONS: "", ...env } });

test("build-tables writes both pinned data versions and non-empty tables", async () => {
  const output = join(await tempDir(), "tables.json");
  const result = run(["scripts/build-tables.mjs", output]);
  assert.equal(result.status, 0, result.stderr);
  const pkg = JSON.parse(await readFile(join(ROOT, "package.json"), "utf8"));
  const tables = JSON.parse(await readFile(output, "utf8"));
  assert.equal(tables.modelsDevVersion, pkg.dependencies["@opencode-ai/models"]);
  assert.equal(tables.modelparamsVersion, pkg.dependencies.modelparams);
  assert.match(tables.modelsDevGeneratedAt, /^\d{4}-\d{2}-\d{2}T/);
  assert.deepEqual(tables.degraded, []);
  assert.ok(Object.keys(tables.modelsDev.providers).length > 0);
  assert.ok(Object.keys(tables.modelsDev.models).length > 0);
  assert.ok(tables.modelparams.catalog.length > 0);
});

test("bundles: server and worker build without Pi packages and compile from tables", async () => {
  const result = run(["scripts/bundle.mjs"]);
  assert.equal(result.status, 0, result.stderr);
  for (const file of ["server.mjs", "worker.mjs"]) {
    const text = await readFile(join(ROOT, "dist", file), "utf8");
    assert.doesNotMatch(text, /@earendil-works/, file);
  }
  const tables = join(await tempDir(), "tables.json");
  await writeFile(tables, JSON.stringify({ modelsDev: { providers: {}, models: {} }, modelparams: { catalog: [] }, degraded: [] }));
  const warm = run(["dist/server.mjs", "warm-compile-cache"], { TABLES_PATH: tables, NODE_COMPILE_CACHE: await tempDir() });
  assert.equal(warm.status, 0, warm.stderr);
  const validate = spawnSync(process.execPath, ["dist/server.mjs", "validate-config", "config.yaml"], { cwd: ROOT, encoding: "utf8" });
  assert.equal(validate.status, 0, validate.stderr);
  assert.match(validate.stdout, /^\{"version":1,"config_digest":"[0-9a-f]{64}"\}\n$/);
});

test("end to end: the bundled server process serves a Codex manifest and the models table from fake CPA and AISIX", async (t) => {
  const { spawn } = await import("node:child_process");
  const { createServer } = await import("node:net");
  const { CLIENT_KEY, MANAGEMENT_KEY, fakeCPA, serveCPA } = await import("./fake-cpa.ts");
  const { channels } = await import("./catalog-harness.ts");
  const { json, serve, sleep } = await import("./helpers.ts");
  assert.equal(run(["scripts/bundle.mjs"]).status, 0);
  const cpa = await serveCPA(fakeCPA({
    native: JSON.stringify({ models: [{ slug: "codex/gpt-5", display_name: "GPT-5" }] }),
    channels: channels([{ prefix: "codex", models: ["gpt-5"] }]),
    apiCall: (_payload, res) => json(res, { status_code: 200, body: JSON.stringify({ data: [{ id: "gpt-5", context_length: 272000 }] }) }),
  }));
  t.after(() => cpa.close());
  let aisixAuth = "";
  const aisix = await serve((req, res) => {
    aisixAuth = req.headers.authorization ?? "";
    json(res, { data: [{ id: "codex/gpt-5" }, { id: "router/extra" }] });
  });
  t.after(() => aisix.close());
  const dir = await tempDir();
  await writeFile(join(dir, "config.yaml"), `cpa_base_url: ${cpa.url}\nmetadata:\n  chain: ["@cpa-upstream", "@self"]\n`);
  const port = await new Promise<number>((resolve) => {
    const probe = createServer().listen(0, "127.0.0.1", () => {
      const { port } = probe.address() as { port: number };
      probe.close(() => resolve(port));
    });
  });
  const child = spawn(process.execPath, ["dist/server.mjs"], {
    cwd: ROOT,
    stdio: ["ignore", "pipe", "pipe"],
    env: {
      PATH: process.env.PATH,
      CONFIG_PATH: join(dir, "config.yaml"),
      CACHE_DIR: join(dir, "cache"),
      LISTEN_ADDR: `127.0.0.1:${port}`,
      CPA_MANAGEMENT_KEY: MANAGEMENT_KEY,
      CPA_CLIENT_KEY: CLIENT_KEY,
      AISIX_MODELS_URL: aisix.url,
      AISIX_TOKEN: "aisix-secret",
    },
  });
  let logs = "";
  child.stdout.on("data", (chunk) => (logs += chunk));
  child.stderr.on("data", (chunk) => (logs += chunk));
  const exited = new Promise<number | null>((resolve) => child.once("exit", resolve));
  t.after(() => child.kill("SIGKILL"));
  const base = `http://127.0.0.1:${port}`;
  let ready = false;
  for (let i = 0; i < 200 && !ready; i++) {
    ready = await fetch(`${base}/readyz`).then((res) => res.ok, () => false);
    if (!ready) await sleep(25);
  }
  assert.ok(ready, logs);
  const manifest = await (await fetch(`${base}/v1/models?client_version=0.130.0`)).json() as { models: Array<Record<string, unknown>> };
  assert.deepEqual(manifest.models.map((model) => model.slug), ["codex/gpt-5", "router/extra"]);
  assert.equal(manifest.models[0]!.context_window, 272000);
  assert.equal(aisixAuth, "Bearer aisix-secret");
  const table = await (await fetch(`${base}/models-table/refresh`, { method: "POST" })).text();
  assert.match(table, /刷新成功/);
  assert.match(table, /<td>codex\/gpt-5<\/td>/);
  assert.match(table, /<td>272000<\/td>/);
  child.kill("SIGTERM");
  assert.equal(await exited, 0, logs);
  assert.match(logs, /"msg":"shutdown","signal":"SIGTERM"/);
  assert.doesNotMatch(logs, new RegExp(`${MANAGEMENT_KEY}|${CLIENT_KEY}|aisix-secret`));
});
