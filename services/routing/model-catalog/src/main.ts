// Entry point: `/model-catalog` runs the server; subcommands `healthcheck`, `validate-config` and
// `warm-compile-cache` keep the Compose/Ansible command interface.
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { get } from "node:http";
import { flushCompileCache } from "node:module";
import { fileURLToPath } from "node:url";
import { AisixClient, AuthorityOwner } from "./authority.ts";
import type { BuildDeps } from "./build.ts";
import { ConfigError, parseConfig, type GatewayConfig } from "./config.ts";
import { parseMetadataConfigValue, runCompileWorker } from "./core.ts";
import { CpaClient } from "./cpa.ts";
import { DiskCache } from "./disk-cache.ts";
import { HttpPool } from "./http.ts";
import { dryRun, LIMITS, membershipScript, MembershipScriptError, readMembershipScript, type MembershipScript } from "./membership.ts";
import { OutputStore } from "./outputs.ts";
import { errorMessage, jsonLogger } from "./log.ts";
import { ReadCache } from "./read-cache.ts";
import { createHTTPServer, parseListenAddr } from "./server.ts";
import { CatalogService, PINNED_KEYS } from "./service.ts";

export const VALIDATE_CONFIG_VERSION = 1;
export const STDIN_LIMIT = 16 << 20;

// Source runs (tests, `node src/main.ts`) use the .ts worker; the bundle ships dist/worker.mjs.
export const WORKER_FILE = new URL(import.meta.url.endsWith(".ts") ? "./worker.ts" : "./worker.mjs", import.meta.url);
export const DEFAULT_TABLES_PATH = fileURLToPath(new URL("../data/source-tables.json", import.meta.url));

const env = (key: string, fallback = "") => process.env[key] || fallback;

async function readStdin(limit: number): Promise<Buffer> {
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of process.stdin as AsyncIterable<Buffer>) {
    size += chunk.length;
    if (size > limit) throw new Error(`stdin exceeds ${limit} bytes`);
    chunks.push(chunk);
  }
  return Buffer.concat(chunks, size);
}

export const DEFAULT_SCRIPT_PATH = "/app/catalog.lua";

/** Loads `catalog.lua` and dry-runs it (design D9); warns when it rejects every synthetic candidate. */
async function checkScript(script: MembershipScript, config: GatewayConfig, warn: (text: string) => void): Promise<void> {
  const result = await dryRun(script, config.staticModels.map((model) => model.slug));
  if (result.rejected === result.candidates) warn(`catalog.lua rejected all ${result.rejected} synthetic dry-run candidates; a real collection may publish an empty catalog`);
}

const scriptFailure = (error: unknown) => (error instanceof MembershipScriptError ? error.message : `membership script worker_exit: ${errorMessage(error)}`);

/**
 * `validate-config <config|-> [--script <path|->]`: prints only a schema version and the input
 * digests; never YAML contents, script text or secrets.
 */
export async function validateConfig(args: readonly string[]): Promise<number> {
  const [path, flag, scriptPath, ...rest] = args;
  if (!path || rest.length > 0 || (flag !== undefined && (flag !== "--script" || !scriptPath)) || (path === "-" && scriptPath === "-")) {
    process.stderr.write("usage: model-catalog validate-config <config|-> [--script <path|->] (at most one -)\n");
    return 2;
  }
  let raw: Buffer;
  try {
    raw = path === "-" ? await readStdin(STDIN_LIMIT) : await readFile(path);
  } catch (error) {
    process.stderr.write(`read config: ${errorMessage(error)}\n`);
    return 1;
  }
  let config: GatewayConfig;
  try {
    config = parseConfig(raw.toString("utf8"));
  } catch (error) {
    process.stderr.write(`invalid config: ${error instanceof ConfigError ? error.message : errorMessage(error)}\n`);
    return 1;
  }
  const output: Record<string, unknown> = { version: VALIDATE_CONFIG_VERSION, config_digest: createHash("sha256").update(raw).digest("hex") };
  if (scriptPath) {
    let script: MembershipScript;
    try {
      script = scriptPath === "-" ? membershipScript(await readStdin(LIMITS.scriptBytes)) : await readMembershipScript(scriptPath);
    } catch (error) {
      process.stderr.write(`read script: ${errorMessage(error)}\n`);
      return 1;
    }
    try {
      await checkScript(script, config, (text) => process.stderr.write(`WARN ${text}\n`));
    } catch (error) {
      process.stderr.write(`invalid script: ${scriptFailure(error)}\n`);
      return 1;
    }
    output.script_digest = script.digest;
  }
  process.stdout.write(`${JSON.stringify(output)}\n`);
  return 0;
}

function healthcheck(): Promise<number> {
  return new Promise((resolve) => {
    const req = get("http://127.0.0.1:8090/readyz", { timeout: 2_000 }, (res) => {
      res.resume();
      resolve(res.statusCode === 200 ? 0 : 1);
    });
    req.on("timeout", () => req.destroy(new Error("timeout")));
    req.on("error", () => resolve(1));
  });
}

