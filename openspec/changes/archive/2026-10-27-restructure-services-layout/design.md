## Context

See proposal.md for the motivation. Current constraints:
- The production deploy root `/root/AI-gateway` mirrors the repository tree. Compose bind-mounts repository paths such as `./apisix/apisix.yaml`, `./aisix/config.yaml`, `./models-enricher/config.yaml` and `./scripts/start-apisix-models.sh` (moved into its service). Runtime state lives in `./data/` and in named volumes.
- `ansible/private-config/` mirrors the deploy-root paths it manages. Ansible drift baselines are keyed by remote path.
- `scripts/validate.sh` (moving to `ops/scripts/`) enforces the network rules: every internal network has exactly two members, and edges are one-way.
- Catalog traffic today:
  - **Front-door intersection**: APISIX → `model-catalog-sidecar` (nginx) → `apisix-models` → `models-enricher`.
  - **9083 diagnostics**: host port → `models-table-relay` → sidecar → `apisix-models` → enricher.
  - **Sub2API catalog**: Sub2API → `apisix-models` → enricher.
  - In every case the enricher reads CPA and AISIX through one-way relays.

## Goals / Non-Goals

**Goals:**
- One `services/<group>/<component>/` tree. Each component's README sits next to its build context and config.
- Every component has one name, from the directory to the image to the Ansible tag.
- One fewer hop and container on the catalog path, with the same public exposure.

**Non-Goals:**
- No change to catalog content, routing rules, Sub2API entitlement logic or `data/` locations.
- OpenSpec capability IDs (`codex-models-enricher`, `models-enricher-runtime`) are not renamed. They are history keys. Only their Purpose text changes, at archive time.
- Submodule repositories (`ai-sse-keepalive-proxy`, `openai-api-cache-keeper`) are not restructured internally.

## Decisions

### D1 Target tree

```
services/
├── sub2api/                README (where/what/how); image-only service
├── cpa/
│   ├── config/             CLIProxyAPI config sample (was cpa/)
│   └── cpa-model-sync/     (docs/records/ holds the dated reports)
├── routing/
│   ├── apisix-models/      routes, selector Lua, start.sh (was scripts/start-apisix-models.sh)
│   ├── aisix/
│   ├── aisix-status/
│   └── model-catalog/      (was models-enricher)
├── middleware/
│   ├── ai-sse-keepalive-proxy/    submodule
│   ├── openai-api-cache-keeper/   submodule
│   └── responses-ws-sse-bridge/   README; image built in its own repo
└── firewall/
    ├── apisix/             front door: public allowlist, catalog intersection, 9083 listener
    ├── egress-proxy/       Squid + Unbound
    └── one-way-relay/      README for the socat relay and netns-owner templates
ops/
├── ansible/            playbook, roles, inventory; private-config/ (gitignored)
└── scripts/            init, validate, render, check, container-runtime
    ├── host/           host-side tooling installed on the deploy root (was scripts/ops/)
    └── tests/          test-* (was scripts/test-*)
docs/            current guides;  docs/records/ dated records and verification evidence
openspec/  skills/  compose.yaml  README.md
```

- `ops/` holds everything that operates the stack and is not a service. `ops/ansible/../scripts` still resolves to the scripts directory, so the playbook's `{{ playbook_dir }}/../scripts` lookups keep their shape.
- `scripts/ops/` becomes `ops/scripts/host/` to avoid the doubled `ops/scripts/ops/`. The systemd unit `ai-gateway-recovery.service` and the Ansible `deploy-recovery` task point to `/root/AI-gateway/ops/scripts/host/gateway-recovery.py`. The installed unit is updated in host batch 1.
- `.gitignore` ignores `ops/ansible/private-config/`. The local private config moves with `git mv`-equivalent paths, outside git.

- `model-catalog` sits under `routing/`. It is reached only through `apisix-models` (and the 9083 listener), and it reads routing inputs (CPA and AISIX membership).
- `one-way-relay/` has no build context. The relay and netns-owner definitions stay as Compose YAML anchors, because Compose cannot include anchors from another file. The README documents the pattern and links to its tests in `ops/scripts/tests/`.
- Tests stay together in `ops/scripts/tests/` so that CI and `validate.sh` keep one entry point. They are not spread across the component directories.
- `skills/` stays at the root. It is agent tooling, not a service.

### D2 Names

| Was | Becomes |
|---|---|
| directory `models-enricher/` | `services/routing/model-catalog/` |
| service `models-enricher`, `models-enricher-netns` | `model-catalog`, `model-catalog-netns` |
| alias `models-enricher`, relays `enricher-*-relay` | `model-catalog`, `catalog-*-relay` |
| networks `enricher-*`, `apisix-models-enricher-*` | `catalog-*`, `apisix-models-catalog-*` |
| volume `models-enricher-cache` | `model-catalog-cache` |
| image `ghcr.io/xz-dev/ai-gateway-models-enricher` | `ghcr.io/xz-dev/ai-gateway-model-catalog` |
| workflow `publish-models-enricher.yml` | `publish-model-catalog.yml` |
| Ansible tags `deploy-/rollback-models-enricher-config`, vars `ai_ops_models_enricher_*` | `deploy-/rollback-model-catalog-config`, `ai_ops_model_catalog_*` |
| binary `/models-enricher` | `/model-catalog` |

