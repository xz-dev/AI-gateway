## Context

See proposal.md, "Why". The current code and the measurements it relies on (the full diagnosis is in `/var/tmp/ai-lua-walltime/report.md`):

**Current code**
- `runMembershipScript` (`src/membership.ts:116–140`) creates a `new Worker` for every run and arms a single `setTimeout(wallMs)` right after the constructor returns.
- The worker (`src/membership-worker.ts:142–197`) constructs a new `LuaFactory`, which compiles and instantiates `glue.wasm`. It then creates the engine, installs the trusted PRELUDE, loads `catalog.lua`, calls `select` for each candidate, closes the state and posts the result.
- Everything after the constructor is charged to the 2 s bound. Worker termination is requested, not awaited.

**Measured cold-path costs**
- Uncontended median phases: thread online plus bundled module load ≈ 20 ms; WASM compile 1.3 ms, instantiate 0.2 ms, Emscripten init 1.6 ms; engine 3.8 ms; PRELUDE setup 9 ms; script load 1.2 ms; 564 candidate calls with conversions ≈ 100 ms.
- Total is 130–159 ms uncontended.
- With 23 CPU burners sharing one CPU, init takes 0.75–0.91 s, execution 1.25–1.54 s, and the total exceeds 2 s in 5/5 runs.
- A scratch warm runtime (`warm.mjs`), with one factory kept alive and a fresh Lua state per round, measured:
  - warm rounds 27–52 ms uncontended, and 0.60–1.20 s with 23 burners;
  - harness RSS 75–88 MiB over 12 rounds. That is not a settled baseline.

**Environment**
- Production container: 0.5 CPU quota (`cpu.max 50000 100000`), 384 MiB memory, main heap 96 MiB, build worker 160 MiB, membership worker heap 32 MiB.
- Collections run single-flight through `AuthorityOwner.#active` (`src/authority.ts:365–397`). The scheduled refresh runs every 30 min, and forced refreshes are possible.
- The script file is read once at startup (`src/main.ts:132–134`), so in practice a digest changes only across a container recreate.

## Goals / Non-Goals

**Goals**
- Host-side work, thread start-up and runtime cold start no longer count toward the 2 s execution bound.
- Remove most of the per-collection cold cost by keeping one runtime warm.
- Keep every sandbox and safety property of the archived contract. Collections stay exactly as isolated as they are today.

**Non-Goals**
- Changing the container CPU quota or any memory cap. The user did not choose this.
- Raising the 2 s execution bound, the instruction bounds or the 16 MiB Lua memory bound.
- Guaranteeing success under arbitrary starvation. In the 31-burner measurement, execution alone sometimes exceeds 2 s, and the bound must still trip there.
- Hot reload of `catalog.lua`. The script is still read at startup; the digest guard only makes the worker lifecycle correct if that ever changes.

## Decisions

### D1. Two-phase protocol with a ready handshake

Per run:
1. The host makes sure a worker exists, spawning one if needed.
2. The host arms the **init timer (10 s)** and posts `{op: "init"}`.
3. The worker builds a fresh Lua state from its cached factory: engine, memory cap, `__host_glob`, limits and PRELUDE. It then replies `ready`. It has not seen the script text yet.
4. The host clears the init timer, arms the **execution timer (2 s)**, then posts `{op: "run", script, candidates}`.
5. The worker loads the script, runs every `select`, closes the state and replies with the result.
6. The host clears the execution timer.

The script travels only in the `run` message, so no operator code can execute before the execution timer is armed.

On the first run, the worker spawn, module load and WASM compile/instantiate also happen under the init timer.

**Alternatives considered**
- Start the execution timer when the worker first posts any message. This races with the worker starting script load on its own.
- Send the script with `init` and let the worker wait. Rejected: it widens what init can touch.

### D2. Init bound is 10 seconds

- The worst init measured is 1.61 s, at 31 burners. 10 s gives about 6× margin over the most extreme measured contention, and about 60× over the uncontended cold path.
- Init executes only trusted code: the module, the wasmoon runtime and the prelude. So this bound guards against a stalled runtime or a stalled thread start, not against operator code, and it does not need the tightness of the execution bound.
- 10 s is far below the 30 min refresh interval. It is also short enough that a stalled first generation still turns `/readyz` into a retry within seconds, not minutes.
- A longer value only delays failure detection. A value under about 3 s would reintroduce spurious failures under the measured contention.

