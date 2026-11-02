// Membership script sandbox, bounds, return validation and the default script (design D3, D5–D7),
// and the resident worker lifecycle (lua-membership-warm-worker D1–D5).
import assert from "node:assert/strict";
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { test } from "node:test";
import type { Worker } from "node:worker_threads";
import {
  LIMITS,
  MembershipScriptError,
  membershipWorkers,
  runMembershipScript,
  type Candidate,
  type Decision,
  type MembershipScript,
  type RunOptions,
} from "../src/membership.ts";
import { serve, sleep, tempDir } from "./helpers.ts";
import { DEFAULT_SCRIPT_TEXT, defaultScript, script } from "./scripts.ts";

const cand = (id: string, extra: Partial<Candidate> = {}): Candidate => ({ id, sources: { cpa: true }, mode: "prefix", has_prefix: false, native: false, ...extra });

async function fails(text: string | MembershipScript, candidates: Candidate[], errorClass: string, pattern?: RegExp, options?: RunOptions): Promise<MembershipScriptError> {
  try {
    await runMembershipScript(typeof text === "string" ? script(text) : text, candidates, options);
  } catch (error) {
    assert.ok(error instanceof MembershipScriptError, String(error));
    assert.equal(error.errorClass, errorClass, error.message);
    if (pattern) assert.match(error.message, pattern);
    return error;
  }
  assert.fail(`expected ${errorClass}`);
}

// The same failure on a warm worker: one successful run of the same script first, on the same
// thread; the failing run's thread has exited before its promise rejects.
const WARM = `\nlocal __cold = select\nfunction select(m) if m.id == "__warm" then return true end return __cold(m) end\n`;
async function warmFails(text: string, candidates: Candidate[], errorClass: string, pattern?: RegExp): Promise<MembershipScriptError> {
  const warm = script(text + WARM);
  const ok = await runMembershipScript(warm, [cand("__warm")]);
  const worker = membershipWorkers().resident;
  assert.ok(worker && worker.threadId === ok.threadId, "the successful run used the resident worker");
  const error = await fails(warm, candidates, errorClass, pattern);
  assert.equal(worker.threadId, -1, `${errorClass}: the warm worker exited before the rejection`);
  return error;
}

async function coldAndWarm(text: string, candidates: Candidate[], errorClass: string, pattern?: RegExp): Promise<MembershipScriptError> {
  await warmFails(text, candidates, errorClass, pattern);
  return fails(text, candidates, errorClass, pattern);
}

/** Waits for the run just started to spawn its resident worker. */
async function nextWorker(previous: Worker | undefined): Promise<{ worker: Worker; id: number }> {
  for (let i = 0; i < 2_000; i++) {
    const worker = membershipWorkers().resident;
    if (worker && worker !== previous) return { worker, id: worker.threadId };
    await sleep(1);
  }
  assert.fail("no new resident worker");
}

/** A test worker speaking the host protocol (design D1) without Lua; it logs every op it receives. */
async function fakeWorker(body: string): Promise<{ file: string; ops: () => Promise<string[]> }> {
  const dir = await tempDir();
  const file = join(dir, "worker.mjs");
  const log = join(dir, "ops.log");
  await writeFile(log, "");
  await writeFile(
    file,
    `import { parentPort } from "node:worker_threads";\nimport { appendFileSync } from "node:fs";\nconst log = (op) => appendFileSync(${JSON.stringify(log)}, op + "\\n");\nconst wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));\nconst decided = (m) => ({ ok: true, decisions: m.candidates.map(() => ({ keep: true, compile: false })), instructions: { load: 0, maxCall: 0, total: 0 } });\n${body}\n`,
  );
  return { file, ops: async () => (await readFile(log, "utf8")).split("\n").filter(Boolean) };
}

const NEVER_READY = `parentPort.on("message", (m) => log(m.op)); setInterval(() => {}, 1000);`;
const SLOW_C_CALL = `function select(m) if m.id == "slow" then return string.find(string.rep("a", 1 << 16), ".-.-.-.-.-.-.-b") ~= nil end return true end`;
const MEMORY_BOMB = "local t = {} function select(m) for i = 1, 64 do t[i] = string.rep('x', 1 << 20) .. i end return true end";

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
    await coldAndWarm(`function select(m) return ${name} end`, [cand("a")], "runtime", /attempt to (call|index) a nil value/);
  }
});