The sidecar's AISIX caller key keeps its value; only its env name is renamed in `.env.example`. The old GHCR package stays for rollback.

### D3 Catalog edges without the sidecar

- New two-member internal network `front-catalog`: `apisix-netns` ↔ `apisix-models-netns`.
  - The front door's intersection Lua uses it for its catalog leg, `GET /v1/models` only.
  - Front-door routes never select this upstream for inference.
- New two-member internal network `front-model-table`: `apisix-netns` ↔ `model-catalog-netns`.
  - The front door's second listener (`9083`) proxies `/models-table` and `/models-table/refresh` to `model-catalog:8090`.
  - `apisix-host-netns` publishes `127.0.0.1:9083` and `${TAILSCALE_IP}:9083` to that listener through its existing host-relay pattern. The tracked Compose file no longer contains the production Tailscale address.
  - Routes on the public listener (`server_port` 9080) never match these paths.
- `apisix-models` keeps only `/v1/models → model-catalog`. The `realip_remote_addr` models-table routes are deleted.
- Rejected alternatives:
  - **Keep the nginx gate**: an extra container, two relays and a third config language, only to forward three paths.
  - **Source-IP allowlist on 9083**: localhost and Tailscale are the trusted operator domain, the same as the CPA and Sub2API operator ports.

### D4 Deploy-root and private-config migration

- The host tree is moved with the same `git mv` mapping, applied by an idempotent script, `ops/scripts/host/migrate-services-layout.py`. It runs `--plan` before `--apply`, writes a receipt, and keeps the old paths in a timestamped backup. `--apply` leaves a compatibility symlink at each old path, so a container that Docker restarts before it is recreated still finds its bind-mount source. `--finalize` removes the symlinks and the retired `model-catalog-sidecar/` (backed up first) after every affected container runs from the new paths.
- `ansible/private-config/` moves the same way, to `ops/ansible/private-config/`. On the host, the installed recovery unit is rewritten to the new script path and `systemctl daemon-reload` runs.
- Ansible drift baselines for moved files are re-adopted through the existing "first adoption matches production" path. Local and remote bytes match after the move, so no service is recreated by baseline adoption.
- `model-catalog-cache` is filled by copying `models-enricher-cache` (`v2/`; `out/` is rebuilt). The old volume is removed only after observation.

### D5 openai-api-pi-extension

- Every root `.ts` except `index.ts` moves to `src/`. `index.ts` stays as the Pi entry (`package.json` `pi.extensions`).
- Tests import from `../src/...`.
- The gateway build follows the new path: its `tsconfig.core.json` include, the core import, the esbuild entries and the Dockerfile guard (`vendor/openai-api-pi-extension/src/metadata-core.ts`). The submodule pointer is bumped in the same gateway commit.

## Risks / Trade-offs

- [Several bind-mount paths change, so the front door, `apisix-models`, AISIX and the status page must be recreated] → Run one batch per component, each recreated once with all of its changes. Do it outside peak hours. Each batch has a readiness check and a rollback from the backup tree plus the previous Compose file.
- [Recreating `apisix-netns` restarts the public front door] → It is a single short restart, already part of the front-component procedure. Verify with public `/v1/models` and one inference request.
- [Ansible and its tests reference old names and paths in many places] → Run `ops/scripts/tests/test-ansible-*` and `ops/scripts/validate.sh` before any production step. `git grep` for old names must hit only `openspec/changes/archive`, `docs/records` and the rename table.
- [The extension move breaks a stale submodule checkout] → The gateway Dockerfile guard fails fast. CI builds with submodules.

## Migration Plan

1. **Repository.** Move the files, rename everything, update Compose, Ansible, CI, Dependabot, scripts, tests and docs. `validate.sh`, all `test-*`, the model-catalog tests and the extension check pass. Publish `ai-gateway-model-catalog`.
2. **Host batch 1 (catalog).** Move the tree, then bring up `model-catalog-netns`, `model-catalog` and its relays from the copied volume. Recreate `apisix-models` (new path, routes, `front-catalog`), then the front door (new path, listener, both networks). Remove the sidecar and its relays and networks, and the old enricher containers. Verify:
   - public `/v1/models` (intersection) for a test key;
   - Sub2API's catalog;
   - `:9083/models-table` and refresh;
   - one Codex `client_version` request.
3. **Host batch 2 (routing).** Recreate `aisix` and `aisix-status` (new config paths). Verify the status page and one AISIX-routed request.
4. **Ansible.** Re-adopt the baselines; run a plan for each deploy tag, which must report no change.
5. **Rollback.** Restore the backup tree and the previous Compose file, then `up -d` the affected services. The old volume and image are still present.
6. **Cleanup** after one scheduled catalog refresh: remove the old volume and the backup tree.