### D3. Resident worker, fresh Lua state per run (chosen isolation)

The worker keeps one `LuaFactory`, which holds the compiled and instantiated WASM module and the warm JS bindings. It creates a new `LuaEngine` per run and closes it at the end of the run. The glob cache is created per run.

**Why fresh state, not resetting globals.** Resetting `_G` cannot reach everything a script can mutate:
- upvalues and closures captured by `select`;
- the shared string metatable (`getmetatable("").__index`);
- the library tables (`string`, `table`, `math`, `utf8`);
- the registry, plus the prelude's own `total` and `exceeded` counters.

Proving a reset complete would be a new security argument. A new state is the same guarantee as today, because each run still starts from a clean `lua_newstate` plus the same prelude.

What is reused is host code and the WASM module. Those are immutable from Lua, since no JS object is reachable from Lua.

The per-run cost is engine plus prelude plus script load, about 15 ms uncontended. That is the price of exact isolation, and it is much smaller than the cold path it removes (thread, module and factory).

**Alternatives considered**
- One Lua state kept across runs, with a global reset. Rejected: the isolation proof is incomplete.
- Caching a compiled `WebAssembly.Module` across short-lived workers. Rejected: it saves only about 1.5 ms, and wasmoon exposes no module hook without monkey-patching `WebAssembly`.

### D4. Lifecycle and failure handling

- The host owns one `MembershipWorker` record: `{worker, digest, busy}`. Runs are serialized. The authority is already single-flight, and a second caller waits for the first.
- On **any** failure the host clears both timers, calls `worker.terminate()`, **awaits** the exit, and drops the record. Failures here are any error reply, any timeout, an `error` event and an `exit` event. The next run spawns a new worker under the init bound.
- Awaiting the exit prevents two WASM runtimes from overlapping in memory.
- The host does the same when the requested script digest differs from the record's digest.
- An unexpected exit while the worker is idle only drops the record. It does not count as a failure, and nothing is logged as a script error.
- Error classes add `init_timeout`. The `wall_time` text is unchanged (`collection exceeded 2000 ms`), so existing alerting and log searches keep working.
- Server shutdown terminates the resident worker.

### D5. `validate-config` and the startup dry-run

Both call the same host API with `resident: false`. They use the same two phases and bounds, and the worker is terminated when the run finishes. That keeps the CLI process exit clean, and keeps the startup dry-run from leaving a worker that the first collection would need to recreate anyway.

The startup dry-run and the first collection may use the same digest. The resident worker is created lazily by the first collection.

### D6. Memory

The resident worker keeps one V8 isolate (heap capped at 32 MiB) and one wasmoon runtime alive. Each Lua state is closed after its run, and the 16 MiB Lua cap stays per state.

Acceptance:
- extend `tests/measure-memory.ts` to 50 or more forced refreshes, each overlapping a generation build;
- peak RSS below 384 MiB;
- settled RSS over the last 10 rounds shows no upward trend (slope ≤ 0 within noise, as the archived measurement defined it).

The archived baseline (lua-catalog-membership D7) had a peak of 216.5 MiB and settled RSS of 159.1–176.9 MiB over the last 10 rounds, with a cold worker per run. That design budgeted about 25 MiB of worker RSS for the time a collection runs. The expected change is that roughly this amount becomes resident between collections instead of transient, so settled RSS rises by about one isolate plus one runtime while the peak stays about the same. The measurement decides; the estimate does not. The record goes into this design's measurement note before the image is published.

**Measurement note (task 3.1/3.2, 2026-10-29, local).** A resident worker keeps V8 young-generation pages committed between runs; a per-run worker died before this could show. Membership worker alone, the real 564-candidate production-shaped script, 150+ runs:

