## 1. AISIX `*` fallback, memory and pin

- [x] 1.1 Add a self-test to `ops/scripts/check-aisix-resource-policy.py`, then implement it. Require exactly one direct row `display_name: "*"` with `model_name: "*"`, `provider: openai`, the CPA pool `provider_key` and the shared cooldown block, and reject any routing target naming `*`. Keep other single-`*` globs legal. **Verify:** each self-test case fails when its row is missing, duplicated, has the wrong `model_name` or provider key, or is targeted by a route. The production-shaped scratch resources with the row pass. The Ansible AISIX deploy runs the same validator.

  Done: self-test covers missing, duplicate, wrong `model_name`/`provider_key`/`provider`, missing cooldown, route targeting `*`, `*` as a routing model, and keeps `devin/*` legal; an ablation with the old `validate()` fails it. The production-shaped scratch resources with the row pass (routes=23); the Ansible AISIX role runs this same script with `--yaml`.
- [ ] 1.2 Add the `*` row to `services/routing/aisix/resources.example.yaml` with a one-line comment. Add it to the private `resources.yaml`; do not commit it. **Verify:** the validator, normalizer (`normalized 0`) and orphan report pass on both files.

  Unchecked: the private half waits for window step 5.4. Done for the tracked example (validator, `normalized 0`, orphan report with no candidates) and for the production-shaped scratch copy. Not done here by instruction: the private `resources.yaml` is not edited; the row lands with window step 5.4. Until then, any `deploy-config -e component=aisix` from this revision refuses a private file without the row.
- [x] 1.3 Set AISIX `mem_limit: 512m` and `memswap_limit: 512m` in `compose.yaml`. Pin `ghcr.io/api7/aisix:1.6.0@sha256:26eae08b5c94291050f179d3c0a56ca154f40751f5c755b5922b044efc523690` in `compose.yaml` and `.env.example`. **Verify:** using the PART 1 harness at 512m with no swap, one 64 MiB Responses request through `*` returns 200 with no OOM. Record the peak. `validate.sh .env.example` passes with the new pin.

  Production caveat (2026-10-29): this local harness result does not establish safety for every body shape. Attempt 3 later OOM-killed AISIX at 512m on a 67,108,692-byte Responses envelope; production activation remains incomplete.

  Done: PART 1 harness at `--memory 512m --memory-swap 512m`, AISIX 1.6.0 pinned digest, two consecutive 64 MiB Responses bodies through `*`: 200, `memory.peak=450048000` of `memory.max=536870912`, `oom_kill=0` (`/var/tmp/ai-aisix-star/impl/mem-512m.txt`). `validate.sh .env.example` passes with the new pin.

## 2. Slim apisix-models

- [x] 2.1 Update the route tests first. In `ops/scripts/tests/test-catalog-routes.sh` and `test-standard-cache-topology.py`, assert:
  - `/healthz` is local;
  - `GET /v1/models`, with and without `client_version`, reaches the model-catalog stub;
  - `POST` `/v1/responses`, `/v1/chat/completions`, `/v1/images/generations` and multipart `/v1/images/edits` reach the AISIX stub with the caller `Authorization` and a byte-identical body, including `prompt_cache_retention`;
  - SSE arrives incrementally;
  - a 64 MiB Responses body is admitted, and 64 MiB + 1 gets 413;
  - no route reaches a CPA stub.

  **Verify:** the tests fail against the current selector configuration and pass after 2.2.

  Done in `test-catalog-routes.sh` (the AISIX stub records path, method, `Authorization`, content type and body SHA-256; `test-standard-cache-topology.py` asserts the route set). Red against the old selector config: requests reached `cpa` and `/healthz` was not local (`/var/tmp/ai-aisix-star/impl/routes-red.txt`); green after 2.2 (`routes-green.txt`). Also asserts `/healthz` returns 200 with every upstream down and reaches no upstream.
- [x] 2.2 Rewrite `services/routing/apisix-models/apisix.yaml` per design D2:
  - upstreams `model-catalog` and `aisix` only;
  - routes `local-health`, `codex-models` and `aisix` (`/*`, `enable_websocket`, unbuffered);
  - no `global_rules`, `serverless-*`, `client-control` or `__CPA_API_KEY__`.

  In `config.yaml`, drop `extra_lua_path` and the `172.30.34.2` listener and keep `client_max_body_size: 64m`. In `start.sh`, remove the `CPA_API_KEY` check and the `sed`, and copy the template. Delete `routing_selector.lua` and `routing_selector_test.lua`. **Verify:** `apisix test` passes, the listener render shows only the loopback, `172.30.12.2` and `172.30.45.3` listeners, tests 2.1 pass, and no file in the directory mentions CPA.

  Done. `local-health` uses `fault-injection` `abort` 200 because the pinned APISIX 3.19.0 rejects `request-termination` as an unknown plugin (see design D2). `apisix test` passes; the rendered `:9080` listeners are exactly loopback, `172.30.12.2` and `172.30.45.3` (validate.sh now asserts this). The only `cpa` text left in the directory is the relay name `sub2api-cpa-relay` in the README.

