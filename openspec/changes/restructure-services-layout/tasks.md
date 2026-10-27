## 1. openai-api-pi-extension

- [x] 1.1 Move every root `.ts` except `index.ts` to `src/`, and move `subagent-analysis*.md` to `docs/records/`. Update imports, tests, `package.json`, docs and the code map in `docs/development.md`. Verify: `PI_REQUEST_COMPRESSION_HELPER=… npm run check` has 0 failures; `pi -e .` loads the provider; there are no broken local doc links. Commit and push.

## 2. Repository tree

- [x] 2.1 Use `git mv` to move the components into `services/` per design D1. This includes `cpa/` → `services/cpa/config/`, `scripts/start-apisix-models.sh` → `services/routing/apisix-models/start.sh`, and the submodules under `services/middleware/` (also update `.gitmodules` and `.git/modules`). Add READMEs for `services/sub2api/`, `services/middleware/responses-ws-sse-bridge/` and `services/firewall/one-way-relay/`. Verify: `git submodule status` resolves all three submodules.
- [x] 2.2 Rename `models-enricher` to `model-catalog` per design D2: the code (binary name, log/service strings), its Dockerfile, its tests and its tracked `config.yaml` comments. Update the vendored-extension paths for task 1.1 and bump the submodule. Verify: `npm ci && npm run typecheck && npm test && npm run build` pass, and `podman build` succeeds.
- [x] 2.3 Move the dated records to `docs/records/` and `services/cpa/cpa-model-sync/docs/records/`, including `.deploy-progress.md` and `deploy-canary-report.md`. Delete `scripts/enricher-shadow-diff.mjs`. Verify: a local link check finds 0 broken links.
- [x] 2.4 Create `ops/` per design D1: `ansible/` → `ops/ansible/`, `scripts/` → `ops/scripts/`, `scripts/ops/` → `ops/scripts/host/`, `scripts/test-*` → `ops/scripts/tests/`. Fix the scripts' own repo-root detection and relative paths, the `.gitignore` private-config entry and the recovery unit's `ExecStart`, then move the local private config. Verify: `ops/scripts/validate.sh` and every `ops/scripts/tests/test-*` pass from the repo root.

## 3. Compose and APISIX

- [x] 3.1 Update `compose.yaml`: build contexts, bind mounts, the D2 names, and the volume `model-catalog-cache`. Remove `model-catalog-sidecar`, `models-table-relay`, `models-table-host-netns` and their networks; rename `apisix-models-enricher-relay` to `apisix-models-catalog-relay` (it carries `/v1/models` to `model-catalog`). Add the `front-catalog` and `front-model-table` networks, and the `9083` publication on `apisix-host-netns` using `${TAILSCALE_IP}`. Verify: `docker compose config -q`, and `ops/scripts/validate.sh` network rules pass.
- [x] 3.2 Front door (`services/firewall/apisix/`):
  - add the `9083` listener and its `/models-table` and `/models-table/refresh` routes to `model-catalog:8090`, matched only on that listener;
  - point the intersection Lua's catalog leg at `apisix-models` over `front-catalog`;
  - update the Lua tests.

  Verify: the Lua tests pass, and a routing test shows that the public listener does not match the models-table paths.
- [x] 3.3 `apisix-models`: keep only `/v1/models → model-catalog`, delete the realip models-table routes, and update the Lua and route tests. Verify: the tests cover both `client_version` cases and show that the models table is not forwarded.

## 4. Operations, CI and docs

- [x] 4.1 Ansible: rename tags, variables, tasks and protected-service lists per D2; move private-config paths; update `ops/scripts/tests/test-ansible-*`, `ops/scripts/host/gateway-recovery.py` and its tests. Verify: all `ops/scripts/tests/test-*` pass and `ansible-playbook ops/ansible/ops.yml --syntax-check` succeeds.
- [x] 4.2 CI and Dependabot: rename the workflow to `publish-model-catalog.yml` (image `ai-gateway-model-catalog`) and update the paths in `validate.yml` and `dependabot.yml`. Verify: `check-jsonschema` passes for both, and CI is green after the push.
- [x] 4.3 Add `ops/scripts/host/migrate-services-layout.py` (plan/apply, idempotent, backup and receipt) for the deploy root and `ansible/private-config`. Verify: a test against a fixture tree covers plan, apply, re-apply as a no-op, and restore.
- [x] 4.4 Docs: the root README's component table, documentation index and architecture diagram follow the new tree (`model-catalog` reads CPA and AISIX; no sidecar), and every current `docs/` page is updated for the new paths and names. Every `services/<group>/` and every component directory under it, plus `ops/ansible/` and `ops/scripts/`, has its own short README saying where it sits, what it does and how to use it, with a link to its `docs/` page. Also update the Purpose of `codex-models-enricher` and `models-enricher-runtime`. Verify: `git grep -nE 'models-enricher|model-catalog-sidecar'` hits only `openspec/changes/archive`, `docs/records` and the D2 table; link check is 0 broken; `openspec validate --all --strict` passes.
- [x] 4.5 Commit (unsigned) and push, then publish `ai-gateway-model-catalog` from the new path. Verify: the publish run succeeds, and the digest is pinned in `compose.yaml` and `.env.example`.

## 5. Production migration

- [ ] 5.1 Run the migration script in plan mode, then apply it on the host and in the local private config; rewrite the installed recovery unit path and run `systemctl daemon-reload`. Verify: the receipt lists every moved path, the backup tree exists, `docker compose config -q` passes on the host, and `systemctl cat ai-gateway-recovery.service` shows `ops/scripts/host/`.
- [ ] 5.2 Batch 1 (catalog), per design Migration Plan step 2: copy the volume, bring up `model-catalog`, recreate `apisix-models` and the front door, remove the sidecar and the old enricher. Verify:
  - public `/v1/models` intersection for a test key;
  - Sub2API's catalog count is unchanged;
  - `:9083/models-table` renders and refresh works;
  - the Codex manifest is byte-identical to the pre-migration capture.
- [ ] 5.3 Batch 2 (routing): rename the AISIX caller label `enricher_catalog` to `model_catalog` in the host `caller-keys.json` (value unchanged), then recreate `aisix`, `aisix-status` and `cpa-model-sync` from their new paths. Install the recovery unit from `ops/scripts/host/`, then run `--finalize`. Verify: the `:3001/status` page works, one AISIX-routed and one CPA-routed inference succeed, cpa-model-sync completes a cycle, no old-path symlink remains, and the recovery timer's next run passes.
- [ ] 5.4 Re-adopt the Ansible baselines, then run a plan for every deploy tag. Verify: each plan reports no change. After one scheduled catalog refresh, remove `models-enricher-cache` and the backup tree, and verify that `docker volume ls` and the host tree no longer contain the old names.