test("bounds: endless loops stop at their instruction bound; the collection total and memory are bounded", async () => {
  await fails("while true do end function select(m) return true end", [cand("a")], "instruction_limit", /load instruction bound/);
  const error = await coldAndWarm("function select(m) while true do end end", [cand("a"), cand("b")], "instruction_limit", /per-call instruction bound \(100000\)/);
  assert.equal(error.candidate, "a");
  // A pcall cannot swallow the bound: the attempt is still a script error.
  await coldAndWarm("function select(m) pcall(function() while true do end end) return true end", [cand("a")], "instruction_limit");
  const busy = "function select(m) for i = 1, 14000 do local x = i end return true end";
  const one = await runMembershipScript(script(busy), [cand("a")], { hookStep: 1 });
  assert.ok(one.instructions.maxCall >= 28_000 && one.instructions.maxCall <= 32_000, `one call uses ${one.instructions.maxCall} instructions`);
  const many = Array.from({ length: 1_000 }, (_, i) => cand(`m-${i}`));
  const total = await coldAndWarm(busy, many, "instruction_limit", /per-collection instruction bound \(20000000\)/);
  assert.ok(Number(total.candidate!.slice(2)) < 1_000, "the bound trips before the last candidate");
  await coldAndWarm(MEMORY_BOMB, [cand("a")], "memory_limit");
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
    await coldAndWarm(`function select(m) return ${value} end`, [cand("a")], "invalid_return");
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

// ---------------------------------------------------------------------------
// Resident worker lifecycle (lua-membership-warm-worker tasks 1.1–1.5).

test("init: a worker that never reports ready fails with init_timeout; no script is sent", async () => {
  const fake = await fakeWorker(NEVER_READY);
  const before = membershipWorkers().resident;
  const run = runMembershipScript(script("function select(m) return true end"), [cand("a")], { workerFile: fake.file, limits: { ...LIMITS, initMs: 200 } }).catch((error) => error);
  const { worker } = await nextWorker(before);
  const error = await run;
  assert.ok(error instanceof MembershipScriptError && error.errorClass === "init_timeout", String(error));
  assert.match(error.message, /initialization exceeded 200 ms/);
  assert.equal(worker.threadId, -1, "the worker exited before the promise settled");
  assert.deepEqual(await fake.ops(), ["init"], "no run message, so no script, was posted");
});

test("init: slow initialization does not use the execution budget (production 10 s / 2 s bounds)", async () => {
  const fake = await fakeWorker(`parentPort.on("message", async (m) => { log(m.op); await wait(m.op === "init" ? 1500 : 1000); parentPort.postMessage(m.op === "init" ? { ok: true, ready: true } : decided(m)); });`);
  const started = performance.now();
  const run = await runMembershipScript(script("function select(m) return true end"), [cand("a"), cand("b")], { workerFile: fake.file });
  assert.ok(performance.now() - started >= 2_400, "init plus execution took longer than the execution bound alone");
  assert.equal(run.decisions.length, 2);
  assert.deepEqual(await fake.ops(), ["init", "run"]);
});

test("warm: the execution bound applies to a reused worker; the next run starts a new worker", async () => {
  const limits = { ...LIMITS, wallMs: 500 };
  const slow = script(SLOW_C_CALL);
  const first = await runMembershipScript(slow, [cand("a")], { limits });
  const worker = membershipWorkers().resident!;
  assert.equal(worker.threadId, first.threadId);
  const second = await runMembershipScript(slow, [cand("a")], { limits });
  assert.equal(second.threadId, first.threadId, "the resident worker is reused");
  const started = performance.now();
  await fails(slow, [cand("slow")], "wall_time", /collection exceeded 500 ms$/, { limits });
  assert.ok(performance.now() - started < 500 + 1_500, "stopped near the bound");
  assert.equal(worker.threadId, -1, "the warm worker exited");
  const third = await runMembershipScript(slow, [cand("a")], { limits });
  assert.notEqual(third.threadId, first.threadId, "a new worker after the failure");
});

test("failures: every failure class terminates its worker before rejecting; the next run uses a new worker", async () => {
  const never = await fakeWorker(NEVER_READY);
  const exits = await fakeWorker(`parentPort.on("message", (m) => { log(m.op); if (m.op === "init") parentPort.postMessage({ ok: true, ready: true }); else process.exit(1); });`);
  const valid = script("function select(m) return true end");
  const cases: Array<[string, MembershipScript, RunOptions]> = [
    ["init_timeout", valid, { workerFile: never.file, limits: { ...LIMITS, initMs: 200 } }],
    ["wall_time", script(SLOW_C_CALL), { limits: { ...LIMITS, wallMs: 300 } }],
    ["instruction_limit", script("function select(m) while true do end end"), {}],
    ["memory_limit", script(MEMORY_BOMB), {}],
    ["runtime", script("function select(m) error('boom') end"), {}],
    ["load", script("function select(m) return true end x = ("), {}],
    ["invalid_return", script("function select(m) return 1 end"), {}],
    ["worker_exit", valid, { workerFile: exits.file }],
  ];
  for (const [errorClass, failing, options] of cases) {
    const before = membershipWorkers().resident;
    const run = runMembershipScript(failing, [cand(errorClass === "wall_time" ? "slow" : "a")], options).catch((error) => error);
    const { worker, id } = await nextWorker(before);
    const error = await run;
    assert.ok(error instanceof MembershipScriptError && error.errorClass === errorClass, `${errorClass}: ${String(error)}`);
    assert.equal(worker.threadId, -1, `${errorClass}: the failed worker exited before the rejection`);
    assert.equal(membershipWorkers().resident, undefined, `${errorClass}: the record is dropped`);
    const next = await runMembershipScript(valid, [cand("a")]);
    assert.notEqual(next.threadId, id, `${errorClass}: the next run uses a new worker`);
    assert.deepEqual(next.decisions, [{ keep: true, source: undefined, compile: false }]);
  }
  // An idle resident worker that exits on its own is replaced without a script error.
  const quits = await fakeWorker(`parentPort.on("message", (m) => { log(m.op); if (m.op === "init") parentPort.postMessage({ ok: true, ready: true }); else { parentPort.postMessage(decided(m)); setTimeout(() => process.exit(0), 50); } });`);
  const once = await runMembershipScript(valid, [cand("a")], { workerFile: quits.file });
  await sleep(300);
  assert.equal(membershipWorkers().resident, undefined, "the idle exit dropped the record");
  const again = await runMembershipScript(valid, [cand("a")], { workerFile: quits.file });
  assert.notEqual(again.threadId, once.threadId);
});

test("digest change: the old worker is terminated and the new script runs on a new worker", async () => {
  const a = script("function select(m) return true end -- A");
  const b = script('function select(m) return false, "b" end');
  const a1 = await runMembershipScript(a, [cand("x")]);
  const workerA = membershipWorkers().resident!;
  const a2 = await runMembershipScript(a, [cand("x")]);
  assert.equal(a2.threadId, a1.threadId, "the same script reuses its worker");
  const b1 = await runMembershipScript(b, [cand("x")]);
  assert.equal(workerA.threadId, -1, "the A worker exited");
  assert.notEqual(b1.threadId, a1.threadId);
  assert.deepEqual(b1.decisions, [{ keep: false, reason: "b" }]);
  const b2 = await runMembershipScript(b, [cand("x")]);
  assert.equal(b2.threadId, b1.threadId, "B reuses its worker");
});

test("isolation: no global, library field, string metatable value, upvalue or glob cache survives a run", async () => {
  const text = `
local INITIAL = table.concat({ tostring(rawget(_G, "counter")), tostring(string.leak), tostring(getmetatable("").leak) }, ",")
local captured = 0
local function bump() captured = captured + 1 return captured end
function select(m)
  counter = (counter or 0) + 1
  string.leak = true
  getmetatable("").leak = true
  bump()
  if counter == 1 then return true end
  local ok, err = pcall(glob, "!bad", m.id)
  return false, INITIAL .. "|" .. counter .. "," .. captured .. "|" .. tostring(ok) .. "|" .. tostring(err)
end`;
  const candidates = [cand("a"), cand("b"), cand("c")];
  const first = await runMembershipScript(script(text), candidates);
  const second = await runMembershipScript(script(text), candidates);
  assert.equal(typeof first.threadId, "number");
  assert.equal(second.threadId, first.threadId, "both runs used the same resident worker");
  assert.deepEqual(second.decisions, first.decisions);
  assert.deepEqual(first.decisions[0], { keep: true, source: undefined, compile: false });
  assert.match((first.decisions[1] as { reason: string }).reason, /^nil,nil,nil\|2,2\|false\|.*leading !/);
  assert.match((first.decisions[2] as { reason: string }).reason, /^nil,nil,nil\|3,3\|false\|.*leading !/);
});
