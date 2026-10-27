import assert from "node:assert/strict";
import { test } from "node:test";
import { parseJSON, type Row } from "../src/json.ts";
import { omitConflictingReasoningDefault, postprocess, raiseMaxContextWindow, type PostprocessInput } from "../src/postprocess.ts";
import { captureLog } from "./helpers.ts";

const levels = (...efforts: string[]) => efforts.map((effort) => ({ effort }));

function run(entries: Array<[string, Row]>, extra: Partial<PostprocessInput> = {}) {
  return postprocess({
    entries: new Map(entries),
    preserveNative: new Set(),
    overrides: new Map(),
    metadataFrom: new Map(),
    pool: new Map(),
    staticModels: [],
    syncMaxContextWindow: () => false,
    ...extra,
  });
}

test("reasoning default is removed only for a proven conflict", () => {
  const cases: Array<[string, string, boolean]> = [
    ["conflict", `{"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"high"}],"unknown":null}`, true],
    ["empty support list", `{"default_reasoning_level":"medium","supported_reasoning_levels":[]}`, true],
    ["supported default", `{"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"high"},{"effort":"medium"}]}`, false],
    ["missing list", `{"default_reasoning_level":"medium"}`, false],
    ["null list", `{"default_reasoning_level":"medium","supported_reasoning_levels":null}`, false],
    ["unknown list shape", `{"default_reasoning_level":"medium","supported_reasoning_levels":false}`, false],
    ["unknown list item", `{"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"high"},{}]}`, false],
    ["empty effort", `{"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":""}]}`, false],
    ["missing default", `{"supported_reasoning_levels":[{"effort":"high"}]}`, false],
    ["null default", `{"default_reasoning_level":null,"supported_reasoning_levels":[{"effort":"high"}]}`, false],
    ["false default", `{"default_reasoning_level":false,"supported_reasoning_levels":[{"effort":"high"}]}`, false],
    ["zero default", `{"default_reasoning_level":0,"supported_reasoning_levels":[{"effort":"high"}]}`, false],
    ["empty default", `{"default_reasoning_level":"","supported_reasoning_levels":[{"effort":"high"}]}`, false],
  ];
  for (const [name, raw, omit] of cases) {
    const row = parseJSON(raw) as Row;
    const want = parseJSON(raw) as Row;
    if (omit) delete want.default_reasoning_level;
    omitConflictingReasoningDefault(row, undefined);
    assert.deepEqual(row, want, name);
  }
  const explicit = parseJSON(cases[0]![1]) as Row;
  omitConflictingReasoningDefault(explicit, { default_reasoning_level: "medium" });
  assert.equal(explicit.default_reasoning_level, "medium");
});

test("reasoning cleanup runs after references and respects explicit overrides", () => {
  const out = run(
    [
      ["demo/reference", { slug: "demo/reference", supported_reasoning_levels: levels("medium") }],
      ["demo/target", { slug: "demo/target", default_reasoning_level: "medium", supported_reasoning_levels: levels("high") }],
      ["demo/manual-default", { slug: "demo/manual-default", default_reasoning_level: "medium", supported_reasoning_levels: levels("high") }],
      ["demo/manual-list", { slug: "demo/manual-list", default_reasoning_level: "medium", supported_reasoning_levels: levels("medium") }],
      ["demo/conflict", { slug: "demo/conflict", default_reasoning_level: "xhigh", supported_reasoning_levels: levels("low", "high") }],
    ],
    {
      metadataFrom: new Map([["demo/target", "demo/reference"]]),
      overrides: new Map([
        ["demo/manual-default", { default_reasoning_level: "medium" }],
        ["demo/manual-list", { supported_reasoning_levels: levels("medium") }],
      ]),
    },
  );
  for (const slug of ["demo/target", "demo/manual-default", "demo/manual-list"]) assert.equal(out.get(slug)!.default_reasoning_level, "medium", slug);
  assert.equal("default_reasoning_level" in out.get("demo/conflict")!, false);
});

test("preserved CPA records pass through untouched", () => {
  const row = { slug: "demo/m", default_reasoning_level: "medium", supported_reasoning_levels: levels("high"), context_window: 10, unknown: null };
  const before = JSON.stringify(row);
  const out = run([["demo/m", row]], { preserveNative: new Set(["demo/m"]), syncMaxContextWindow: () => true, metadataFrom: new Map([["demo/m", "x/y"]]) });
  assert.equal(JSON.stringify(out.get("demo/m")), before);
});