| Variant | Steady RSS added by the membership worker | Exec time |
|---|---|---|
| no fix | about +100 MiB, plateau after ~200 runs (heap total ~89 MiB, used up to 48 MiB) | p50 47 ms |
| `gc()` in the worker after each reply only | +57 MiB, flat after ~75 runs; used heap stays 7 MiB, so not a leak | p50 47 ms |
| `gc()` plus `resourceLimits.maxYoungGenerationSizeMb: 8` (chosen) | about +9 MiB, flat from run 1; heap total 16 MiB; 600 runs 81.1→82.4 MiB process RSS | p50 47 ms, p90 53 ms (unchanged) |

The 8 MiB value is a new cap that **tightens** the young generation of the membership worker only. It raises none of the 96/160/32 MiB caps and the 384 MiB container limit is unchanged. `gc()` runs after the reply is posted, so it is outside the execution bound; the image runs node with `--expose-gc`, which workers inherit.

`tests/measure-memory.ts`, bundled workers, real tables, tracked `config.yaml`, each round overlapping a Codex build, `MALLOC_ARENA_MAX=2 node --max-old-space-size=96 --expose-gc`, same machine:

| Run | Peak RSS | Settled RSS | Last-10 slope | Threads |
|---|---|---|---|---|
| old code (per-run worker), 150 rounds | 246.8 MiB | 210–232 MiB (25-round means 210.6–231.6) | −0.1 MiB/round | per run |
| chosen variant, 150 rounds | 248.9 MiB | 235.3→246.1 MiB (25-round means), last 10 245.8–246.7 | 0.1 MiB/round | 1 |
| chosen variant, 60 rounds, `wall_time` injected every 10th round | 247.2 MiB | 218.9–240.6 MiB (25-round means), returns to range after each recreate | — | 7 (1 + 6 injected) |

The archived 216.5 MiB peak / 159.1–176.9 MiB settled figures came from a different machine state; on today's machine the old code measures 246.8 MiB peak, so the like-for-like delta is about +2 MiB peak and +15 MiB settled. The slow creep in the 150-round run (about 1 MiB per 25 rounds) is outside the membership worker, which stays flat over 600 runs alone. Every membership worker reports `resourceLimits` 32 MiB old / 8 MiB young.

## Risks / Trade-offs

- **Retained memory.** A resident isolate and runtime add a constant amount of RSS. → Enforced by the 50-round gate (D6) under the unchanged caps. If the gate fails, stop and report instead of raising a cap.
- **State leak through reuse.** → A fresh Lua state per run (D3), plus a test in which a script keeps a global counter and gets identical decisions on two runs.
- **Wasmoon runtime corruption after an in-state error.** For example, an allocation failure at the memory cap. → Every failure recreates the worker (D4), so a damaged runtime is never reused.
- **Execution can still exceed 2 s under extreme starvation.** → Intended. The bound stays, and the previous generation keeps serving. The goal is to stop charging cold-start cost to it.
- **Local CPU-quota reproduction.** Rootless `podman --cpus` did not enforce throttling locally (report §2). → The contention gate runs the report's harness: the production container profile (`--cpus 0.5 --memory 384m`), with the captured 564-candidate input, pinned to one CPU with `taskset` and with 23 burners. Today this reproduces the production error 5/5. The gate first shows it still fails with the current code, then requires 0/20 failures with the new code. The `taskset` contention is the binding load; the `--cpus 0.5` flag keeps the profile equal to production.
- **Init timer is wall-clock, like the execution timer.** A blocked main thread can fire it late. → Same property as today. Both are callbacks on the main thread, which stays responsive because Lua never runs there.

## Migration Plan

1. Implement and test locally (tasks 1–3). Publish `v1.4.0-<sha>` with the existing publish workflow, and pin it in `compose.yaml` and `.env.example`.
2. Production is image-only:
   - pin the private `.env`;
   - run `ops/ansible` `deploy.yml -e component=model-catalog --check --diff`. The diff must show only the image;
   - apply with the printed approval digest.
   - `deploy-config.yml -e component=model-catalog --check --diff` must report `changed=0`, because config and `catalog.lua` are unchanged.
3. Before/after: run `cmp` on the standard and Codex catalog bodies, internal and public. Watch for `wall_time` and `init_timeout` WARNs over several hours, which covers at least 6 scheduled refreshes plus forced refreshes.
4. Rollback: restore the previous `.env` pin and run `deploy.yml` again, or `rollback.yml -e component=model-catalog`. Then verify health and run `cmp` again.
