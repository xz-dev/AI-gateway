// Membership script worker (design D5–D7): one Lua 5.4 state (wasmoon) per authority collection
// or validate-config run. It loads the script once, calls `select` for every candidate under the
// sandbox bounds, posts the decisions and exits; its WebAssembly memory leaves with the thread.
import { parentPort, workerData } from "node:worker_threads";
import { LuaFactory } from "wasmoon";
import { compileModelPattern } from "./core.ts";
import type { Candidate, Decision, MemberSource, WorkerReply, WorkerRequest } from "./membership.ts";

const GLOB_CACHE = 256;

// Host prelude: captures what the wrappers need, removes host access (and Lua's own `select`, so a
// script that defines none is detected), and returns the host entry point. Script errors never pass through tostring (a __tostring metamethod would run
// script code outside the count hook).
const PRELUDE = `
local sethook, pcall, error, type, next, rawget, load, pack, sub = debug.sethook, pcall, error, type, next, rawget, load, table.pack, string.sub
local G = _G
local rawglob = __host_glob
local LOAD, CALL, TOTAL, STEP = __host_limits[1], __host_limits[2], __host_limits[3], __host_limits[4]
__host_glob, __host_limits = nil, nil
for _, name in next, { "io", "os", "package", "require", "load", "loadstring", "loadfile", "dofile", "debug", "coroutine", "collectgarbage", "print", "warn", "select" } do G[name] = nil end

function glob(pattern, id)
  if type(pattern) ~= "string" or type(id) ~= "string" then error("glob(pattern, id) expects two strings", 2) end
  local matched = rawglob(pattern, id)
  if matched == true or matched == false then return matched end
  error(matched, 2)
end

local total, exceeded, select_fn = 0, nil, nil
local function hooked(limit, label, fn, arg)
  local count = 0
  sethook(function()
    count = count + STEP
    total = total + STEP
    if count > limit then exceeded = exceeded or label; error("instruction limit exceeded", 0) end
    if total > TOTAL then exceeded = exceeded or "collection"; error("instruction limit exceeded", 0) end
  end, "", STEP)
  local result = pack(pcall(fn, arg))
  sethook()
  return result, count
end

local function message(value)
  if type(value) == "string" then return sub(value, 1, 300) end
  return "error value of type " .. type(value)
end

local function invalid(text) return { error = "invalid_return", message = text } end

local function load_script(text)
  local chunk, err = load(text, "=catalog.lua", "t", G)
  if not chunk then return { error = "load", message = message(err) } end
  local result, count = hooked(LOAD, "load", chunk)
  if exceeded then return { error = "instruction_limit", message = exceeded } end
  if not result[1] then return { error = "load", message = message(result[2]) } end
  select_fn = rawget(G, "select")
  if type(select_fn) ~= "function" then return { error = "missing_select", message = "catalog.lua does not define a global function select(m)" } end
  return { count = count }
end

local function decide(m)
  local result, count = hooked(CALL, "call", select_fn, m)
  if exceeded then return { error = "instruction_limit", message = exceeded } end
  if not result[1] then return { error = "runtime", message = message(result[2]) } end
  local keep, opts = result[2], result[3]
  if keep == true then
    if result.n > 3 then return invalid("select returned more than two values") end
    if opts == nil then return { keep = true, count = count } end
    if type(opts) ~= "table" then return invalid("the value after true must be a table") end
    local source, compile
    for key, value in next, opts do
      if key == "source" then
        if type(value) ~= "string" then return invalid("opts.source must be a string") end
        source = value
      elseif key == "compile" then
        if type(value) ~= "boolean" then return invalid("opts.compile must be a boolean") end
        compile = value
      else
        return invalid("unknown opts key of type " .. type(key))
      end
    end
    return { keep = true, source = source, compile = compile, count = count }
  elseif keep == false then
    if result.n > 3 then return invalid("select returned more than two values") end
    if opts == nil then return { keep = false, count = count } end
    if type(opts) ~= "string" then return invalid("the reason after false must be a string") end
    return { keep = false, reason = sub(opts, 1, 200), cut = #opts > 200, count = count }
  end
  return invalid("select must return true or false, got " .. type(keep))
end

return function(op, value)
  if op == "load" then return load_script(value) end
  local decision = decide(value)
  decision.total = total
  return decision
end
`;

const SOURCES: ReadonlySet<string> = new Set(["cpa", "aisix", "static"]);
const fail = (errorClass: string, message: string, candidate?: string): WorkerReply => ({ ok: false, errorClass, message, candidate });