## 3. Network cut, recovery and validation

- [x] 3.1 Add the negative test before removing anything. In `ops/scripts/validate.sh`, add a reachability walk from `apisix-models-netns` over networks and socat relays. The walk does not traverse application namespaces. It fails if `cpa-netns` is reachable, if `apisix-models-netns` has a `cpa` extra host, if `apisix-models` carries `CPA_API_KEY`, or if `apisix.yaml` names `cpa:` or `8317`. Add an intentional-break fixture that re-adds `apisix-models-cpa-relay`. **Verify:** the check fails on the current `compose.yaml` and on the fixture, and passes after 3.2.

  Done as `ops/scripts/check-apisix-models-no-cpa.py`, run by validate.sh on the rendered Compose, plus the fixture `ops/scripts/tests/fixtures/apisix-models-cpa-relay.override.yaml` that validate.sh requires to fail. Red on the old `compose.yaml` (all four violations), red on the fixture, green after 3.2.
- [x] 3.2 Apply design D3 in `compose.yaml`:
  - remove `apisix-models-cpa-relay`, `apisix-models-host-netns` and `apisix-models-host-relay`;
  - remove the four networks `apisix-models-cpa-source`, `apisix-models-cpa-target`, `apisix-models-host-source` and `host-apisix-models-target`;
  - remove the `cpa` extra host and both attachments from `apisix-models-netns`;
  - remove `CPA_API_KEY`, the CPA and host-relay dependencies and the selector mount from `apisix-models`;
  - remove the `apisix-models-cpa-target` attachment from `cpa-netns`.

  Update the selector-specific checks in `validate.sh`: the mount, the `__CPA_API_KEY__` count, the 16 MiB global rule, catch-all→CPA, the `routing_selector_test.lua` run and the AISIX edge comments. Keep the front-catalog pair and the aisix-netns membership checks. **Verify:** with `compose.override.yaml` moved to /var/tmp and ALWAYS restored afterwards, `./ops/scripts/validate.sh .env.example` passes, and the 3.1 fixture still fails.

  Done; `./ops/scripts/validate.sh .env.example` exits 0 (this worktree has no `compose.override.yaml`; the shared one was never moved). The fixture still fails.
- [x] 3.3 Change the CPA probe in `ops/scripts/host/gateway-recovery.py` to read `CPA_CLIENT_KEY` from the model-catalog container environment. Keep the `apisix-models` `/healthz` check and the in-memory-only key handling. Update `ops/scripts/tests/test-gateway-recovery.py` and its Compose section split. **Verify:** the recovery tests pass, a missing `CPA_CLIENT_KEY` fails closed as before, and no code reads the `apisix-models` environment.

  Done: `test-gateway-recovery.py` 24 tests pass, including a stale `apisix-models` key that is never read and a missing `CPA_CLIENT_KEY` that yields an unknown (`ok: None`) probe as before.
- [x] 3.4 Ansible: confirm that `ai_ops_component` profiles and argument specs, the model-catalog protected identities and the inventory examples name no removed service. Keep `apisix-models` as a component. Run the existing Ansible tests (`test-ansible-model-catalog-config.sh` and the component-profile/argument-spec equality test). **Verify:** the tests pass, and any list change is limited to removed names. None is expected; record "no change" if none is found.

  No change needed: no removed service name appears in `ops/ansible`. `test-ansible-model-catalog-config.sh`, `test-ansible-operations.py`, `test-ansible-preflight.py`, `test-ansible-managed-file.py`, `test-ansible-aisix-mode.py`, `test-ansible-squid-source.sh` and `test-component-deploy.py` pass locally with the pinned collections.

## 4. Documentation

