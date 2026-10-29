## 1. Tests first (red against the current code)

All tests live under `services/routing/model-catalog/tests/`. Each one is written before section 2, and must fail or be unrunnable against the current per-collection worker. Record the red run in this task list. Tests may lower `LIMITS` through `RunOptions.limits`, as the existing bound tests do. They may also use a test-only worker file through `RunOptions.workerFile`.

- [ ] 1.1 Init timeout, in `membership.test.ts`. A test worker that never posts `ready`, run with `initMs: 200`, rejects with class `init_timeout`. The worker thread has exited when the promise settles, and no `run` message was posted, so no script was sent. A second test worker sleeps 1.5 s before `ready`, then runs a script that needs about 1 s. With the production 10 s / 2 s limits it succeeds, because the execution clock started at `ready` (spec scenario "Slow initialization does not use the execution budget"). Verify: both tests are red now and green after section 2.
- [ ] 1.2 Execution timeout on a warm worker, in `membership.test.ts`. Do one successful run, then a second run on the same resident worker (assert the same thread ID) with a `select` that runs one long `string.find` past `wallMs`. The second run rejects with class `wall_time` and the message `collection exceeded <wallMs> ms`, and that worker has exited. A third run succeeds on a new thread ID. The existing endless-loop, memory-bomb, sandbox-access and invalid-return tests run both on a cold worker and on a warm one (a successful run first). Verify: the warm cases are red now and green after section 2.
- [ ] 1.3 Worker recreated after failure, in `membership.test.ts`. Use one case for each failure class: `init_timeout`, `wall_time`, `instruction_limit`, `memory_limit`, `runtime`, `load`, `invalid_return`, and `worker_exit` (the test worker calls `process.exit(1)`). The failing run's thread has exited before its promise rejects, and the next valid run succeeds on a new thread ID. An idle resident worker that exits on its own is replaced on the next run without a script error. Verify: red now, green after section 2.
- [ ] 1.4 Digest-change reload, in `membership.test.ts`. Run script A twice; the second run uses the same thread. Then run script B, which has a different digest and decides differently. The A worker has exited, B runs on a new thread, and B's decisions are returned. Then run B again; it reuses B's thread. Verify: red now, green after section 2.
- [ ] 1.5 No state leak between collections, in `membership.test.ts`. A script increments a global counter and keeps the candidate only while the counter is 1. It also writes a field into `string`, sets a metatable value on `""`, and stores a closure upvalue. Two runs on the same resident worker return identical decisions. A second script reads all four values, and sees each as absent or at its initial value. The `glob` cache is per run: a pattern that was invalid in run 1 is reported again in run 2, the same as on a cold worker. Verify: green now (cold worker) and still green after section 2. It must also still pass when the resident path is forced, by asserting the thread ID is unchanged.
- [ ] 1.6 Collection-level behavior, in `membership-stage.test.ts`:
  - an `init_timeout` failure keeps the previous generation serving, logs one WARN with class `init_timeout`, marks the table attempt failed, and the next collection retries and publishes;
  - the existing `wall_time`, hang, memory, invalid-return and fallback-to-previous-generation stage tests pass unchanged;
  - a forced refresh after a success reuses the resident worker;
  - `validate-config --script` and the startup dry-run leave no worker thread alive after they return.

  Verify: red now where the behavior is new; the existing tests stay green.
- [ ] 1.7 CPU-contention reproduction, using the report's harness. Script it under `services/routing/model-catalog/tests/`, or as a scratch script under `/var/tmp` that is referenced here. Setup:
  - a local container with the production profile, `--cpus 0.5 --memory 384m`;
  - the bundled image code;
  - the captured 564-candidate input and the production-shaped script from `/var/tmp/ai-lua-walltime/`;
  - pinned to one CPU with `taskset`, with 23 CPU burners that always end in `finally`.

  Run 20 consecutive collections through the real host API on a resident worker, with a cold first run. Verify:
  - against the current code (the deployed bundle), the same harness reproduces `membership script wall_time: collection exceeded 2000 ms`, which proves the load is in effect;
  - after section 2, 0/20 runs fail;
  - every run accepts the same 344 candidates;
  - the per-run init and exec times are recorded here.

