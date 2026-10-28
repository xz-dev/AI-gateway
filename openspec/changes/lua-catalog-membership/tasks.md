## 0. Gate

- [x] 0.1 Start only after `union-catalog-membership` has finished its production tasks (3.2–3.5) and is archived. Then rebase this change's MODIFIED requirements on the archived main specs. Verify: `openspec list` no longer shows `union-catalog-membership`; `openspec/specs/codex-models-enricher/spec.md` contains "Membership exception list"; `openspec validate lua-catalog-membership --strict` passes.

## 1. model-catalog code and tests

- [x] 1.1 Golden parity output (design Risks): before any code change, run the union parity fixture (`tests/parity.test.ts`, forced-prefix and default variants, plus a fallback variant), with `cpa_include: ["devin/*"]` and an `include_models` hide list. Write the member list (id, origin, preserveNative), the standard list bytes and the Codex manifest bytes to `tests/fixtures/membership-golden.json`. Verify: the file is committed with the test, and regenerating it from the union code gives no diff.
- [x] 1.2 Runtime and sandbox (D5, D6, D7):
  - add `wasmoon@1.16.0` as an exact pin;
  - add `src/membership-worker.ts` (prelude, count hook, removed globals, `setMemoryMax(16 MiB)` with `traceAllocations: true`, `glob`) and `src/membership.ts` (host: candidate build, worker with `maxOldGenerationSizeMb: 32`, 2 s termination, return validation);
  - the bundle emits the worker and copies `glue.wasm`; the Dockerfile copies them and `catalog.lua` to `/app/`.

  Verify: tests show that
  - `io`, `os`, `require`, `load`, `loadfile`, `dofile`, `debug`, `coroutine`, `collectgarbage`, `print` and `package` are `nil`;
  - `while true do end` in load and in `select` stops at its instruction bound;
  - the per-collection total bound trips with 1,000 candidates of 30,000 instructions each;
  - a 16 MiB string table fails at the memory bound;
  - a long `string.find` is terminated at 2 s while a concurrent HTTP request to the test server is answered;
  - each invalid return shape is rejected (nil, number, string, unknown opts key, source not listed);
  - a reason is cut at 200 bytes;
  - JS objects are not reachable.
- [x] 1.3 Default script (D3): add `services/routing/model-catalog/catalog.lua` exactly as in D3. Verify:
  - a property test feeds 10,000 random candidates (random sources, modes, prefixes, native flags) and every return is valid;
  - instruction use per call and per production-sized collection is logged by the test and stays under 1% of each bound;
  - the numbers are written into design D6.
- [x] 1.4 Membership stage (D2, D4):
  - `AuthorityOwner.#collect` builds candidates and runs the script;
  - the generation carries `members` and `rejected`;
  - `prepare()` takes members from the generation;
  - `admitModels`, `readCpaInclude` and `cpaInclude` are deleted;
  - `membership` joins the legacy table with the `catalog.lua` hint, and the `bare_models_takeover` hint is updated;
  - the tracked `config.yaml` comments point to `catalog.lua`.

  Verify: `authority.test.ts`, `catalog.test.ts` and `config.test.ts` cover every scenario of the codex-models-enricher delta:
  - channel-only model not a candidate;
  - prefixed, bare and slash-original CPA IDs;
  - extra list;
  - AISIX and CPA shared IDs;
  - admit-all and prefer-AISIX scripts;
  - `source` not listed is an error;
  - `glob` segment semantics;
  - `membership.cpa_include` rejected with the hint.
- [x] 1.5 Default-script parity: run the 1.1 fixture through the Lua path with the default script, with `EXTRA_CPA = { "devin/*" }` and the hides moved into the script. Verify: members (id, origin, preserveNative), the standard list bytes and the Codex manifest bytes equal `membership-golden.json` in all three prefix modes.
- [x] 1.6 Failure fallback and memory (D7, D8):
  - a load error, a runtime error, an invalid return, a bound hit and a worker exit each keep the previous generation;
  - each logs one WARN with class and candidate ID and without script text;
  - each marks the table attempt failed;
  - a failure before the first generation keeps `/readyz` not ready and retries on the cadence;
  - a script that validly rejects every candidate is NOT a failure: it publishes an empty membership, logs INFO with the candidate and rejected counts, and the table shows every candidate hidden with its reason.

  Verify:
  - `catalog.test.ts`/`server.test.ts` cover each failure case, and the old `/v1/models` body is served byte-identical after the failure;
  - a test with a reject-all script shows the empty catalog is published (an empty `data` list, ready), the INFO line, and no failed attempt in the table;
  - a measurement script runs 50 forced refreshes on the production-shaped fixture, with a build overlapping one collection;
  - it records peak and settled RSS in design D7: peak under 384 MiB, and no upward trend over the last 10 refreshes.
