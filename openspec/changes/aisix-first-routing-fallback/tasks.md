## 1. AISIX `*` fallback, memory and pin

- [ ] 1.1 Add a self-test to `ops/scripts/check-aisix-resource-policy.py`, then implement it. Require exactly one direct row `display_name: "*"` with `model_name: "*"`, `provider: openai`, the CPA pool `provider_key` and the shared cooldown block, and reject any routing target naming `*`. Keep other single-`*` globs legal. **Verify:** each self-test case fails when its row is missing, duplicated, has the wrong `model_name` or provider key, or is targeted by a route. The production-shaped scratch resources with the row pass. The Ansible AISIX deploy runs the same validator.
- [ ] 1.2 Add the `*` row to `services/routing/aisix/resources.example.yaml` with a one-line comment. Add it to the private `resources.yaml`; do not commit it. **Verify:** the validator, normalizer (`normalized 0`) and orphan report pass on both files.
- [ ] 1.3 Set AISIX `mem_limit: 512m` and `memswap_limit: 512m` in `compose.yaml`. Pin `ghcr.io/api7/aisix:1.6.0@sha256:26eae08b5c94291050f179d3c0a56ca154f40751f5c755b5922b044efc523690` in `compose.yaml` and `.env.example`. **Verify:** using the PART 1 harness at 512m with no swap, one 64 MiB Responses request through `*` returns 200 with no OOM. Record the peak. `validate.sh .env.example` passes with the new pin.

## 2. Slim apisix-models

- [ ] 2.1 Update the route tests first. In `ops/scripts/tests/test-catalog-routes.sh` and `test-standard-cache-topology.py`, assert:
  - `/healthz` is local;
  - `GET /v1/models`, with and without `client_version`, reaches the model-catalog stub;
  - `POST` `/v1/responses`, `/v1/chat/completions`, `/v1/images/generations` and multipart `/v1/images/edits` reach the AISIX stub with the caller `Authorization` and a byte-identical body, including `prompt_cache_retention`;
  - SSE arrives incrementally;
  - a 64 MiB Responses body is admitted, and 64 MiB + 1 gets 413;
  - no route reaches a CPA stub.

  **Verify:** the tests fail against the current selector configuration and pass after 2.2.
- [ ] 2.2 Rewrite `services/routing/apisix-models/apisix.yaml` per design D2:
  - upstreams `model-catalog` and `aisix` only;
  - routes `local-health`, `codex-models` and `aisix` (`/*`, `enable_websocket`, unbuffered);
  - no `global_rules`, `serverless-*`, `client-control` or `__CPA_API_KEY__`.

  In `config.yaml`, drop `extra_lua_path` and the `172.30.34.2` listener and keep `client_max_body_size: 64m`. In `start.sh`, remove the `CPA_API_KEY` check and the `sed`, and copy the template. Delete `routing_selector.lua` and `routing_selector_test.lua`. **Verify:** `apisix test` passes, the listener render shows only the loopback, `172.30.12.2` and `172.30.45.3` listeners, tests 2.1 pass, and no file in the directory mentions CPA.

## 3. Network cut, recovery and validation

- [ ] 3.1 Add the negative test before removing anything. In `ops/scripts/validate.sh`, add a reachability walk from `apisix-models-netns` over networks and socat relays. The walk does not traverse application namespaces. It fails if `cpa-netns` is reachable, if `apisix-models-netns` has a `cpa` extra host, if `apisix-models` carries `CPA_API_KEY`, or if `apisix.yaml` names `cpa:` or `8317`. Add an intentional-break fixture that re-adds `apisix-models-cpa-relay`. **Verify:** the check fails on the current `compose.yaml` and on the fixture, and passes after 3.2.
- [ ] 3.2 Apply design D3 in `compose.yaml`:
  - remove `apisix-models-cpa-relay`, `apisix-models-host-netns` and `apisix-models-host-relay`;
  - remove the four networks `apisix-models-cpa-source`, `apisix-models-cpa-target`, `apisix-models-host-source` and `host-apisix-models-target`;
  - remove the `cpa` extra host and both attachments from `apisix-models-netns`;
  - remove `CPA_API_KEY`, the CPA and host-relay dependencies and the selector mount from `apisix-models`;
  - remove the `apisix-models-cpa-target` attachment from `cpa-netns`.

  Update the selector-specific checks in `validate.sh`: the mount, the `__CPA_API_KEY__` count, the 16 MiB global rule, catch-all→CPA, the `routing_selector_test.lua` run and the AISIX edge comments. Keep the front-catalog pair and the aisix-netns membership checks. **Verify:** with `compose.override.yaml` moved to /var/tmp and ALWAYS restored afterwards, `./ops/scripts/validate.sh .env.example` passes, and the 3.1 fixture still fails.