test("metadata_from reads a fixed snapshot and keeps the model's own overrides on top", () => {
  const entries: Array<[string, Row]> = [
    ["c/A", { slug: "c/A", id: "raw-A", max_output_tokens: 10, vendor: { a: true } }],
    ["c/B", { slug: "c/B", id: "raw-B", max_tokens: 20, input_modalities: ["text", "image"], output_modalities: ["text"], vendor: { b: true, overrideB: true } }],
    ["c/C", { slug: "c/C", id: "raw-C", max_output_tokens: 30, input_modalities: [], vendor: { c: true } }],
  ];
  for (const order of [entries, [...entries].reverse()]) {
    const log = captureLog();
    const out = run(order.map(([slug, row]) => [slug, structuredClone(row)]), {
      metadataFrom: new Map([["c/A", "c/B"], ["c/B", "c/C"], ["c/D", "c/A"], ["c/C", "missing/x"]]),
      overrides: new Map([
        ["c/A", { max_tokens: 40, id: "wrong", slug: "wrong", vendor: { target: false } }],
        ["c/B", { vendor: { overrideB: true } }],
      ]),
      log,
    });
    assert.deepEqual(out.get("c/A"), { slug: "c/A", id: "c/A", max_output_tokens: 10, vendor: { a: true, b: true, overrideB: true, target: false }, max_tokens: 40, input_modalities: ["text", "image"], output_modalities: ["text"] });
    assert.deepEqual(out.get("c/B"), { slug: "c/B", id: "c/B", max_tokens: 20, input_modalities: [], output_modalities: ["text"], vendor: { b: true, overrideB: true, c: true }, max_output_tokens: 30 });
    assert.equal(out.has("c/D"), false);
    assert.match(log.text(), /metadata reference missing/);
    // Shared arrays are copied, so later edits to the source do not leak into the referrer.
    (out.get("c/B")!.output_modalities as string[]).push("mutated");
    assert.deepEqual(out.get("c/A")!.output_modalities, ["text"]);
  }
});

test("static models inherit in order from catalog and hidden pool, patch with RFC 7396 and protect identity", () => {
  const log = captureLog();
  const pool = new Map<string, Row>([["pool/m", { id: "database-id", vendor: { pool: true }, input_modalities: [], output_modalities: ["image"], max_output_tokens: 50 }]]);
  const out = run(
    [
      ["public/new", { slug: "public/new", max_tokens: 100, vendor: { existing: true }, drop: "me" }],
      ["c/channel", { slug: "c/channel", vendor: { lower: true }, input_modalities: ["text", "image"], output_modalities: ["text"] }],
    ],
    {
      pool,
      log,
      staticModels: [
        { slug: "public/new", inherit: ["pool/m", "missing/x", "c/channel"], overrides: { max_tokens: 0, id: "wrong", slug: "wrong", drop: null, vendor: { final: true } } },
        { slug: "alias/next", inherit: ["public/new"] },
      ],
    },
  );
  assert.deepEqual([...out.keys()], ["public/new", "c/channel", "alias/next"]);
  for (const slug of ["public/new", "alias/next"]) {
    assert.deepEqual(out.get(slug), { slug, id: slug, max_tokens: 0, vendor: { existing: true, lower: true, pool: true, final: true }, input_modalities: [], output_modalities: ["image"], max_output_tokens: 50 }, slug);
  }
  assert.match(log.text(), /static inherit target missing/);
});

test("static inheritance from pool and catalog drops conflicting defaults unless set explicitly", () => {
  const conflicting = () => ({ default_reasoning_level: "medium", supported_reasoning_levels: levels("high") });
  const out = run([["native/parent", { slug: "native/parent", ...conflicting() }]], {
    pool: new Map([["pool/m", conflicting()]]),
    staticModels: [
      { slug: "from-pool", inherit: ["pool/m"] },
      { slug: "from-native", inherit: ["native/parent"] },
      { slug: "manual", inherit: ["native/parent"], overrides: { default_reasoning_level: "medium" } },
      { slug: "manual-list", inherit: ["native/parent"], overrides: { supported_reasoning_levels: levels("medium") } },
    ],
  });
  assert.equal(out.size, 5, "pool members must never be emitted");
  for (const slug of ["native/parent", "from-pool", "from-native"]) assert.equal("default_reasoning_level" in out.get(slug)!, false, slug);
  assert.equal(out.get("manual")!.default_reasoning_level, "medium");
  // Every chain-built entry is cleaned before statics run, so a static inherits the parent's cleaned record.
  assert.equal("default_reasoning_level" in out.get("manual-list")!, false);
});

test("max_context_window: sync equals context, otherwise only raised; explicit overrides are not synchronized", () => {
  const row = (max: number) => ({ context_window: 200000, max_context_window: max });
  const out = run(
    [
      ["s/big", row(1000000)],
      ["s/small", row(100)],
      ["n/big", row(1000000)],
      ["s/explicit", row(1000000)],
      ["s/static", {}],
    ],
    {
      syncMaxContextWindow: (slug) => slug.startsWith("s/"),
      overrides: new Map([["s/explicit", { max_context_window: 1000000 }]]),
      staticModels: [{ slug: "s/static", inherit: [], overrides: { context_window: 500000, max_context_window: 1000000 } }],
    },
  );
  assert.equal(out.get("s/big")!.max_context_window, 200000);
  assert.equal(out.get("s/small")!.max_context_window, 200000);
  assert.equal(out.get("n/big")!.max_context_window, 1000000);
  assert.equal(out.get("s/explicit")!.max_context_window, 1000000);
  assert.equal(out.get("s/static")!.max_context_window, 1000000);
  const bare: Row = { context_window: 0 };
  raiseMaxContextWindow(bare, true);
  assert.equal("max_context_window" in bare, false);
});
