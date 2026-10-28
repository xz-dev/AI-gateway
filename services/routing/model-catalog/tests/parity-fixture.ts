// Parity fixture: synthetic IDs in the shape of the 2026-10-27 production authority (design.md):
// 331 native IDs (274 under a CPA prefix, 57 `devin/*`), 18 key channels, prefixed codex/kimi/xai
// OAuth accounts, an unprefixed Devin account, and 92 AISIX IDs (66 prefixed, 26 bare or
// `devin/*`, 46 also in CPA). The forced variant is today's CPA; the default variant adds the bare
// copies CPA's applyModelPrefixes registers when force-model-prefix is false.
import assert from "node:assert/strict";
import type { Row } from "../src/json.ts";
import { channels, harness, type Harness, type HarnessOptions } from "./catalog-harness.ts";
import { json, serve } from "./helpers.ts";

interface Route {
  prefix: string;
  original: string;
}

const KEY_CHANNELS = 18;
export const OAUTH = ["codex", "kimi", "supergrok"];
export const routes: Route[] = [];
// 274 prefixed IDs: key channels carry some upstream slash originals (`deepseek/…`), every
// prefixed source carries image models that include_models hides.
for (let i = 0; i < 274; i++) {
  const source = i % (KEY_CHANNELS + OAUTH.length);
  const prefix = source < KEY_CHANNELS ? `k${String(source).padStart(2, "0")}` : OAUTH[source - KEY_CHANNELS]!;
  const original = i % 37 === 0 ? `gpt-image-${i}` : source < KEY_CHANNELS && i % 5 === 0 ? `deepseek/model-${i}` : `model-${i}`;
  routes.push({ prefix, original });
}
export const prefixed = routes.map((route) => `${route.prefix}/${route.original}`);
export const devin = Array.from({ length: 57 }, (_, i) => `devin/model-${i}`);
assert.equal(prefixed.length + devin.length, 331);

const row = (slug: string): Row => ({ slug, display_name: slug, context_window: 1000 + slug.length });
export const forcedRows = [...prefixed, ...devin].map(row);
// Default listing: `x` before `P/x` per route, as applyModelPrefixes registers them.
export const defaultRows = [...routes.flatMap((route) => [row(route.original), row(`${route.prefix}/${route.original}`)]), ...devin.map(row)];

// AISIX: 40 prefixed and 6 devin IDs shared with CPA, 26 prefixed AISIX-only IDs, 20 bare
// logical IDs; 5 of those are bare originals the default CPA variant also lists (D5).
export const bareLogical = [...routes.filter((route) => !route.original.includes("/")).slice(1, 6).map((route) => route.original), ...Array.from({ length: 15 }, (_, i) => `logical-${i}`)];
export const aisixIDs = [
  ...prefixed.slice(0, 40),
  ...devin.slice(0, 6),
  ...Array.from({ length: 26 }, (_, i) => `k00/aisix-only-${i}`),
  ...bareLogical,
];
assert.equal(aisixIDs.length, 92);
assert.equal(new Set(aisixIDs).size, 92);

export function accounts(forced: boolean, failForce = false) {
  const registered = new Map<string, string[]>(OAUTH.map((prefix) => [`${prefix}.json`, []]));
  for (const route of routes) {
    const list = registered.get(`${route.prefix}.json`);
    if (!list) continue;
    if (!forced) list.push(route.original);
    list.push(`${route.prefix}/${route.original}`);
  }
  registered.set("devin.json", devin);
  return (req: { url?: string }, res: Parameters<typeof json>[0]) => {
    const url = new URL(req.url!, "http://cpa");
    if (url.pathname === "/v0/management/force-model-prefix") return failForce ? json(res, {}, 500) : json(res, { "force-model-prefix": forced });
    if (url.pathname === "/v0/management/auth-files") return json(res, { files: [...registered.keys()].map((name) => ({ name })) });
    const ids = registered.get(url.searchParams.get("name") ?? "");
    if (url.pathname !== "/v0/management/auth-files/models" || !ids) return json(res, {}, 404);
    json(res, { models: ids.map((id) => ({ id })) });
  };
}

export const EXTRA_CPA = ["devin/*"];
export const HIDES = ["**/gpt-image-*"];
export const YAML = `metadata:
  chain: ["@self"]
  include_models: ["**", "!**/gpt-image-*"]
static_models:
  - slug: alias/a
    inherit: [k00/model-21]
  - slug: alias/b
    inherit: [codex/model-18]
`;

export type Mode = "forced" | "prefix" | "fallback";

/** A harness on the fixture in one prefix mode; `options` override the YAML, script and limits. */
export async function fixtureHarness(t: Parameters<typeof harness>[0], mode: Mode, options: Partial<HarnessOptions> = {}): Promise<Harness> {
  const aisix = await serve((_req, res) => json(res, { data: aisixIDs.map((id) => ({ id })) }));
  t.after(() => aisix.close());
  const byChannel = new Map<string, string[]>();
  for (const route of routes) if (route.prefix.startsWith("k")) byChannel.set(route.prefix, [...(byChannel.get(route.prefix) ?? []), route.original]);
  const h = await harness(t, {
    channels: channels([...byChannel].map(([prefix, models]) => ({ prefix, models }))),
    native: mode === "forced" ? forcedRows : defaultRows,
    aisix: aisix.url,
    yaml: YAML,
    extraCpa: EXTRA_CPA,
    ...options,
  });
  h.fake.identity = accounts(mode === "forced", mode === "fallback");
  return h;
}