- [x] 1.7 `validate-config --script` and startup (D9): add the option, the synthetic dry-run set, `script_digest`, the two-`-` usage error, `CATALOG_SCRIPT_PATH`, and the startup dry-run. Verify:
  - tests for exit 0/1/2;
  - `script_digest` equals `sha256sum`;
  - stderr carries no script text;
  - a directory, a missing file and a 257 KiB file are rejected;
  - startup with a script without `select` exits non-zero before listening;
  - a script that rejects every synthetic candidate exits 0 with the digest JSON and one WARN with the rejected count on stderr;
  - output without `--script` is byte-identical to today's.
- [x] 1.8 Models table (D10): rejected rows `hidden by catalog.lua: <reason>`, the `rejected` summary count, `include_models` text unchanged. Verify: render tests for every scenario of the models-table-force-refresh delta (including the empty-catalog one), and a JSON-catalog comparison shows the markings do not change catalog bytes.
- [x] 1.9 Full suite and image. Verify:
  - `npm ci && npm run typecheck && npm test && npm run build` pass in `services/routing/model-catalog`;
  - `podman build` succeeds;
  - in the built image, `/model-catalog validate-config /app/config.yaml --script /app/catalog.lua` exits 0 with `--read-only --network none`;
  - the image size grows by less than 1 MiB.

## 2. Compose, publish and pin

- [x] 2.1 `compose.yaml`: mount `./services/routing/model-catalog/catalog.lua:/app/catalog.lua:ro,Z` and set `CATALOG_SCRIPT_PATH: /app/catalog.lua`. `ops/scripts/validate.sh` checks the tracked sample exists and is a file. Verify: `ops/scripts/validate.sh .env.example` passes, with `compose.override.yaml` moved to `/var/tmp` and restored; deleting the sample makes it fail.
- [ ] 2.2 Commit unsigned (`git -c commit.gpgsign=false`) and push, then publish `ai-gateway-model-catalog` with the current publish workflow. Pin the digest in `compose.yaml` and `.env.example`. Verify: CI is green for the commit, the publish run succeeds, and `podman pull` of the pinned digest works.

## 3. Ansible

- [x] 3.1 Second managed file and combination check (D11):
  - `ai_ops_model_catalog` deploy, verify and rollback handle `model-catalog/catalog.lua` when the private file exists;
  - activation happens once;
  - verify runs `validate-config /app/config.yaml --script /app/catalog.lua`;
  - on a failed verify, both files are restored;
  - the combination check runs first, also under `--check`.

  Update defaults, `argument_specs.yml` and the example inventory comments. Verify: `test-ansible-model-catalog-config.sh` passes with new cases:
  - custom script plus `include_models` fails under `--check` and under apply, naming both files;
  - no private script leaves the remote file untouched and allows `include_models`;
  - a script-only change recreates once;
  - both files changed with a failed verify restores both;
  - check mode uploads nothing.
- [x] 3.2 Lint and the full Ansible test set. Verify: the production lint gate and every `ops/scripts/tests/test-ansible-*` pass.

## 4. Docs

- [ ] 4.1 Docs (D12):
  - `services/routing/model-catalog/README.md` gets one line and a link;
  - `docs/model-catalog.md` gets the "Membership script" section: fields, return values, `glob`, sandbox and limits, failure behavior, `validate-config --script`, and the examples admit all, exclude a few, prefer AISIX on same-name IDs, and the extra CPA list;
  - `docs/ansible-operations.md` and `ops/ansible/README.md` list the second file, the custom rule, the combination check, and that coupled image/Compose/profile changes use a reviewed manual window;
  - the script reference states that a script rejecting everything publishes an empty catalog;
  - `docs/catalog.md` membership wording points to the script.

  Verify:
  - every docs example is executed by a test against the fixture and gives the documented result;
  - a link check finds 0 broken links;
  - `git grep -n cpa_include -- ':!openspec/changes/archive' ':!docs/records'` finds only the legacy-field table, its tests and this change.
