## Context

See proposal.md for the motivation. This change starts after `union-catalog-membership` has landed in production and is archived. Its code is the baseline:
- `authority.ts` `admitModels(rows, prefixes, cpaInclude, staticSlugs)` decides CPA admission during authority collection on the main thread (`AuthorityOwner.#collect`). The prefix mode is `prefix`, `forced` or `fallback`. `preserveNative` marks rows emitted as CPA returned them.
- `build.ts` `prepare()` adds every AISIX ID and every static model, deduplicated in CPA, AISIX, static order (source precedence), and sorts the members once into the catalog output order: bare IDs first, then the rest, each in code-point order. Codex `priority` is the 1-based output position, also on passthrough rows (requirement "Catalog output order and Codex priority"). It then applies `include_models`, builds the compile set and compiles in a short-lived worker (`runCompileWorker`, 160 MiB heap).
- `config.ts` holds the root `membership.cpa_include` list, compiled with `readGlobList`/`compileModelPattern`, the standard-glob matcher shared with openai-api-pi-extension.
- Memory discipline: main heap 96 MiB, build worker 160 MiB, container `mem_limit: 384m`, `MALLOC_ARENA_MAX=2`; measured build peak 256 MiB. The main thread holds no metadata tables.
- There is no metrics endpoint. Operators see state in logs and on `/models-table`.
- Production after union (2026-10-28 record): 357 catalog entries, Codex manifest 348, 47 passthrough rows, `force-model-prefix: false`. CPA lists 554 native IDs, 157 of them bare; AISIX lists 91. The private config has `membership.cpa_include: ["devin/*"]` and an `include_models` list of `"**"` plus 11 `!` hides (image models). No hide matches a static slug or an `inherit` target, and `metadata_from` is empty. The queued `prune-dangling-aisix-targets` change removes `xl/grok-imagine-image-2.0` from AISIX, which leaves its hide matching nothing.

## Goals / Non-Goals

**Goals:**
- Membership is a profile file, `catalog.lua`, that an operator can edit without an image release.
- The default script reproduces today's membership exactly, including passthrough and compatibility mode.
- A broken or hostile script cannot take the service down or read the host, and a script failure never replaces the served catalog. A script that deliberately rejects everything is honoured.
- Production moves to the script with a byte-identical catalog.

**Non-Goals:**
- No change to `include_models` semantics, to chain metadata, to post-processing or to routing.
- No Lua in openai-api-pi-extension, and no change there.
- The script cannot add IDs, rewrite IDs or change metadata.
- No new metrics endpoint. Failures are reported in the log and on `/models-table`.
- The gateway does not reject `catalog.lua` combined with `include_models`. Only the Ansible deploy check does (D11).

## Decisions

### D1 `catalog.lua` is part of the profile

- The tracked sample `services/routing/model-catalog/catalog.lua` is the default script. The Dockerfile copies it to `/app/catalog.lua`, like `config.yaml`. So the image default and the sample are one file.
- `CATALOG_SCRIPT_PATH` selects the script; unset means `/app/catalog.lua`. Compose sets it and mounts `./services/routing/model-catalog/catalog.lua:/app/catalog.lua:ro,Z`.
- The deploy root mirrors the repository tree, so without a private override the mounted file is the tracked default.
- A private override lives at `ops/ansible/private-config/services/routing/model-catalog/catalog.lua` and is deployed by D11.
- The file must be a regular file of at most 256 KiB. A missing file, a directory (what a bind mount of a missing source creates) or an oversized file fails startup and `validate-config`.

### D2 Interface: `select(m)` per candidate

Candidates are the unique IDs from the CPA native manifest (first occurrence), AISIX and the static models. The host calls `select(m)` once per candidate, in CPA, AISIX, static order. `m` is a fresh table:

| field | meaning |
|---|---|
| `id` | exact ID bytes |
| `sources` | `{cpa = true, aisix = true, static = true}`, only the sources that list it |
| `mode` | `"prefix"`, `"forced"` or `"fallback"` for this collection |
| `has_prefix` | starts with a known CPA prefix plus `/` at any `/` boundary (today's `hasPrefix`) |
| `prefix` | the shortest matched prefix, or `nil` |
| `native` | host passthrough flag: OAuth registration under an inferred prefix (`prefix` mode), any OAuth registration (`forced`), any CPA row (`fallback`); never for static slugs or non-CPA IDs |

Return values:
- `true`: keep; source = first of cpa, aisix, static that lists it; passthrough = `native`.
- `true, {source = "aisix"|"cpa"|"static", compile = true|false}`: both keys optional. `source` must be one of the ID's sources. `compile = true` turns passthrough off for a CPA member.
- `false, reason`: reject. The reason is cut at 200 bytes; missing → `rejected by catalog.lua`.
- Anything else is a script error.

The script only sees a copy of each candidate and returns a decision for it. Extra IDs it builds go nowhere, so it cannot invent members. The call order only fixes the default source; output order stays the host's fixed catalog order, and Codex `priority` stays the output position.

`glob(pattern, id)` calls `compileModelPattern` with exact-byte, case-sensitive matching. Compiled patterns are cached per collection (at most 256 entries, then compiled per call). There is no `!` in `glob`. Lists with negation are three lines of Lua (D3), so `glob` keeps one meaning.

The script's global `select` replaces Lua's built-in `select`. The docs say to use `table.pack`/`#` instead.

Why the options (user decision, 2026-10-28: keep both):
- `source`: without it, "prefer AISIX on same-name IDs" cannot be expressed.
- `compile`: in `forced` mode the union code compiles OAuth IDs matched by `cpa_include` (`devin/*`) and emits other OAuth IDs as-is. The host cannot tell these apart without the list, so the script must say it. Without it, a CPA rollback to `force-model-prefix: true` would change the catalog.

### D3 Default script

```lua
-- CPA IDs without a CPA prefix that are still admitted, as ordered standard globs.
-- The last matching entry decides; a leading "!" excludes. Example: { "devin/*" }.
local EXTRA_CPA = {}

local function listed(id, rules)
  local keep = false
  for _, rule in ipairs(rules) do
    if rule:sub(1, 1) == "!" then
      if glob(rule:sub(2), id) then keep = false end
    elseif glob(rule, id) then
      keep = true
    end
  end
  return keep
end

function select(m)
  if m.sources.cpa then
    local extra = listed(m.id, EXTRA_CPA)
    local prefixed = m.has_prefix
    if m.mode ~= "prefix" then prefixed = m.id:find("/", 1, true) ~= nil end
    if prefixed or extra then
      -- Forced prefixes: listed IDs are compiled, other OAuth IDs pass through as CPA returned them.
      return true, { source = "cpa", compile = m.mode == "forced" and extra }
    end
  end
  if m.sources.aisix then return true, { source = "aisix" } end
  if m.sources.static then return true, { source = "static" } end
  return false, "CPA ID without a CPA prefix"
end
```

This is `admitModels` plus the AISIX and static rules of `prepare()`, line for line. Parity is tested against the union parity fixture (task 1.5).

### D4 Lua is the membership stage

- `AuthorityOwner.#collect` reads CPA, prefixes and AISIX as today. It then builds the candidate list and runs the script (D7). `admitModels` and `membership.cpa_include` are deleted.
- The generation stores `members` (`{id, origin, index?, preserveNative}`, member order) and `rejected` (`{id, sources, reason}`) in place of `admitted`. `aisixIDs`, `prefixMode` and `prefixFallbackCount` stay.
- `prepare()` takes `members` from the generation, then runs `include_models`, the compile set, chain metadata and post-processing unchanged. `runtimeConflicts` reads CPA members.
- Lua runs only on authority collections: startup, the scheduled cadence (`scheduled_refresh_interval`, 30 min) and forced refresh. Catalog reads never run it.
- `membership` joins the legacy table with the hint `move cpa_include entries to EXTRA_CPA in catalog.lua`. The `bare_models_takeover` hint points there too.

### D5 Runtime: wasmoon 1.16.0

Checked on 2026-10-28:

| item | value |
|---|---|
| package | `wasmoon` 1.16.0, exact pin in `package.json` and `package-lock.json` |
| release | 2023-12-08, latest stable; `2.0.0-next.0` prerelease on 2026-04-25 is not used |
| license | MIT |
| Lua | 5.4 (`_VERSION` = `Lua 5.4`) |
| size | 313 KiB unpacked; `glue.wasm` 271,581 bytes; one dependency, `@types/emscripten` |
| maintenance | github.com/ceifa/wasmoon, not archived, 704 stars, 22 open issues, last push 2026-09-07, last default-branch commit 2025-01-18 ("fix memory leak after running code on new thread") |

Local probe on Node 26:
- import plus engine: about 23 ms;
- 1000 `select` calls: about 21 ms;
- RSS: +24.5 MiB.

Findings that shape D6:
- `setMemoryMax` throws unless the engine is created with `traceAllocations: true`.
- `functionTimeout` did not stop `while true do end`; the process hung until killed. A Lua count hook stopped it in 2 ms.

Alternatives:
- fengari (pure-JS Lua 5.3, unmaintained since 2020);
- a native Lua addon (needs a compiler in the slim image, and its crashes kill the process);
- a JS expression language (not Lua, which the user asked for).

wasmoon is maintained slowly, so its version is pinned and Dependabot bumps it like other dependencies. The sandbox does not rely on wasmoon internals beyond `LuaFactory`, `doString`, `global.get/set`, `setMemoryMax` and `close`.

### D6 Sandbox and limits

A host prelude runs first in each Lua state:
- It keeps `debug.sethook` in a local.
- It sets these globals to `nil`: `io`, `os`, `package`, `require`, `load`, `loadstring`, `loadfile`, `dofile`, `debug`, `coroutine`, `collectgarbage` and `print`.
- It returns a wrapper function to the host. The wrapper installs a count hook (every 1,000 instructions), calls the target under `pcall`, then removes the hook.

What remains: `string`, `table`, `math`, `utf8`, `pairs`/`ipairs`/`next`/`type`/`tostring`/`tonumber`/`error`/`pcall`/`assert`/`setmetatable`/`getmetatable`/`rawget`/`rawset`/`rawequal`/`rawlen`, and `glob`. Lua's own vararg `select` is removed too, so a script that defines no `select(m)` fails with `missing_select` instead of calling the builtin.

- The engine uses `injectObjects: false` and `enableProxy: false`, so Lua sees only copied tables, never JS objects.
- `coroutine` is removed because Lua hooks are per-coroutine: a coroutine would run without the count hook.

| bound | value | enforced by |
|---|---|---|
| load (top-level chunk) | 1,000,000 instructions | count hook |
| one `select` call | 100,000 instructions | count hook |
| one collection | 20,000,000 instructions | count hook (running total) |
| Lua memory | 16 MiB | `setMemoryMax` |
| wall time per collection | 2 s | `worker.terminate()` from the main thread |
| script size | 256 KiB | host read |
| worker V8 heap | 32 MiB | `resourceLimits.maxOldGenerationSizeMb` |

The count hook cannot interrupt one long C call, such as a pathological `string.find` on a large string. The wall-time bound covers that case. It is the reason the script cannot run on the main thread (D7).

The per-call budget is about 100 times what the default script uses for one candidate. 20,000,000 per collection allows around 200 calls at the full per-call budget, and production has about 470 candidates using a few hundred instructions each. Task 1.3 measured the default script with an exact per-instruction hook on a production-sized collection (554 CPA + 91 AISIX = 645 candidates): load 15 instructions, at most 71 per call, 34,421 per collection. That is under 0.1% of the load bound, 0.07% of the per-call bound and 0.17% of the collection bound. The hook runs every 1,000 instructions in production, so a bound trips at most 1,000 instructions late.

### D7 Thread placement and memory

- **Where Lua runs**: one short-lived worker thread per authority collection (and per `validate-config`). It receives the script text and the candidate array (about 500 small objects, tens of KiB), creates one `LuaFactory` and one engine, loads the script once, calls `select` per candidate, posts the decisions, closes the engine and exits.
- **Why not the main thread**: a long C call would block `/v1/models` streaming, and wasmoon's linear memory only grows. When it lives in the worker, it is freed when the worker exits.
- **Why not the build worker**: membership is decided at collection time and feeds `runtimeConflicts` and the generation. Builds run later and per client version. Moving membership into builds would change the generation model.
- **Why one factory per worker**: in 1.16.0 all engines of a `LuaFactory` share one WASM instance and its memory, so a long-lived factory keeps its high-water mark. One factory per worker costs about 20 ms per collection, at most once per 30 minutes plus forced refreshes.
- **Memory budget**: about 25 MiB worker RSS (isolate plus up to 16 MiB Lua memory plus the WASM module). This sits above the 256 MiB measured build peak when a collection overlaps a build: about 281 MiB, under 384 MiB. The main 96 MiB and worker 160 MiB heaps are unchanged.
- WASM linear memory is one `ArrayBuffer` mapping, not malloc arenas, so `MALLOC_ARENA_MAX=2` is unaffected.
- No per-call host allocation beyond the small `m` table and the decision. Decisions are collected into one preallocated array.
- Task 1.6 measured with `tests/measure-memory.ts` (bundled workers, real metadata tables, the tracked `config.yaml` chains, `MALLOC_ARENA_MAX=2 node --max-old-space-size=96 --expose-gc`): 50 forced refreshes on the production-shaped fixture (390 members), each overlapping a Codex build with the next collection (median overlap 98 ms). Peak process RSS 216.5 MiB; settled RSS after the first round 178.7 MiB and over the last 10 rounds 159.1–176.9 MiB, slope −1.4 MiB per round (no upward trend). The image grows by 0.83 MiB (259,051,104 → 259,924,557 bytes).
- `glue.wasm` ships next to `dist/` (bundle step copies it; `LuaFactory` gets its file URL) so the read-only container needs no runtime fetch.

### D8 Failure fallback

Each of these fails the collection with a `MembershipScriptError`:
- a load error, a runtime error, an invalid return or an exceeded bound;
- a worker exit, a wall-time termination.

An empty result is not a failure (user decision, 2026-10-28). When every `select` call returns a valid `false, reason`, the collection succeeds:
- the empty membership is published as is, and the gateway does not block it;
- an INFO line reports the candidate and rejected counts;
- the models table lists every candidate as hidden by `catalog.lua` with its reason.

In that case:
- `AuthorityOwner` does not publish. The previous generation keeps serving, including its `/v1/models` files.
- A WARN is logged with the error class, the candidate ID when known, and the bound when one was hit. The script text is not logged.
- The models table records the failed attempt with its error class, like a worker crash today.
- Before the first generation, `/readyz` stays not ready and the bounded retry applies.

CPA unavailability keeps its existing path (502 `native_manifest_failed`). A script failure never turns into an empty or CPA-only catalog; only a successful run can produce an empty one.

Startup loads and dry-runs the script (D9) and exits non-zero before listening on failure. So a broken script is caught at deploy time, not at the first collection.

### D9 `validate-config --script`

- `validate-config <config|-> [--script <path|->]`, at most one `-`.
- It parses the config as today. With `--script`, it then runs the worker with a fixed synthetic candidate set: CPA-only, AISIX-only, static-only and every pair of sources, each in `prefix`, `forced` and `fallback` modes, with and without a prefix, plus the config's static slugs. Every return must be valid. Rejecting all synthetic candidates is allowed: it exits 0 and writes a WARN with the rejected count to stderr, because real data differs and an empty catalog is a legitimate operator choice.
- Output: one JSON line `{"version":1,"config_digest":…,"script_digest":…}` (`script_digest` only with `--script`). Existing callers without `--script` see the same output.
- Ansible validates a candidate config with `validate-config -` and a candidate script with `validate-config /app/config.yaml --script -` (D11). The dry-run does not depend on the config beyond static slugs, so validating each candidate against the live other file is enough.

### D10 Models table

- Rejected candidates become hidden rows with `hidden by catalog.lua: <reason>` and no metadata. Like every other row they follow the catalog output order.
- The summary adds a `rejected` count next to kept, hidden and referenced-only.
- `project.ts` keeps the `include_models[i]` text for include hides.
- With the default script in production, the table gains a few hundred rejected rows, mostly CPA bare copies of prefixed IDs. Rows are small: ID, sources and reason.

### D11 Ansible: second managed file and combination check

- `ai_ops_model_catalog` handles a list of managed files: `model-catalog/config.yaml` always, and `model-catalog/catalog.lua` when the private file exists.
- **Custom script**: `{{ ai_ops_config_dir }}/services/routing/model-catalog/catalog.lua` exists on the controller. Content does not matter, so the rule is simple to test and to explain.
- The operation runs these steps in order:
  1. **Combination check**: slurp the private `config.yaml`. If a custom script exists and `metadata.include_models` is set, fail. This runs first, also under `--check`.
  2. **Prepare**: drift gate and baseline for each file (`ai_ops_managed_file` `prepare`).
  3. **Validate**: each changed candidate with the deployed binary (D9).
  4. **Deploy**: deploy the files with activation only on the last changed file, so `model-catalog` is recreated once.
  5. **Verify** (`verify.yml`): checksum of both remote files, `validate-config /app/config.yaml --script /app/catalog.lua` in the new container, readiness and neighbours.
  6. **On failure**: restore every file deployed in this run from its baseline, then recreate once.
- Without a custom script the remote `catalog.lua` is not managed, and `include_models` stays allowed. Removing a private override does not delete the remote file: the operator restores the default by copying the tracked sample into the private directory, deploying, and then deleting it.
- There is no staging mode. A change that couples the image, the Compose definition and the profile (such as this change's cutover) goes through a one-time reviewed manual window, as in earlier migrations (layout move, CPA plugins). After that window, the managed-file operation adopts the deployed files as baselines.
- Tests extend `ops/scripts/tests/test-ansible-model-catalog-config.sh` with the docker shim:
  - the combination check, also under `--check`;
  - a script-only change;
  - both files changed;
  - restore of both on a failed verify.

### D12 Docs

- `services/routing/model-catalog/README.md`: one line saying membership is the profile's `catalog.lua`, with a link.
- `docs/model-catalog.md` gets a "Membership script" section:
  - the `m` fields and return values, `glob`, the sandbox and limits, and failure behavior;
  - `validate-config --script`;
  - examples: admit all; exclude a few (`listed(m.id, HIDE)` → `false, "excluded"`); prefer AISIX on same-name IDs (`if m.sources.aisix then return true, {source = "aisix"} end` first); the extra CPA list.
- `docs/ansible-operations.md` and `ops/ansible/README.md` list the second file, the custom rule, the combination check, and that coupled image/Compose/profile changes use a reviewed manual window.

## Risks / Trade-offs

- [Parity drift between the TS rule and the default script] → task 1.1 records the union code's output on the union parity fixture as a golden file before the code is removed. The Lua path must match it byte for byte.
- [wasmoon slow maintenance or a WASM bug] → exact pin, minimal API surface, all limits enforced by our host (count hook, `setMemoryMax`, worker termination), not by `functionTimeout`. A runtime crash only kills the worker (D8).
- [Script freezes CPA updates] → a script that fails on new real data blocks new generations until fixed. The WARN and the table make it visible. The default script cannot fail on valid inputs (task 1.3 fuzzes it).
- [Extra memory during collection] → about 25 MiB for at most 2 s per collection. It is measured in task 1.6 with an overlapping build.
- [Bind-mount of a missing file creates a directory] → D1 fails closed on a directory. Production always deploys the file (D11). `validate.sh` checks the tracked sample exists.
- [Coupled cutover: the old image does not read `catalog.lua`, and the new image rejects `membership.cpa_include`] → the profile files, the Compose file and the image switch happen in one manual window with one recreate, so neither image ever runs with the other's profile. Rollback restores all three together.
- [An operator script that rejects everything empties the catalog] → this is allowed by design. The INFO line, the models table and the `validate-config` WARN make it visible before and after deploy.
- [Hidden-by-script models cannot be `inherit`/`metadata_from` targets] → include hides can still be referenced; script rejects cannot. Production has no such reference (checked in Context; task 4.1 re-checks with the real matcher). The docs say to keep such hides in `include_models`, which then needs the default script.

## Migration Plan

All production steps run on an `openai/*` model with the playbooks current at apply time. `--check --diff` runs first for every playbook.

1. **Repository**: implement D1–D10 with tests. Publish the image and pin its digest. Implement D11 with tests. Write the docs. CI green.
2. **Private profile** (local): write the private `catalog.lua` as the default script with `EXTRA_CPA = { "devin/*" }` and a `HIDE` list holding the `include_models` hide patterns that still match a live candidate at migration time (a hide that matches nothing is dropped and listed in the record). Rejections use the reason `hidden by profile`. Delete `membership` and `metadata.include_models` from the private `config.yaml`. Check with the new image locally: `validate-config` and a dry-run.
3. **Pre-capture** (read-only, `/var/tmp`): native manifest IDs, AISIX IDs, the standard list bytes, Codex manifest bytes for the pinned client versions, member and passthrough sets, public `/v1/models` count for a test key, the Sub2API count, `:9083/models-table` summary.
4. **Manual window** (reviewed, one recreate). The Compose change (mount and `CATALOG_SCRIPT_PATH`) is a structure change that the component gate rejects, and the old and new images need different profiles, so this cutover does not use the deploy playbooks:
   1. Before the window: back up the host `compose.yaml`, `.env`, `config.yaml` and any `catalog.lua`; pull the new image by digest; validate the private pair with a throwaway container of the new image (`--rm --network none --read-only`, `validate-config /candidate/config.yaml --script /candidate/catalog.lua`).
   2. Pause the recovery timer.
   3. Upload the private `config.yaml`, `catalog.lua` and `compose.yaml` (diff against the host = only this change's lines) and the `.env` image pin; `docker compose config -q`.
   4. `docker compose up -d --no-deps --no-build model-catalog`: one recreate.
   5. `POST :9083/models-table/refresh`; restart the recovery timer.
5. **Post-capture and diff**: the same captures as step 3. The JSON catalogs must be byte-identical, the member and passthrough sets equal, and the counts equal. The table may differ only by script-rejected rows and the hide reason text.
6. **Adopt baselines**: `deploy-config.yml -e component=model-catalog --check --diff` and `deploy.yml -e component=model-catalog --check --diff` report `changed=0`; any stale baseline is re-adopted per the documented first-adoption path.
7. **Record**: `docs/records/lua-catalog-membership-deployment-<date>.md` with digests, timings, the diff summary and rollback commands.
8. **Rollback**: in the same manual way, restore the backed-up `compose.yaml`, `.env` and `config.yaml` (with `cpa_include` and `include_models`), then one `up -d --no-deps --no-build model-catalog`. A remaining `catalog.lua` is ignored by the old image.

## Resolved decisions (user, 2026-10-28)

- Keep both optional keep options, `source` and `compile` (D2).
- Custom means a private `catalog.lua` exists, whatever its content (D11).
- The production cutover uses a one-time reviewed manual window, with no Ansible staging mode (D11, Migration Plan step 4).
- An empty result from a successful run is published, not blocked (D8). Only script failures keep the previous generation.
- The limit values stay fixed in code (D6). Task 1.3 checks the default script uses under 1% of each.
