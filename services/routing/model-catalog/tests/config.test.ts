import assert from "node:assert/strict";
import { test } from "node:test";
import { ConfigError, parseConfig, parseDuration, syncedSlugs } from "../src/config.ts";

const base = "cpa_base_url: http://cpa:8317\n";
const rejects = (yaml: string, pattern: RegExp) => assert.throws(() => parseConfig(base + yaml), (error: unknown) => error instanceof ConfigError && pattern.test(error.message));

test("every legacy field is rejected with its path and a migration hint", () => {
  const cases: Array<[string, RegExp]> = [
    ["source_priority: [models.dev/openai]", /^source_priority: removed field; use metadata\.chain/],
    ["provider_prefix_map: {axis: openai}", /^provider_prefix_map: removed field; use literal provider ids/],
    ["lookup_ids: {}", /^lookup_ids: removed field; use a literal 4th chain segment/],
    ["global_fallback: false", /^global_fallback: removed field/],
    ["exclude_models: [x]", /^exclude_models: removed field; use metadata\.include_models/],
    ["fetch_models: false", /^fetch_models: removed field/],
    ["bare_models_takeover: true", /^bare_models_takeover: removed field; .*\*\/\*\*/],
    ["channels:\n  xl:\n    fetch_models: false", /^channels\.xl\.fetch_models: removed field/],
    ["channels:\n  xl:\n    source_priority: []", /^channels\.xl\.source_priority: removed field; use metadata\.chain/],
    ["channels:\n  xl:\n    include: [a]", /^channels\.xl\.include: removed field; use metadata\.include_models/],
    ["channels:\n  xl:\n    exclude: [a]", /^channels\.xl\.exclude: removed field/],
    ["channels:\n  xl:\n    overrides: {}", /^channels\.xl\.overrides: removed field; use metadata\.models rules/],
    ["channels:\n  xl:\n    models: {}", /^channels\.xl\.models: removed field; .*root metadata_from map/],
    ["channels:\n  xl:\n    provider_prefix_map: {}", /^channels\.xl\.provider_prefix_map: removed field/],
    ["channels:\n  xl:\n    global_fallback: true", /^channels\.xl\.global_fallback: removed field/],
    ["channels:\n  xl:\n    sync_max_context_window: true", /^channels\.xl\.sync_max_context_window: removed field; use the root sync_max_context_window glob list/],
    ["static_models:\n  - slug: a\n    source_priority: []", /^static_models\[0\]\.source_priority: removed field/],
    ["chain: [\"@self\"]", /^chain: removed field; move it under metadata\.chain/],
    ["include_models: [\"**\"]", /^include_models: removed field; move it under metadata/],
  ];
  for (const [yaml, pattern] of cases) rejects(yaml, pattern);
});

test("shared metadata errors carry the metadata. path prefix", () => {
  rejects('metadata:\n  chain: ["@ollama_cloud"]', /^metadata\.chain item "@ollama_cloud".*@self.*@cpa-upstream/);
  rejects("metadata:\n  include_models: []", /^metadata\.include_models/);
  rejects("metadata:\n  chain: []", /^metadata\.chain/);
  rejects('metadata:\n  models:\n    - match: "a/**"\n      overrides: {slug: x}', /^metadata\.models\[0\]\.overrides\.slug: overrides may not set slug/);
  rejects('metadata:\n  models:\n    - match: "a/**"\n      overrides: {id: x}', /^metadata\.models\[0\]\.overrides\.id/);
  rejects("metadata:\n  typo: 1", /^metadata\.typo: unknown field/);
});

test("reference cycles through inherit and metadata_from are rejected", () => {
  rejects("static_models:\n  - slug: a\n    inherit: [b]\n  - slug: b\n    inherit: [a]", /reference cycle a -> b -> a/);
  rejects("static_models:\n  - slug: a\n    inherit: [c]\nmetadata_from:\n  c: a", /reference cycle/);
  rejects("metadata_from:\n  a: a", /reference cycle a -> a/);
  assert.doesNotThrow(() => parseConfig(`${base}static_models:\n  - slug: a\n    inherit: [b]\n  - slug: c\n    inherit: [b]\nmetadata_from:\n  d: b`));
});

