// Membership script sandbox, bounds, return validation and the default script (design D3, D5–D7).
import assert from "node:assert/strict";
import { test } from "node:test";
import { LIMITS, MembershipScriptError, runMembershipScript, type Candidate, type Decision } from "../src/membership.ts";
import { serve } from "./helpers.ts";
import { DEFAULT_SCRIPT_TEXT, defaultScript, script } from "./scripts.ts";

const cand = (id: string, extra: Partial<Candidate> = {}): Candidate => ({ id, sources: { cpa: true }, mode: "prefix", has_prefix: false, native: false, ...extra });

async function fails(text: string, candidates: Candidate[], errorClass: string, pattern?: RegExp): Promise<MembershipScriptError> {
  try {
    await runMembershipScript(script(text), candidates);
  } catch (error) {
    assert.ok(error instanceof MembershipScriptError, String(error));
    assert.equal(error.errorClass, errorClass, error.message);
    if (pattern) assert.match(error.message, pattern);
    return error;
  }
  assert.fail(`expected ${errorClass}`);
}

const REMOVED = ["io", "os", "package", "require", "load", "loadstring", "loadfile", "dofile", "debug", "coroutine", "collectgarbage", "print", "warn"];

test("sandbox: host access is removed and only plain Lua values are reachable", async () => {
  const text = `
local names = { ${REMOVED.map((name) => JSON.stringify(name)).join(", ")} }
local present = {}
for _, name in ipairs(names) do if _G[name] ~= nil then present[#present + 1] = name end end
local globals = {}
for key in pairs(_G) do globals[#globals + 1] = key end
table.sort(globals)
local LISTED = table.concat(globals, ",")
function select(m)
  local plain = type(m) == "table" and getmetatable(m) == nil and type(m.sources) == "table" and getmetatable(m.sources) == nil
  for _, v in pairs(m) do if type(v) ~= "string" and type(v) ~= "boolean" and type(v) ~= "table" then plain = false end end
  return false, table.concat(present, ",") .. "|" .. tostring(plain) .. "|" .. LISTED
end`;
  const { decisions } = await runMembershipScript(script(text), [cand("a")]);
  const [present, plain, listed] = (decisions[0] as { reason: string }).reason.split("|");
  assert.equal(present, "", "removed names are nil");
  assert.equal(plain, "true", "m is a plain copied table");
  assert.deepEqual(listed!.split(","), [
    "_G", "_VERSION", "assert", "error", "getmetatable", "glob", "ipairs", "math", "next", "pairs", "pcall", "rawequal", "rawget", "rawlen", "rawset",
    "setmetatable", "string", "table", "tonumber", "tostring", "type", "utf8", "xpcall",
  ]);
  for (const name of ["io.open('x')", "os.getenv('HOME')", "require('x')", "load('return 1')", "debug.sethook()"]) {
    await fails(`function select(m) return ${name} end`, [cand("a")], "runtime", /attempt to (call|index) a nil value/);
  }
});

test("bounds: endless loops stop at their instruction bound; the collection total and memory are bounded", async () => {
  await fails("while true do end function select(m) return true end", [cand("a")], "instruction_limit", /load instruction bound/);
  const error = await fails("function select(m) while true do end end", [cand("a"), cand("b")], "instruction_limit", /per-call instruction bound \(100000\)/);
  assert.equal(error.candidate, "a");
  // A pcall cannot swallow the bound: the attempt is still a script error.
  await fails("function select(m) pcall(function() while true do end end) return true end", [cand("a")], "instruction_limit");
  const busy = "function select(m) for i = 1, 14000 do local x = i end return true end";
  const one = await runMembershipScript(script(busy), [cand("a")], { hookStep: 1 });
  assert.ok(one.instructions.maxCall >= 28_000 && one.instructions.maxCall <= 32_000, `one call uses ${one.instructions.maxCall} instructions`);
  const many = Array.from({ length: 1_000 }, (_, i) => cand(`m-${i}`));
  const total = await fails(busy, many, "instruction_limit", /per-collection instruction bound \(20000000\)/);
  assert.ok(Number(total.candidate!.slice(2)) < 1_000, "the bound trips before the last candidate");
  await fails("local t = {} function select(m) for i = 1, 64 do t[i] = string.rep('x', 1 << 20) .. i end return true end", [cand("a")], "memory_limit");
});

test("bounds: a long C call is terminated at the wall-time bound while the main thread keeps serving", async (t) => {
  const server = await serve((_req, res) => {
    res.end("ok");
  });
  t.after(() => server.close());
  const started = performance.now();
  const run = runMembershipScript(script(`function select(m) return string.find(string.rep("a", 1 << 16), ".-.-.-.-.-.-.-b") ~= nil end`), [cand("a")]).catch((error) => error);
  await new Promise((resolve) => setTimeout(resolve, 200));
  const answer = await fetch(server.url).then((res) => res.text());
  const answeredAfter = performance.now() - started;
  assert.equal(answer, "ok");
  const error = await run;
  const elapsed = performance.now() - started;
  assert.ok(error instanceof MembershipScriptError && error.errorClass === "wall_time", String(error));
  assert.ok(answeredAfter < 1_000, `HTTP answered after ${answeredAfter} ms`);
  assert.ok(elapsed >= LIMITS.wallMs && elapsed < LIMITS.wallMs + 1_500, `terminated after ${elapsed} ms`);
});