- [ ] 3.3 Change the CPA probe in `ops/scripts/host/gateway-recovery.py` to read `CPA_CLIENT_KEY` from the model-catalog container environment. Keep the `apisix-models` `/healthz` check and the in-memory-only key handling. Update `ops/scripts/tests/test-gateway-recovery.py` and its Compose section split. **Verify:** the recovery tests pass, a missing `CPA_CLIENT_KEY` fails closed as before, and no code reads the `apisix-models` environment.
- [ ] 3.4 Ansible: confirm that `ai_ops_component` profiles and argument specs, the model-catalog protected identities and the inventory examples name no removed service. Keep `apisix-models` as a component. Run the existing Ansible tests (`test-ansible-model-catalog-config.sh` and the component-profile/argument-spec equality test). **Verify:** the tests pass, and any list change is limited to removed names. None is expected; record "no change" if none is found.

## 4. Documentation

- [ ] 4.1 Update the `apisix-models` row in `README.md` to where/what/how: constant routes, `/v1/models` → model-catalog, everything else → AISIX, no host port. Rewrite `services/routing/apisix-models/README.md` the same way. In `services/routing/aisix/README.md`, document the `*` fallback row in one line. **Verify:** the READMEs stay short and contain no `9081`, slash rule or CPA upstream.
- [ ] 4.2 Rewrite `docs/model-routing.md`:
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

## 5. Production window

- [ ] 5.1 Pre-window gates, all read-only or local:
  - CI is green on the repository changes;
  - prove how CPA v8.0.45 applies `requests.payload.filter`: a config reload that leaves plugins untouched, or a restart;
  - confirm that the filter matcher covers every CPA-received Kimi name (`kimi-coding/kimi-k3`, `sensenova/kimi-k3`, `devin/kimi-k2-7`, CPA-only bare Kimi IDs);
  - prepare the private diffs (Compose, `.env`, AISIX `resources.yaml`, CPA `config.yaml`) and the exact command list from design Migration Plan, and have them reviewed.

  **Verify:** the review is approved before any production write, and the method choice is recorded.
- [ ] 5.2 Window step 1, backups and baselines, exactly as listed in design Migration Plan step 1, including an online CPA SQLite backup and one temporary normal-client key. **Verify:** every backup is readable and hashed; the five catalog bodies, AISIX metrics and Sub2API account rows are captured.
- [ ] 5.3 Window step 2, the CPA retention filter. **Verify:** Kimi requests with `prompt_cache_retention` return 200, streaming and non-streaming, sent directly to CPA. A supporting upstream keeps the field. Plugins and version keys are unchanged, and there is no plugin reload.
- [ ] 5.4 Window step 3, AISIX: Compose limits and pin plus the `*` row, through `deploy-config.yml -e component=aisix` (`--check --diff`, then apply). **Verify:** AISIX is healthy at 512m with no swap, on the pinned digest; `/v1/models` has 76 IDs and no `*`; a CPA-only bare ID through the public chain succeeds.
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
  - One 64 MiB Responses body: 200 with no AISIX OOM, peak recorded.
  - Codex manifest and plain list via Sub2API.
  - Public standard and Codex lists, plus the five catalog bodies, `cmp`-identical to the baseline.
  - CPA unreachable from the `apisix-models` netns.
  - The recovery timer is healthy.
  - Restart counts are 0 and protected identities are unchanged except the services recreated on purpose.

  **Verify:** every item passes, or the window rolls back per design. The temporary key is deleted with proof: DELETE 200, GET 404, local file absent.

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
- [ ] 6.2 Sync the delta specs and archive the change. **Verify:** `openspec validate --strict` passes after the archive, and the main specs no longer contain the slash rule or the classification seam.