function rawGlob(cache: Map<string, ((id: string) => boolean) | string>) {
  return (pattern: string, id: string): boolean | string => {
    let matcher = cache.get(pattern);
    if (matcher === undefined) {
      if (pattern.startsWith("!")) matcher = "glob() does not accept a leading !; negate the result in Lua";
      else {
        try {
          const compiled = compileModelPattern(pattern, "glob");
          matcher = (value: string) => compiled.match(value);
        } catch (error) {
          matcher = error instanceof Error ? error.message : "invalid glob";
        }
      }
      if (cache.size < GLOB_CACHE) cache.set(pattern, matcher);
    }
    return typeof matcher === "string" ? matcher : matcher(id);
  };
}

// The candidate as Lua sees it: absent fields stay absent (wasmoon cannot push null).
function luaCandidate(candidate: Candidate): Record<string, unknown> {
  const m: Record<string, unknown> = { id: candidate.id, sources: { ...candidate.sources }, mode: candidate.mode, has_prefix: candidate.has_prefix, native: candidate.native };
  if (candidate.prefix !== undefined) m.prefix = candidate.prefix;
  return m;
}

/** Trims a 200-byte cut that split a UTF-8 sequence (decoded as U+FFFD). */
const trimCut = (reason: string, cut: boolean) => (cut ? reason.replace(/\uFFFD+$/, "") : reason);

function boundText(label: string, limits: WorkerRequest["limits"]): string {
  if (label === "load") return `load instruction bound (${limits.loadInstructions}) exceeded`;
  if (label === "call") return `per-call instruction bound (${limits.callInstructions}) exceeded`;
  return `per-collection instruction bound (${limits.totalInstructions}) exceeded`;
}

function errorClassOf(message: string): string {
  return /not enough memory/.test(message) ? "memory_limit" : "runtime";
}

async function run(request: WorkerRequest): Promise<WorkerReply> {
  const lua = await new LuaFactory(request.wasmPath).createEngine({ injectObjects: false, enableProxy: false, traceAllocations: true });
  try {
    lua.global.setMemoryMax(request.limits.memoryBytes);
    lua.global.set("__host_glob", rawGlob(new Map()));
    lua.global.set("__host_limits", [request.limits.loadInstructions, request.limits.callInstructions, request.limits.totalInstructions, request.hookStep]);
    const host = lua.doStringSync(PRELUDE) as (op: string, value: unknown) => Record<string, unknown>;
    let loaded: Record<string, unknown>;
    try {
      loaded = host("load", request.script);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return fail(errorClassOf(message), message.slice(0, 300));
    }
    if (typeof loaded.error === "string") {
      const message = String(loaded.message);
      if (loaded.error === "instruction_limit") return fail("instruction_limit", boundText(message, request.limits));
      if (loaded.error !== "load") return fail(loaded.error, message);
      // Syntax errors quote the offending token (`near 'x'`); the log never carries script text.
      return errorClassOf(message) === "memory_limit" ? fail("memory_limit", message) : fail("load", message.replace(/ near .*$/s, ""));
    }
    const decisions: Decision[] = [];
    let maxCall = 0;
    let total = Number(loaded.count);
    for (const candidate of request.candidates) {
      let decision: Record<string, unknown>;
      try {
        decision = host("call", luaCandidate(candidate));
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        return fail(errorClassOf(message), message.slice(0, 300), candidate.id);
      }
      if (typeof decision.error === "string") {
        const message = String(decision.message);
        if (decision.error === "instruction_limit") return fail("instruction_limit", boundText(message, request.limits), candidate.id);
        return fail(decision.error === "runtime" ? errorClassOf(message) : decision.error, message, candidate.id);
      }
      maxCall = Math.max(maxCall, Number(decision.count));
      total = Number(decision.total);
      if (decision.keep === true) {
        const source = decision.source as string | undefined;
        if (source !== undefined && (!SOURCES.has(source) || candidate.sources[source as MemberSource] !== true)) {
          return fail("invalid_return", `opts.source ${JSON.stringify(source.slice(0, 20))} is not a source of this candidate`, candidate.id);
        }
        decisions.push({ keep: true, source: source as MemberSource | undefined, compile: decision.compile === true });
      } else {
        decisions.push({ keep: false, reason: decision.reason === undefined ? undefined : trimCut(String(decision.reason), decision.cut === true) });
      }
    }
    return { ok: true, decisions, instructions: { load: Number(loaded.count), maxCall, total } };
  } finally {
    lua.global.close();
  }
}

if (parentPort) parentPort.postMessage(await run(workerData as WorkerRequest));