- [x] 4.1 Update the `apisix-models` row in `README.md` to where/what/how: constant routes, `/v1/models` → model-catalog, everything else → AISIX, no host port. Rewrite `services/routing/apisix-models/README.md` the same way. In `services/routing/aisix/README.md`, document the `*` fallback row in one line. **Verify:** the READMEs stay short and contain no `9081`, slash rule or CPA upstream.
- [x] 4.2 Rewrite `docs/model-routing.md`:
  - the rule: AISIX priority, then `*` → CPA;
  - the topology;
  - the Sub2API account contract (unchanged);
  - the unknown-everywhere error shape;
  - attribution (`model="*"` in metrics);
  - retention owned by CPA;
  - the 512m memory limit and its concurrency risk;
  - how to read the internal catalog without 9081 (`docker exec` into `apisix-models` with bash `/dev/tcp` to `127.0.0.1:9080`);
  - the pre-deploy checklist (replacing the selector hash check).

  Reconcile `docs/architecture.md`, `docs/security.md`, `docs/layout.md`, `docs/gateway-health-and-recovery.md`, `docs/websocket-termination.md`, `docs/catalog.md`, `docs/model-catalog.md` and `services/routing/README.md`. **Verify:** `rg -n 'routing_selector|slash rule|9081|apisix-models-cpa|apisix-models-host'` over docs and service READMEs finds only historical records.

  Done. Remaining hits are historical only: the 2026-10-22 incident list in `docs/model-routing.md`, `docs/records/`, the unarchived historical change `openspec/changes/stabilize-responses-path`, and the keeper submodule README (separate repository, not edited here).

## 5. Production window

- [x] 5.1 Pre-window gates, all read-only or local:
  - CI is green on the repository changes;
  - prove how CPA v8.0.45 applies `requests.payload.filter`: a config reload that leaves plugins untouched, or a restart;
  - confirm that the filter matcher covers every CPA-received Kimi name (`kimi-coding/kimi-k3`, `sensenova/kimi-k3`, `devin/kimi-k2-7`, CPA-only bare Kimi IDs);
  - prepare the private diffs (Compose, `.env`, AISIX `resources.yaml`, CPA `config.yaml`) and the exact command list from design Migration Plan, and have them reviewed.

  **Verify:** the review is approved before any production write, and the method choice is recorded.

  Done: the reviewed runbook was approved on 2026-10-29 with immediate execution, in-place CPA hot reload, namespace recreation (not live disconnect), and one CPA operator. The first attempt stopped on an upstream Kimi entitlement failure; a supervisor-authorized second attempt subsequently rolled back on the final passthrough-membership gate. See the window record.
- [x] 5.2 Window step 1, backups and baselines, exactly as listed in design Migration Plan step 1, including an online CPA SQLite backup and one temporary normal-client key. **Verify:** every backup is readable and hashed; the five catalog bodies, AISIX metrics and Sub2API account rows are captured.

  Done: readable, hashed file/unit backups and two integrity-checked online SQLite backups; fresh five-body baseline, AISIX metrics, account rows and identities captured. Temporary keys 41 and 42 were deleted (DELETE 200, GET 404, file absent).
- [ ] 5.3 Window step 2, the CPA retention filter. **Verify (approved correction, 2026-10-29):** remove the existing `from-protocol: responses` restriction by an in-place rewrite, so every protocol strips the field. Responses and Chat requests carrying `prompt_cache_retention`, streaming and non-streaming, return 200 on a working Kimi route; a non-Kimi check also returns 200. Plugins and version keys remain unchanged with no plugin reload. The independently failing `kimi-coding/kimi-k3` credential is not repaired by this change.

  Rolled back: `kimi-k3` passed all four direct checks and `claude-sonnet-5` passed; CPA logged one filter reload with unchanged inode/plugin hash. Initial `kimi-coding/kimi-k3` returned 403, then 503 on the original config as well: its upstream subscription lacks k3 entitlement. Final rollback restored the original Responses-only filter.
- [ ] 5.4 Window step 3, AISIX: Compose limits and pin plus the `*` row, through `deploy-config.yml -e component=aisix` (`--check --diff`, then apply). **Verify:** AISIX is healthy at 512m with no swap, on the pinned digest; `/v1/models` has 76 IDs and no `*`; a CPA-only bare ID directly at AISIX succeeds; repeat through the public chain after 5.5 (before then the old selector still bypasses AISIX).

  Rolled back: AISIX briefly ran at 512m/no swap on the pinned digest, with 76 listed IDs and no `*`; direct `claude-sonnet-5` returned 200. The final gate failed later, so original resources and 192m/64m-swap limits are restored.