test("returns: every invalid shape is a script error; reasons are cut at 200 bytes", async () => {
  for (const value of ["nil", "1", "'yes'", "{}", "true, {source = 'cpa', extra = 1}", "true, {source = 1}", "true, {compile = 'yes'}", "true, 'x'", "false, 1", "true, nil, 3"]) {
    await fails(`function select(m) return ${value} end`, [cand("a")], "invalid_return");
  }
  await fails("function select(m) return true, {source = 'aisix'} end", [cand("a")], "invalid_return", /not a source of this candidate/);
  await fails("function select(m) return true, {source = 'other'} end", [cand("a")], "invalid_return");
  await fails("x = 1", [cand("a")], "missing_select");
  await fails("function select(m) return 'codex/new' end", [cand("a")], "invalid_return");
  const syntax = await fails("local SECRET_TOKEN = (", [cand("a")], "load");
  assert.doesNotMatch(syntax.message, /SECRET_TOKEN/, "no script text in the error");
  const { decisions } = await runMembershipScript(
    script(`function select(m) if m.id == "long" then return false, string.rep("a", 199) .. "é" .. string.rep("b", 50) end if m.id == "none" then return false end return true, {compile = true} end`),
    [cand("long"), cand("none"), cand("keep", { sources: { aisix: true } })],
  );
  assert.deepEqual(decisions, [{ keep: false, reason: "a".repeat(199) }, { keep: false, reason: undefined }, { keep: true, source: undefined, compile: true }]);
});

test("glob(): the include_models matcher with segment semantics; no leading !; invalid patterns fail", async () => {
  const { decisions } = await runMembershipScript(
    script(`function select(m) return false, tostring(glob("devin/*", "devin/claude-opus-5-5")) .. tostring(glob("devin/*", "devin/a/b")) .. tostring(glob("devin/**", "devin/a/b")) .. tostring(glob("Devin/*", "devin/x")) end`),
    [cand("a")],
  );
  assert.deepEqual(decisions, [{ keep: false, reason: "truefalsetruefalse" }]);
  await fails(`function select(m) return glob("!devin/*", m.id) end`, [cand("a")], "runtime", /leading !/);
  await fails(`function select(m) return glob(string.rep("a", 70000), m.id) end`, [cand("a")], "runtime", /too long/);
  await fails(`function select(m) return glob(1, m.id) end`, [cand("a")], "runtime", /expects two strings/);
});

// Reference model of the default script (design D3).
function expected(c: Candidate, extra: (id: string) => boolean): Decision {
  if (c.sources.cpa) {
    const listed = extra(c.id);
    const prefixed = c.mode === "prefix" ? c.has_prefix : c.id.includes("/");
    if (prefixed || listed) return { keep: true, source: "cpa", compile: c.mode === "forced" && listed };
  }
  if (c.sources.aisix) return { keep: true, source: "aisix", compile: false };
  if (c.sources.static) return { keep: true, source: "static", compile: false };
  return { keep: false, reason: "CPA ID without a CPA prefix" };
}

test("default script: 10,000 random candidates give valid returns matching D3; instruction use is under 1% of every bound", async (t) => {
  let seed = 42;
  const random = () => ((seed = (seed * 1103515245 + 12345) % 2 ** 31) / 2 ** 31);
  const pick = <T>(items: readonly T[]) => items[Math.floor(random() * items.length)]!;
  const candidates: Candidate[] = [];
  for (let i = 0; i < 10_000; i++) {
    const shape = pick(["p", "vendor", "devin", "bare"]);
    const id = shape === "bare" ? `m-${i}` : `${shape}/m-${i}`;
    const sources: Partial<Record<"cpa" | "aisix" | "static", true>> = {};
    if (random() < 0.7) sources.cpa = true;
    if (random() < 0.4) sources.aisix = true;
    if (random() < 0.1 || Object.keys(sources).length === 0) sources.static = true;
    candidates.push({ id, sources, mode: pick(["prefix", "forced", "fallback"]), has_prefix: shape === "p", ...(shape === "p" ? { prefix: "p" } : {}), native: random() < 0.5 });
  }
  const extra = (id: string) => id.startsWith("devin/") && !id.startsWith("devin/m-9");
  const run = await runMembershipScript(defaultScript(["devin/*", "!devin/m-9*"]), candidates, { hookStep: 1 });
  assert.equal(run.decisions.length, candidates.length);
  for (const [i, c] of candidates.entries()) {
    const want = expected(c, extra);
    const got = run.decisions[i]!;
    assert.deepEqual(got.keep ? { ...got } : got, want.keep ? want : { keep: false, reason: want.reason }, c.id);
  }
  // Production-sized collection: 554 CPA + 91 AISIX candidates (2026-10-28 record).
  const production = candidates.slice(0, 645);
  const sized = await runMembershipScript(defaultScript(["devin/*"]), production, { hookStep: 1 });
  const use = { load: sized.instructions.load, perCall: sized.instructions.maxCall, collection: sized.instructions.total };
  t.diagnostic(`default script instructions: ${JSON.stringify(use)} (bounds ${LIMITS.loadInstructions}/${LIMITS.callInstructions}/${LIMITS.totalInstructions})`);
  assert.ok(use.load < LIMITS.loadInstructions / 100, `load ${use.load}`);
  assert.ok(use.perCall < LIMITS.callInstructions / 100, `per call ${use.perCall}`);
  assert.ok(use.collection < LIMITS.totalInstructions / 100, `collection ${use.collection}`);
  assert.ok(DEFAULT_SCRIPT_TEXT.includes("local EXTRA_CPA = {}"));
});