function workerLimits(): BuildDeps["resourceLimits"] {
  const mb = Number(env("WORKER_MAX_OLD_GENERATION_MB", "160"));
  return Number.isFinite(mb) && mb > 0 ? { maxOldGenerationSizeMb: mb } : undefined;
}

/** Loads every bundle (server, compile and membership workers) once so NODE_COMPILE_CACHE holds their code. */
async function warmCompileCache(): Promise<number> {
  const config = parseMetadataConfigValue({ chain: ["models.dev/@creator/api_key", "modelparams.dev/@creator/api_key", "@self"] }, { recordSources: ["@self"] });
  await runCompileWorker({ workerFile: WORKER_FILE, config, publicIds: ["openai/gpt-5"], tablesPath: env("TABLES_PATH", DEFAULT_TABLES_PATH) });
  await dryRun(await readMembershipScript(env("CATALOG_SCRIPT_PATH", DEFAULT_SCRIPT_PATH)), []);
  flushCompileCache();
  return 0;
}

async function serve(): Promise<number> {
  const log = jsonLogger();
  let config: GatewayConfig;
  try {
    config = parseConfig(await readFile(env("CONFIG_PATH", "/app/config.yaml"), "utf8"));
  } catch (error) {
    log.error("config", { err: errorMessage(error) });
    return 1;
  }
  let script: MembershipScript;
  try {
    script = await readMembershipScript(env("CATALOG_SCRIPT_PATH", DEFAULT_SCRIPT_PATH));
    await checkScript(script, config, (text) => log.warn(text));
  } catch (error) {
    log.error("membership script", { err: scriptFailure(error) });
    return 1;
  }
  log.info("membership script loaded", { script_digest: script.digest });
  const managementKey = env("CPA_MANAGEMENT_KEY");
  const clientKey = env("CPA_CLIENT_KEY", env("CPA_API_KEY"));
  if (!managementKey || !clientKey) {
    log.error("missing CPA_MANAGEMENT_KEY or CPA_CLIENT_KEY");
    return 1;
  }
  const cacheDir = env("CACHE_DIR", "/var/cache/model-catalog");
  let disk: DiskCache;
  let outputs: OutputStore;
  try {
    disk = await DiskCache.open(cacheDir, log);
    outputs = await OutputStore.open(join(cacheDir, "out"), PINNED_KEYS);
  } catch (error) {
    // Same contract as the Go sidecar: the input cache volume must be writable by UID 65532.
    log.error("read cache initialization failed; CACHE_DIR must be a writable directory", { dir: cacheDir, err: errorMessage(error) });
    return 1;
  }
  const cache = new ReadCache(disk, log);
  const deadlineMs = config.overallDeadlineMs + 5_000;
  const pool = new HttpPool(config.httpConcurrency, deadlineMs);
  const cpa = new CpaClient({ baseUrl: config.cpaBaseUrl, managementKey, clientKey, pool, cache, log });
  const aisixURL = env("AISIX_MODELS_URL", config.aisixModelsUrl);
  const aisix = aisixURL ? new AisixClient(aisixURL, env("AISIX_TOKEN"), pool, cache) : undefined;
  const owner = new AuthorityOwner({
    cpa,
    aisix,
    staticSlugs: config.staticModels.map((model) => model.slug),
    script,
    cpaTimeoutMs: deadlineMs,
    aisixTimeoutMs: config.aisixTimeoutMs,
    log,
  });
  const service = new CatalogService({
    owner,
    outputs,
    log,
    build: { config, cpa, workerFile: WORKER_FILE, tablesPath: env("TABLES_PATH", DEFAULT_TABLES_PATH), resourceLimits: workerLimits(), log },
  });
  const listen = parseListenAddr(env("LISTEN_ADDR", ":8090"));
  const server = createHTTPServer(service, log);
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(listen.port, listen.host, resolve);
  });
  log.info("listen", { addr: env("LISTEN_ADDR", ":8090") });
  service.start();
  const signal = await new Promise<string>((resolve) => {
    process.once("SIGTERM", () => resolve("SIGTERM"));
    process.once("SIGINT", () => resolve("SIGINT"));
  });
  log.info("shutdown", { signal });
  server.close();
  server.closeIdleConnections();
  setTimeout(() => server.closeAllConnections(), 5_000).unref();
  await service.stop();
  return 0;
}

export async function main(argv: readonly string[]): Promise<number> {
  const [command, ...args] = argv;
  if (command === "healthcheck" && args.length === 0) return healthcheck();
  if (command === "validate-config") return validateConfig(args);
  if (command === "warm-compile-cache" && args.length === 0) return warmCompileCache();
  if (command === undefined) return serve();
  process.stderr.write("usage: model-catalog [healthcheck | validate-config <config|-> [--script <path|->] | warm-compile-cache]\n");
  return 2;
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  // Explicit exit: pooled keep-alive sockets must not hold a finished command or a stopped server open.
  process.exit(await main(process.argv.slice(2)));
}
