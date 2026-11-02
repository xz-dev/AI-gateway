// Runs every Lua example of docs/model-catalog.md "Membership script" against a fixture and checks
// the documented result (task 4.1).
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";
import type { Row } from "../src/json.ts";
import { channels, harness, replyText, type Harness } from "./catalog-harness.ts";
import { json, serve } from "./helpers.ts";
import { defaultScriptText } from "./scripts.ts";

const DOC = new URL("../../../../docs/model-catalog.md", import.meta.url);
const native = (slug: string): Row => ({ slug, display_name: slug });

async function examples(): Promise<string[]> {
  const doc = await readFile(DOC, "utf8");
  const section = doc.slice(doc.indexOf("### Membership script"), doc.indexOf("Source configuration is fully explicit"));
  return [...section.matchAll(/```lua\n([\s\S]*?)```/g)].map((match) => match[1]!);
}

async function run(t: Parameters<typeof harness>[0], script: string): Promise<Harness> {
  const aisix = await serve((_req, res) => json(res, { data: ["gpt-6.1-sol", "codex/gpt-5.3-codex-spark", "router/only"].map((id) => ({ id })) }));
  t.after(() => aisix.close());
  const h = await harness(t, {
    channels: channels([{ prefix: "codex", models: ["gpt-6.1-sol", "gpt-5.3-codex-spark", "gpt-image-1"] }]),
    native: ["codex/gpt-6.1-sol", "gpt-6.1-sol", "codex/gpt-5.3-codex-spark", "codex/gpt-image-1", "deepseek/deepseek-v4.1-flash", "devin/claude-opus-5-5", "devin/model_private_2", "my-alias"].map(native),
    aisix: aisix.url,
    yaml: `metadata:\n  chain: ["@self"]\nstatic_models:\n  - slug: alias/a\n    inherit: [codex/gpt-6.1-sol]\n`,
    script,
  });
  await h.service.catalog("");
  return h;
}

const ids = async (h: Harness) => (JSON.parse(await replyText(await h.service.catalog(""))) as { data: Array<{ id: string }> }).data.map((m) => m.id);
const rejected = (h: Harness) => Object.fromEntries(h.owner.current().rejected.map((r) => [r.id, r.reason]));
const origin = (h: Harness, id: string) => h.owner.current().members.find((m) => m.id === id)?.origin;
const NO_PREFIX = "CPA ID without a CPA prefix";

test("docs examples: admit all, exclude a few, prefer AISIX, extra CPA list", async (t) => {
  const [all, exclude, preferAisix, ...rest] = await examples();
  assert.equal(rest.length, 0, "the section has exactly three Lua examples");

  const admitAll = await run(t, all!);
  assert.equal((await ids(admitAll)).length, 10, "every candidate is a member");

  const excluded = await run(t, exclude!);
  assert.deepEqual(await ids(excluded), ["gpt-6.1-sol", "alias/a", "codex/gpt-6.1-sol", "router/only"]);
  assert.deepEqual(rejected(excluded), {
    "codex/gpt-5.3-codex-spark": "excluded",
    "codex/gpt-image-1": "excluded",
    "deepseek/deepseek-v4.1-flash": NO_PREFIX,
    "devin/claude-opus-5-5": NO_PREFIX,
    "devin/model_private_2": NO_PREFIX,
    "my-alias": NO_PREFIX,
  });

  const aisix = await run(t, preferAisix!);
  assert.equal(origin(aisix, "codex/gpt-5.3-codex-spark"), "aisix", "the AISIX route wins on a shared ID");
  assert.equal(origin(aisix, "codex/gpt-6.1-sol"), "cpa");
  assert.deepEqual(await ids(aisix), ["gpt-6.1-sol", "alias/a", "codex/gpt-5.3-codex-spark", "codex/gpt-6.1-sol", "codex/gpt-image-1", "router/only"]);

  // The extra CPA list example in the prose: { "devin/*", "!devin/model_private_*" }.
  const doc = await readFile(DOC, "utf8");
  assert.match(doc, /`\{ "devin\/\*", "!devin\/model_private_\*" \}` admits Devin IDs except the private ones/);
  const extra = await run(t, defaultScriptText(["devin/*", "!devin/model_private_*"]));
  assert.ok((await ids(extra)).includes("devin/claude-opus-5-5"));
  assert.equal(rejected(extra)["devin/model_private_2"], NO_PREFIX);
});