test("gateway fields: defaults, validation and unknown keys", () => {
  const config = parseConfig(base);
  assert.equal(config.httpConcurrency, 8);
  assert.equal(config.channelTimeoutMs, 8_000);
  assert.equal(config.overallDeadlineMs, 25_000);
  assert.equal(config.scheduledRefreshMs, 30 * 60_000);
  assert.equal(config.entryRefreshWindowMs, 10 * 60_000);
  assert.equal(config.aisixTimeoutMs, 8_000);
  assert.equal(config.metadata.chain, undefined, "absent chain uses the shared default");

  const tuned = parseConfig(`${base}entry_refresh_window: 2h\nscheduled_refresh_interval: 1m\naisix_timeout: 1h\nsync_max_context_window: true\nchannels:\n  a:\n    path: /v1/models\n`);
  assert.equal(tuned.entryRefreshWindowMs, 2 * 3_600_000);
  assert.equal(tuned.scheduledRefreshMs, 60_000);
  assert.equal(tuned.aisixTimeoutMs, 25_000, "AISIX budget is capped by the overall deadline");
  assert.deepEqual([...syncedSlugs(tuned, ["a/m", "bare"])], ["a/m", "bare"]);
  assert.equal(config.syncMaxContextWindow, false, "absent selects none");
  assert.equal(syncedSlugs(config, ["a/m"]).size, 0);
  assert.equal(tuned.channels.get("a")!.path, "/v1/models");

  rejects("scheduled_refresh_interval: 0s", /scheduled_refresh_interval: must be positive/);
  rejects("entry_refresh_window: -1m", /entry_refresh_window: expected a duration/);
  rejects("entry_refresh_window: 10", /entry_refresh_window: expected a duration/);
  rejects("http_concurrency: 0", /http_concurrency/);
  rejects("typo: 1", /^typo: unknown field/);
  rejects("channels:\n  a:\n    bogus: 1", /^channels\.a\.bogus: unknown field/);
  rejects("static_models:\n  - slug: a\n    inherit: b", /inherit: expected an ordered list/);
  rejects("static_models:\n  - slug: a\n  - slug: a", /duplicate static model/);
  rejects("static_models:\n  - slug: a\n    overrides: {slug: b}", /overrides may not set slug/);
  rejects("custom_channels:\n  pool:\n    models: {m: {}}", /custom_channels\.pool\.models: expected a list/);
  rejects("custom_channels:\n  a: {models: [m]}\nchannels:\n  a: {}", /custom_channels\.a: also defined under channels\.a/);
  assert.throws(() => parseConfig("http_concurrency: 1\n"), /cpa_base_url/);
  assert.throws(() => parseConfig(`${base}cpa_base_url: x\n`), /invalid YAML/);
  assert.equal(parseDuration("1h30m", "x"), 5_400_000);
  assert.equal(parseDuration("1.5s", "x"), 1_500);
});

test("sync_max_context_window: boolean or an ordered glob list with include_models semantics", () => {
  const listed = parseConfig(`${base}sync_max_context_window: ["xl/**", "!xl/keep-*", "zcode/glm-?"]\n`);
  assert.deepEqual([...syncedSlugs(listed, ["xl/a", "xl/keep-b", "xl/sub/c", "aihub/c", "zcode/glm-5", "zcode/glm-55", "bare"])], ["xl/a", "xl/sub/c", "zcode/glm-5"]);
  const reincluded = parseConfig(`${base}sync_max_context_window: ["!xl/keep-*", "xl/**"]\n`);
  assert.deepEqual([...syncedSlugs(reincluded, ["xl/keep-b"])], ["xl/keep-b"], "the last matching entry decides");
  assert.equal(parseConfig(`${base}sync_max_context_window: false\n`).syncMaxContextWindow, false);
  rejects("sync_max_context_window: []", /^sync_max_context_window: an empty list selects no model/);
  rejects("sync_max_context_window: [\"\"]", /^sync_max_context_window\[0\]: expected a non-empty glob/);
  rejects("sync_max_context_window: [\"!\"]", /^sync_max_context_window\[0\]: `!` must be followed/);
  rejects("sync_max_context_window: [1]", /^sync_max_context_window\[0\]: expected a non-empty glob/);
  rejects("sync_max_context_window: yes-please", /^sync_max_context_window: expected true, false or a list/);
});

test("membership is a legacy field: every shape points to EXTRA_CPA in catalog.lua", () => {
  for (const value of ['membership:\n  cpa_include: ["devin/*"]', "membership: {}", "membership: [devin/*]"]) {
    rejects(value, /^membership: removed field; .*move cpa_include entries to EXTRA_CPA in catalog\.lua/);
  }
  rejects("bare_models_takeover: true", /^bare_models_takeover: removed field; .*EXTRA_CPA in catalog\.lua/);
});

test("the tracked example configuration is valid", async () => {
  const { readFile } = await import("node:fs/promises");
  const config = parseConfig(await readFile(new URL("../config.yaml", import.meta.url), "utf8"));
  assert.equal(config.metadata.chain?.at(-1), "@self", "sources first, CPA's row last (Go precedence)");
  assert.equal(config.syncMaxContextWindow, false, "the example does not synchronize upper bounds");
  assert.ok(config.metadata.models?.some((rule) => rule.match === "**/gpt-image-*"));
});