## 2. Implementation

- [ ] 2.1 Worker protocol, in `src/membership-worker.ts` (design D1, D3). The worker keeps one `LuaFactory` for its lifetime and takes `{op: "init"}` and `{op: "run", script, candidates}` messages from `parentPort`.
  - On `init`, it creates a new engine with `setMemoryMax(16 MiB)`, a new glob cache, the limits and PRELUDE, then posts `ready`. It never sees the script.
  - On `run`, it loads the script, calls `select` for every candidate, closes the engine in `finally` and posts the reply.
  - A non-resident run exits after replying.

  Verify: 1.1, 1.2 and 1.5 are green.
- [ ] 2.2 Host lifecycle, in `src/membership.ts` (design D1, D4, D5).
  - Add `LIMITS.initMs = 10_000`.
  - Add one resident worker record `{worker, digest}` per process, with serialized runs.
  - Arm the init timer before `init` is posted. Arm the execution timer only on `ready`, before `run` is posted.
  - On any error reply, timeout, `error` event or `exit` event: clear the timers, `await worker.terminate()`, then drop the record.
  - Recreate the worker when the digest differs. An exit while idle only drops the record.
  - Add `resident: false` for `dryRun` and `validate-config`, and a shutdown hook from `src/main.ts`.

  Keep `runMembershipScript`'s signature and error classes, and add `init_timeout`. Verify: 1.1–1.6 are green.
- [ ] 2.3 Call sites. `src/authority.ts` is unchanged apart from passing the digest, if it is not already in `MembershipScript`. In `src/main.ts`, the dry-run and `validate-config` use `resident: false`, and server shutdown closes the resident worker. The compile-cache warm-up still loads the membership worker bundle. Verify: the `main` tests and `validate-config` output tests are byte-identical to before.
- [ ] 2.4 Docs. `docs/model-catalog.md`, the "Sandbox and limits" and "Failure behavior" paragraphs:
  - a resident worker with a fresh Lua state per collection;
  - the 10 s init bound, separate from the 2 s execution bound;
  - recreation after any failure or a digest change;
  - the `init_timeout` class.

  The "Runtime" paragraph names the resident membership worker. Verify: the link check finds 0 broken links, and no doc still says "short-lived worker per collection" for the membership worker.
- [ ] 2.5 Full suite and image. Verify:
  - `npm ci && npm run typecheck && npm test && npm run build` pass in `services/routing/model-catalog`;
  - `podman build` succeeds;
  - in the image, `/model-catalog validate-config /app/config.yaml --script /app/catalog.lua` exits 0 with `--read-only --network none`, and its output is byte-identical to the v1.3.0 image's;
  - 1.7 passes, with 0/20 failures, on the built image.

## 3. Memory measurement

- [ ] 3.1 Extend `tests/measure-memory.ts` (design D6). Use the bundled workers, the real metadata tables, and the tracked `config.yaml` chains, with `MALLOC_ARENA_MAX=2 node --max-old-space-size=96 --expose-gc`. Run at least 50 forced refreshes on the production-shaped fixture, each overlapping a Codex build, on one resident membership worker. Assert the thread ID stays the same, apart from deliberately injected failures. Verify:
  - peak process RSS is below 384 MiB;
  - settled RSS over the last 10 rounds shows no upward trend;
  - the membership worker's `resourceLimits` still reports 32 MiB.

  Record peak, settled range, slope and the delta against the archived 216.5 MiB peak and 159.1–176.9 MiB settled range in design.md under D6. If a cap would have to change, stop and report instead.
- [ ] 3.2 Repeat 3.1 with one injected `wall_time` failure every 10 rounds. Verify: RSS returns to the same settled range after each recreate, with no growth, and a new thread ID is seen after each injected failure.

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
