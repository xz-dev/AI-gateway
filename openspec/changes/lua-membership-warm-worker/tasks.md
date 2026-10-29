## 1. Tests first (red against the current code)

All tests live under `services/routing/model-catalog/tests/`. Each one is written before section 2, and must fail or be unrunnable against the current per-collection worker. Record the red run in this task list. Tests may lower `LIMITS` through `RunOptions.limits`, as the existing bound tests do. They may also use a test-only worker file through `RunOptions.workerFile`.

- [x] 1.1 Init timeout, in `membership.test.ts`. A test worker that never posts `ready`, run with `initMs: 200`, rejects with class `init_timeout`. The worker thread has exited when the promise settles, and no `run` message was posted, so no script was sent. A second test worker sleeps 1.5 s before `ready`, then runs a script that needs about 1 s. With the production 10 s / 2 s limits it succeeds, because the execution clock started at `ready` (spec scenario "Slow initialization does not use the execution budget"). Verify: both tests are red now and green after section 2.
  - Done 2026-10-29: red run (old host plus a shim exporting `membershipWorkers` so the file loads) fails both tests, `/var/tmp/ai-gw-warm-evidence/1-red.log` (11 of 21 tests in the two files red, every pre-existing test green); green after section 2, `2-green-membership.log`. The slow-init case uses a protocol test worker (1.5 s init, 1 s run) so the timing is deterministic.
- [x] 1.2 Execution timeout on a warm worker, in `membership.test.ts`. Do one successful run, then a second run on the same resident worker (assert the same thread ID) with a `select` that runs one long `string.find` past `wallMs`. The second run rejects with class `wall_time` and the message `collection exceeded <wallMs> ms`, and that worker has exited. A third run succeeds on a new thread ID. The existing endless-loop, memory-bomb, sandbox-access and invalid-return tests run both on a cold worker and on a warm one (a successful run first). Verify: the warm cases are red now and green after section 2.
  - Done 2026-10-29: `warm:` test asserts the same thread ID on two runs, `wall_time` with `collection exceeded 500 ms` (`wallMs` lowered through `RunOptions.limits`), the warm thread ID -1 afterwards and a new thread ID on the third run. Endless loop, pcall loop, collection total, memory bomb, sandbox access and every invalid return run cold and warm (`coldAndWarm`). Red/green as in 1.1.
- [x] 1.3 Worker recreated after failure, in `membership.test.ts`. Use one case for each failure class: `init_timeout`, `wall_time`, `instruction_limit`, `memory_limit`, `runtime`, `load`, `invalid_return`, and `worker_exit` (the test worker calls `process.exit(1)`). The failing run's thread has exited before its promise rejects, and the next valid run succeeds on a new thread ID. An idle resident worker that exits on its own is replaced on the next run without a script error. Verify: red now, green after section 2.
  - Done 2026-10-29: one table-driven test covers all eight classes; each failing worker has `threadId` -1 before its promise rejects, the record is dropped, and the next valid run gets a new thread ID. A protocol worker that exits 50 ms after its reply is replaced on the next run without a script error. Red/green as in 1.1.
- [x] 1.4 Digest-change reload, in `membership.test.ts`. Run script A twice; the second run uses the same thread. Then run script B, which has a different digest and decides differently. The A worker has exited, B runs on a new thread, and B's decisions are returned. Then run B again; it reuses B's thread. Verify: red now, green after section 2.
  - Done 2026-10-29: `digest change:` test; A reuses its thread, B runs on a new thread with its own decisions after A's thread exited, B reuses its thread. Red/green as in 1.1.
- [x] 1.5 No state leak between collections, in `membership.test.ts`. A script increments a global counter and keeps the candidate only while the counter is 1. It also writes a field into `string`, sets a metatable value on `""`, and stores a closure upvalue. Two runs on the same resident worker return identical decisions. A second script reads all four values, and sees each as absent or at its initial value. The `glob` cache is per run: a pattern that was invalid in run 1 is reported again in run 2, the same as on a cold worker. Verify: green now (cold worker) and still green after section 2. It must also still pass when the resident path is forced, by asserting the thread ID is unchanged.
  - Done 2026-10-29: one script reads, at load, the global counter, a `string` field and a string-metatable field, and in `select` reports the counter, a closure upvalue and an invalid `glob` through `pcall`. Both runs on the same thread ID return identical decisions with `nil,nil,nil` initial values and per-run counts 2 and 3. Red only because the thread-ID assertion needs the resident path (old code has no `threadId`).