- [ ] 5.5 Window step 4, slim `apisix-models` and remove its CPA and host relays. **Verify:** `apisix-models` is healthy, the Codex manifest via Sub2API is byte-identical to the baseline, and inference reaches AISIX.
- [ ] 5.6 Window step 5: recreate `cpa-netns` without `apisix-models-cpa-target`, then remove the four networks. **Verify:** CPA is healthy with its plugins loaded. From `apisix-models-netns`, TCP connects to `cpa:8317` and to every former CPA address on the removed networks fail.
- [ ] 5.7 Window step 6: deploy the recovery script through the recovery playbook (`--check --diff`, then apply). **Verify:** a manual timer run reports every check healthy, and the CPA probe uses the model-catalog key.
- [ ] 5.8 Window step 7, the full post-check list. Use small prompts and small max tokens; record statuses, not prompts.
  - An AISIX-known bare route (`claude-opus-5-5`), Responses and Chat.
  - All 53 AISIX slash direct IDs. Each resolves to its own row: the AISIX metrics delta is on `model=<id>`, never `model="*"`. Upstream availability failures are recorded, not fixed.
  - A CPA-only bare ID and a CPA-only slash ID through `*`, with the ID unchanged in the AISIX access log.
  - Unknown everywhere: Responses and Chat return the documented 400 `upstream_error` shape. The Sub2API account status is unchanged afterwards.
  - Images: generations for one gpt-image and one grok-imagine model, plus one multipart edit.
  - SSE incremental for Responses and Chat, plus a two-turn keeper continuation.
  - One 64 MiB Responses body: no hop returns 413, no AISIX OOM, and peak recorded. An upstream context-limit 400 is acceptable (supervisor-approved reinterpretation, 2026-10-29).
  - Codex manifest and plain list via Sub2API.
  - Public standard and Codex lists, plus the five catalog bodies, `cmp`-identical to the baseline.
  - CPA unreachable from the `apisix-models` netns.
  - The recovery timer is healthy.
  - `apisix-models` `/healthz` returns 200 with no upstream hit (AISIX and model-catalog access logs unchanged), and the Compose health is `healthy`.
  - Restart counts are 0 and protected identities are unchanged except the services recreated on purpose.

  **Verify:** every item passes, or the window rolls back per design. The temporary key is deleted with proof: DELETE 200, GET 404, local file absent.

  Operational gate approved by the user for attempt 3 (supersedes the strict byte/passthrough gate used in attempts 1–2): fresh baselines are evidence, not a byte-identity requirement. Investigate briefly, record and continue through source-explained metadata/native/passthrough churn and isolated upstream 429/502 responses. Roll back only for real change-caused breakage: unhealthy/crash-looping services, known-good public inference regression, CPA reachable from the inner namespace, nonlocal `/healthz`, AISIX OOM, data/credential loss, or catalog membership loss unexplained by native inventory. Observe 30–60 minutes of closing live traffic; compare status mix, per-model 5xx/timeouts, 413s, memory/OOM, CPA state and recovery runs with a same-length pre-period where retained evidence permits.

  Attempts 1–2 rolled back under the earlier strict gates (including passthrough 46 → 47 as Kimi recovered). The user explicitly replaced that philosophy and authorized attempt 3; benign inventory churn did not trigger that rollback. Attempt 3 exercised all 53 slash IDs (all 53 own-row counters increased), public images, SSE and keeper continuation successfully, with upstream-account failures recorded. It then hit a **real AISIX OOM at 512m** on a 67,108,692-byte Responses envelope at 08:26:45 UTC. Docker events prove OOM/die/start despite the subsequently reset `OOMKilled` flag. Full rollback completed 08:29:08; key 43 DELETE 200 / GET 404 / file absent. Steps 5.3–5.8 remain unchecked because activation was reversed. See `docs/records/aisix-first-routing-fallback-2026-10-29.md`.

## 6. Record and closeout

- [ ] 6.1 Write `docs/records/aisix-first-routing-fallback-<date>.md` in English, with no IPs, no public hostname and no secrets. Include:
  - the step timestamps;
  - image digests before and after;
  - the post-check results table;
  - the catalog SHA-256 values;
  - the CPA filter method;
  - AISIX peak memory for the 64 MiB probe;
  - the residual risk that two concurrent 64 MiB requests can OOM AISIX at 512m;
  - the error-shape change;
  - the `model="*"` attribution;
  - the rollback commands.

  **Verify:** the record is committed with an explicit pathspec and CI is green.

  Failure history is recorded for attempts 1–3, including the user-approved operational gates and the real attempt-3 OOM. This remains unchecked as successful-deployment closeout: production is back on the original topology. The restored-path 30-minute observation (31 samples, healthy CPA, no further OOM, 48 successful recovery runs) is recovery evidence, not a successful candidate observation.
- [ ] 6.2 Sync the delta specs and archive the change. **Verify:** `openspec validate --strict` passes after the archive, and the main specs no longer contain the slash rule or the classification seam.