- [ ] 4.2 Commit unsigned and push. Verify: CI is green, and `git status` shows no private-config file staged.

## 5. Production

Run every step on an `openai/*` model. The cutover itself (5.3) is a one-time reviewed manual window, because it couples the image, the Compose definition and the profile (design D11, Migration Plan step 4). Playbooks are used afterwards, `--check --diff` first.

- [ ] 5.1 Private profile (local only):
  - write the private `catalog.lua` per Migration Plan step 2, and delete `membership` and `metadata.include_models` from the private `config.yaml`;
  - copy into `HIDE` only the `include_models` hides that still match a live candidate at migration time; a hide that matches nothing (such as `!xl/grok-imagine-image-2.0` once `prune-dangling-aisix-targets` has removed that AISIX entry) is dropped and listed in the record;
  - re-check with the real matcher that no hide matches a static slug or an `inherit`/`metadata_from` target.

  Verify:
  - `podman run --rm --network none --read-only -v <private dir>:/candidate:ro <pinned image> validate-config /candidate/config.yaml --script /candidate/catalog.lua` exits 0;
  - a local run of the 1.5 harness on the private profile and a capture of the live native manifest and AISIX IDs gives the live member set;
  - the matcher check reports 0 conflicts.
- [ ] 5.2 Pre-capture, read-only, to `/var/tmp`: native manifest IDs, AISIX IDs, standard list bytes, Codex manifest bytes for the pinned client versions, member and passthrough sets, public `/v1/models` count for a test key, Sub2API count, `:9083/models-table` summary. Verify: every capture is non-empty, and the catalog count is recorded.
- [ ] 5.3 Manual window (Migration Plan step 4): back up the host `compose.yaml`, `.env`, `config.yaml` and any `catalog.lua`; pull the new image by digest and validate the private pair with a throwaway container of it; pause the recovery timer; upload the private `config.yaml`, `catalog.lua`, `compose.yaml` (mount and `CATALOG_SCRIPT_PATH`) and the `.env` image pin; `docker compose config -q`; one `docker compose up -d --no-deps --no-build model-catalog`; `POST :9083/models-table/refresh`; restart the recovery timer. Verify:
  - the uploaded `compose.yaml` differs from the backup only by this change's lines;
  - `model-catalog` is recreated once and no other container ID changes;
  - healthy, restarts=0;
  - logs show the script loaded and no WARN;
  - in-container `validate-config /app/config.yaml --script /app/catalog.lua` exits 0;
  - the window start, recreate and ready times are recorded.
- [ ] 5.4 Adopt baselines: run `deploy-config.yml -e component=model-catalog --check --diff` and `deploy.yml -e component=model-catalog --check --diff`. Verify: both report `changed=0 failed=0`; any stale baseline is re-adopted through the documented first-adoption path and the checks are rerun.
- [ ] 5.5 Post-capture and diff: repeat the 5.2 captures. Verify:
  - standard list bytes and Codex manifest bytes are byte-identical to the pre-capture (`cmp`);
  - the member and passthrough sets are equal;
  - the public and Sub2API counts are equal;
  - the table differs only by `catalog.lua`-rejected rows, the hide reason text and the new count.
  - If the native or AISIX captures changed between 5.2 and 5.5, stop and explain the diff from them before continuing.
  - If the catalog bytes differ for any other reason, roll back with the manual steps of Migration Plan step 8 and stop for a user decision.
- [ ] 5.6 Write `docs/records/lua-catalog-membership-deployment-<date>.md` with digests, timings, the diff summary, the 1.6 memory numbers and the rollback commands (Migration Plan step 8), without private values or the public hostname. Commit unsigned and push. Verify: CI is green, and `git grep` of the record finds no private hostnames or keys.