- [x] 1.6 Collection-level behavior, in `membership-stage.test.ts`:
  - an `init_timeout` failure keeps the previous generation serving, logs one WARN with class `init_timeout`, marks the table attempt failed, and the next collection retries and publishes;
  - the existing `wall_time`, hang, memory, invalid-return and fallback-to-previous-generation stage tests pass unchanged;
  - a forced refresh after a success reuses the resident worker;
  - `validate-config --script` and the startup dry-run leave no worker thread alive after they return.

  Verify: red now where the behavior is new; the existing tests stay green.
  - Done 2026-10-29: `membership-stage.test.ts` adds the `init_timeout` stage test (previous generation and standard bytes unchanged, one WARN `"class":"init_timeout"`, table attempt failed, next collection publishes), the forced-refresh reuse test, and the dry-run plus in-process `validateConfig` test (live worker count and resident record unchanged). Existing stage tests unchanged and green. `tests/catalog.test.ts` "workers have exited after a generation build" now expects the membership worker to stay resident and the compile worker to exit (warm-worker D3).
- [x] 1.7 CPU-contention reproduction, using the report's harness. Script it under `services/routing/model-catalog/tests/`, or as a scratch script under `/var/tmp` that is referenced here. Setup:
  - a local container with the production profile, `--cpus 0.5 --memory 384m`;
  - the bundled image code;
  - the captured 564-candidate input and the production-shaped script from `/var/tmp/ai-lua-walltime/`;
  - pinned to one CPU with `taskset`, with 23 CPU burners that always end in `finally`.

  Run 20 consecutive collections through the real host API on a resident worker, with a cold first run. Verify:
  - against the current code (the deployed bundle), the same harness reproduces `membership script wall_time: collection exceeded 2000 ms`, which proves the load is in effect;
  - after section 2, 0/20 runs fail;
  - every run accepts the same 344 candidates;
  - the per-run init and exec times are recorded here.

  - Done 2026-10-29: harness `/var/tmp/ai-gw-warm-contention/run.py` (plus `new-entry.ts`/`old-entry.ts` bundled with the image's `membership-worker.mjs` and `glue.wasm`; the captured private input stays in `/var/tmp` mode 0600). `podman run --cpuset-cpus 0 --cpus 0.5 --memory 384m --network none --read-only`, launched under `taskset -c 0`, 23 `taskset -c 0` burners stopped in `finally`, 20 collections through `runMembershipScript`. Old code (v1.3.0 image and its deployed worker bundle): 9/20 `membership script wall_time: collection exceeded 2000 ms`. New code: 0/20 in three separate runs (`new-b23-r20-a.jsonl`, `-b`, `-final`), 344 kept every run, one thread ID. Final run init ms: 1047.2 (cold), 149.1, 1, 3.4, 0.9, 0.8, 4.7, 1, 1.1, 1, 1.3, 1.3, 145.1, 0.7, 0.8, 144.5, 0.8, 0.8, 0.9, 0.8; exec ms: 1532.3 (cold), 907.8, 1353.4, 614.2, 607.1, 603.8, 602, 604, 888.3, 602.9, 748.4, 749.8, 600.4, 601.1, 600.9, 751.8, 601.2, 597.9, 601.7, 601.2. The cold first run is the closest to the bound (exec 1.2–1.8 s across the three runs).
## 2. Implementation

- [x] 2.1 Worker protocol, in `src/membership-worker.ts` (design D1, D3). The worker keeps one `LuaFactory` for its lifetime and takes `{op: "init"}` and `{op: "run", script, candidates}` messages from `parentPort`.
  - On `init`, it creates a new engine with `setMemoryMax(16 MiB)`, a new glob cache, the limits and PRELUDE, then posts `ready`. It never sees the script.
  - On `run`, it loads the script, calls `select` for every candidate, closes the engine in `finally` and posts the reply.
  - A non-resident run exits after replying.

  Verify: 1.1, 1.2 and 1.5 are green.
  - Done 2026-10-29: one `LuaFactory` per thread, `init` builds a new engine (16 MiB cap, new glob cache, limits, PRELUDE) and replies `{ok: true, ready: true}`; `run` loads the script, decides, closes the engine in `finally`. Non-resident runs are terminated by the host after the reply (same effect, one code path). The worker calls `gc()` after each `run` reply (design D6 measurement note).
- [x] 2.2 Host lifecycle, in `src/membership.ts` (design D1, D4, D5).
  - Add `LIMITS.initMs = 10_000`.
  - Add one resident worker record `{worker, digest}` per process, with serialized runs.
  - Arm the init timer before `init` is posted. Arm the execution timer only on `ready`, before `run` is posted.
  - On any error reply, timeout, `error` event or `exit` event: clear the timers, `await worker.terminate()`, then drop the record.
  - Recreate the worker when the digest differs. An exit while idle only drops the record.
  - Add `resident: false` for `dryRun` and `validate-config`, and a shutdown hook from `src/main.ts`.

  Keep `runMembershipScript`'s signature and error classes, and add `init_timeout`. Verify: 1.1–1.6 are green.
  - Done 2026-10-29: `LIMITS.initMs = 10_000`; one resident record keyed by script digest, worker file and heap caps; runs serialized through a promise queue; init timer armed before `init` is posted, execution timer on `ready` before `run` is posted; any error reply, timeout, `error` or `exit` clears the timer, awaits `terminate()` and drops the record; an idle exit only drops the record; `resident: false` for `dryRun`; `closeMembershipWorker()` on server shutdown. Also `LIMITS.workerYoungMb = 8` (`maxYoungGenerationSizeMb`), the approved tightening from the D6 measurement. Error classes unchanged plus `init_timeout`.
- [x] 2.3 Call sites. `src/authority.ts` is unchanged apart from passing the digest, if it is not already in `MembershipScript`. In `src/main.ts`, the dry-run and `validate-config` use `resident: false`, and server shutdown closes the resident worker. The compile-cache warm-up still loads the membership worker bundle. Verify: the `main` tests and `validate-config` output tests are byte-identical to before.
  - Done 2026-10-29: `src/authority.ts` unchanged (the digest is already in `MembershipScript`); `dryRun` passes `resident: false`, so the startup dry-run, `validate-config` and `warm-compile-cache` use a run-scoped worker; `serve()` calls `closeMembershipWorker()` after `service.stop()`. `server.test.ts` (validate-config output) and `build-artifacts.test.ts` (bundles, warm-compile-cache, validate-config) pass unchanged.
- [x] 2.4 Docs. `docs/model-catalog.md`, the "Sandbox and limits" and "Failure behavior" paragraphs:
  - a resident worker with a fresh Lua state per collection;
  - the 10 s init bound, separate from the 2 s execution bound;
  - recreation after any failure or a digest change;
  - the `init_timeout` class.

  The "Runtime" paragraph names the resident membership worker. Verify: the link check finds 0 broken links, and no doc still says "short-lived worker per collection" for the membership worker.
  - Done 2026-10-29: `docs/model-catalog.md` Sandbox and limits, Failure behavior and Runtime updated (resident worker, fresh state, 10 s init / 2 s execution, `init_timeout`, recreation, 32 MiB old / 8 MiB young). Relative-link check over the changed docs and this change: 7 links, 0 broken. `grep` finds no "short-lived worker per collection" outside `docs/records`.
- [x] 2.5 Full suite and image. Verify:
  - `npm ci && npm run typecheck && npm test && npm run build` pass in `services/routing/model-catalog`;
  - `podman build` succeeds;
  - in the image, `/model-catalog validate-config /app/config.yaml --script /app/catalog.lua` exits 0 with `--read-only --network none`, and its output is byte-identical to the v1.3.0 image's;
  - 1.7 passes, with 0/20 failures, on the built image.

  - Done 2026-10-29: `npm ci`, `npm run typecheck`, `npm test` (120/120, four consecutive full runs green after the last code change; the first full run of section 2 had a one-off `golden` miss of the AISIX members, which did not reproduce in 6 later full runs or in isolation), `npm run build`; `podman build` succeeds; in-image `validate-config /app/config.yaml --script /app/catalog.lua` with `--read-only --network none` exits 0 and its output is `cmp`-identical to the v1.3.0 image's; 1.7 passes on the built image.
## 3. Memory measurement

- [x] 3.1 Extend `tests/measure-memory.ts` (design D6). Use the bundled workers, the real metadata tables, and the tracked `config.yaml` chains, with `MALLOC_ARENA_MAX=2 node --max-old-space-size=96 --expose-gc`. Run at least 50 forced refreshes on the production-shaped fixture, each overlapping a Codex build, on one resident membership worker. Assert the thread ID stays the same, apart from deliberately injected failures. Verify:
  - peak process RSS is below 384 MiB;
  - settled RSS over the last 10 rounds shows no upward trend;
  - the membership worker's `resourceLimits` still reports 32 MiB.

  Record peak, settled range, slope and the delta against the archived 216.5 MiB peak and 159.1–176.9 MiB settled range in design.md under D6. If a cap would have to change, stop and report instead.
  - Done 2026-10-29: `tests/measure-memory.ts` takes `ROUNDS` and asserts one membership thread; 150 rounds: peak 248.9 MiB, last 10 settled 245.8–246.7 MiB, last-10 slope 0.1 MiB/round, `resourceLimits` 32 MiB old / 8 MiB young. Old code on the same machine: peak 246.8 MiB, settled 210–232 MiB. Numbers and the variant comparison are in design.md D6 "Measurement note" (`/var/tmp/ai-gw-warm-evidence/7-memory-a-inject0-r150.log`, `6-memory-baseline-old-150.log`).
- [x] 3.2 Repeat 3.1 with one injected `wall_time` failure every 10 rounds. Verify: RSS returns to the same settled range after each recreate, with no growth, and a new thread ID is seen after each injected failure.

  - Done 2026-10-29: `INJECT=1`, 60 rounds: 6 injected `wall_time` failures (`wallMs: 1`), 7 thread IDs (1 + 6), peak 247.2 MiB, 25-round settled means 235.6 / 240.6 / 218.9 MiB, no growth (`7-memory-a-inject1-r60.log`).
## 4. Publish and pin

- [ ] 4.1 Commit unsigned (`git -c commit.gpgsign=false`) with explicit pathspecs, and push. Publish `ai-gateway-model-catalog` with the existing publish workflow. Verify: the Validate CI is green for the commit, the publish run succeeds, and `podman pull` of the digest works.
- [ ] 4.2 Pin the new digest in `compose.yaml` and `.env.example`. Verify: `ops/scripts/validate.sh .env.example` passes, with `compose.override.yaml` moved to `/var/tmp` and restored; CI is green after the pin commit; nothing under the private config is staged.

## 5. Production deploy (image only)

Run every step on an `openai/*` model.

- [ ] 5.1 Pre-capture, read-only, to `/var/tmp`:
  - the internal standard list bytes and the Codex manifest bytes for the pinned client versions;
  - public `/v1/models` bytes for a test key;
  - the `:9083/models-table` summary;
  - the private `config.yaml` and `catalog.lua` digests, and the current image pin;
  - the count of `wall_time` WARNs since the last start.

  Verify: every capture is non-empty.
- [ ] 5.2 Update the private `.env` image pin only. Run `ops/ansible` `deploy.yml -e component=model-catalog --check --diff`, then apply it the documented way. Verify:
  - the check diff shows only the model-catalog image line;
  - only `model-catalog` is recreated, and no other container ID changes;
  - it is healthy with restarts=0;
  - the logs show the script loaded with the same digest and no WARN.
- [ ] 5.3 Config unchanged. Run `deploy-config.yml -e component=model-catalog --check --diff`. Verify:
  - it reports `changed=0 failed=0`;
  - the private `config.yaml` and `catalog.lua` digests on the host equal the 5.1 capture;
  - the in-container `validate-config /app/config.yaml --script /app/catalog.lua` exits 0.

## 6. Verify in production

- [ ] 6.1 Watch the logs for several hours: at least 6 scheduled refreshes, plus 3 forced refreshes spread across the window. Verify:
  - zero `wall_time` WARNs and zero `init_timeout` WARNs;
  - every collection published or reused the previous inputs without a script error;
  - the cgroup memory peak is below 384 MiB, with `oom` and `oom_kill` at 0.
- [ ] 6.2 Post-capture and diff. Repeat the 5.1 captures. Verify:
  - the internal standard list bytes, the Codex manifest bytes and the public `/v1/models` bytes are `cmp`-identical to the pre-capture;
  - the member count is unchanged (344, unless native or AISIX inputs changed, in which case explain the diff from those captures first);
  - if the catalog differs for any other reason, roll back (design Migration Plan step 4) and stop for a user decision.
- [ ] 6.3 Write `docs/records/lua-membership-warm-worker-deployment-<date>.md`, without private values or the public hostname. Include:
  - the digests and timings;
  - the 1.7 and section 3 numbers;
  - the WARN counts before and after;
  - the `cmp` results;
  - the rollback commands.

  Commit unsigned and push. Verify: CI is green, and `git grep` of the record finds no private hostnames, IPs or keys.
