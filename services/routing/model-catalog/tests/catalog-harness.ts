// End-to-end harness: fake CPA (and optional AISIX) + the real config parser, authority owner,
// compile worker (src/worker.ts) and catalog service.
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { AisixClient, AuthorityOwner, type AuthorityOwnerOptions } from "../src/authority.ts";
import type { BuildDeps } from "../src/build.ts";
import { parseConfig } from "../src/config.ts";
import { DiskCache } from "../src/disk-cache.ts";
import type { Row } from "../src/json.ts";
import { OutputStore } from "../src/outputs.ts";
import { ReadCache } from "../src/read-cache.ts";
import { CatalogService, PINNED_KEYS, type Reply } from "../src/service.ts";
import { cpaClient, fakeCPA, serveCPA, type FakeCPA } from "./fake-cpa.ts";
import { captureLog, tempDir, type CapturedLog } from "./helpers.ts";
import { defaultScriptText, script } from "./scripts.ts";

export const WORKER = fileURLToPath(new URL("../src/worker.ts", import.meta.url));

/** models.dev provider tables keyed provider → model → record (shape of the real snapshot). */
export async function writeTables(dir: string, providers: Record<string, Record<string, Row>>): Promise<string> {
  const path = join(dir, "source-tables.json");
  const modelsDevProviders = Object.fromEntries(Object.entries(providers).map(([provider, models]) => [provider, { id: provider, models }]));
  await writeFile(path, JSON.stringify({
    modelsDev: { providers: modelsDevProviders, models: {} },
    modelparams: { catalog: [] },
    modelsDevVersion: "0.0.test",
    modelsDevGeneratedAt: "2026-10-01T00:00:00.000Z",
    modelparamsVersion: "0.0.mp",
    degraded: [],
  }));
  return path;
}

export const channels = (entries: Array<{ prefix: string; models: string[]; base?: string }>) =>
  JSON.stringify({
    "openai-compatibility": entries.map((entry, index) => ({
      name: `ch-${entry.prefix}`,
      prefix: entry.prefix,
      "base-url": entry.base ?? `https://${entry.prefix}.example/v1`,
      "auth-index": `auth-${index}`,
      models: entry.models.map((name) => ({ name })),
    })),
  });

export interface Harness {
  service: CatalogService;
  owner: AuthorityOwner;
  deps: BuildDeps & { workerFile: string };
  fake: FakeCPA;
  log: CapturedLog;
  apiCalls: string[];
  advance(ms: number): void;
}

export interface HarnessOptions {
  yaml: string;
  native: Row[];
  channels: string;
  providers?: Record<string, Record<string, Row>>;
  /** Inner api-call responses by inner URL. */
  upstream?: Record<string, unknown>;
  aisix?: string;
  /** `catalog.lua` text; default: the tracked default script with `extraCpa`. */
  script?: string;
  /** The default script's EXTRA_CPA list. */
  extraCpa?: string[];
  /** Membership worker overrides (bounds). */
  membership?: AuthorityOwnerOptions["membership"];
}

export async function harness(t: { after(fn: () => unknown): void }, options: HarnessOptions): Promise<Harness> {
  const apiCalls: string[] = [];
  const fake = fakeCPA({
    native: JSON.stringify({ models: options.native }),
    channels: options.channels,
    apiCall: (payload, res) => {
      apiCalls.push(`${payload.method} ${payload.url}${payload.data ? ` ${payload.data}` : ""}`);
      const body = options.upstream?.[payload.url];
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify(body === undefined ? { status_code: 404, body: "" } : { status_code: 200, body: JSON.stringify(body) }));
    },
  });
  const server = await serveCPA(fake);
  t.after(() => server.close());
  const dir = await tempDir();
  const log = captureLog();
  let now = Date.now();
  const clock = () => now;
  const cache = new ReadCache(await DiskCache.open(join(dir, "cache"), log), log, clock);
  const { client } = cpaClient(server.url, { cache, log });
  const config = parseConfig(`cpa_base_url: ${server.url}\n${options.yaml}`);
  const aisix = options.aisix ? new AisixClient(options.aisix, "", client.pool, cache) : undefined;
  const owner = new AuthorityOwner({
    cpa: client,
    aisix,
    staticSlugs: config.staticModels.map((m) => m.slug),
    script: script(options.script ?? defaultScriptText(options.extraCpa)),
    membership: options.membership,
    cpaTimeoutMs: 5_000,
    aisixTimeoutMs: 5_000,
    log,
    clock,
  });
  const deps = { config, cpa: client, workerFile: WORKER, tablesPath: await writeTables(dir, options.providers ?? {}), log };
  const outputs = await OutputStore.open(join(dir, "cache", "out"), PINNED_KEYS);
  const service = new CatalogService({ build: deps, owner, outputs, log, clock });
  t.after(() => service.stop());
  return { service, owner, deps, fake, log, apiCalls, advance: (ms) => (now += ms) };
}

/** The full response text: body, published file and tail; releases the file lease. */
export async function replyText(reply: Reply): Promise<string> {
  if (!reply.file) return reply.body;
  try {
    return reply.body + (await readFile(reply.file.path, "utf8")) + reply.file.tail;
  } finally {
    reply.file.release();
  }
}

export async function codex(h: Harness, clientVersion = "1.0.0"): Promise<Row[]> {
  const reply = await h.service.catalog(clientVersion);
  const text = await replyText(reply);
  if (reply.status !== 200) throw new Error(`catalog ${reply.status}: ${text}`);
  return (JSON.parse(text) as { models: Row[] }).models;
}

export const bySlug = (models: Row[]) => new Map(models.map((model) => [model.slug as string, model]));
