## Why

The repository layout no longer matches how the gateway works:
- Ten component directories sit flat at the root next to docs, scripts and reports.
- `middleware/` holds only two of the request-path middleware components.
- `docs/` and `cpa-model-sync/` mix current guides with dated records.
- The name `models-enricher` does not say what the service is. The architecture diagram draws it as an endpoint, but it is the read-only aggregator that builds the model catalog from CPA and AISIX.
- `model-catalog-sidecar` adds an nginx hop and two socat relays only to bridge the front door and the 9083 diagnostics to `apisix-models`.

## What Changes

- **BREAKING (operations)**: all components move under `services/`, grouped by role:
  - `services/sub2api/`;
  - `services/cpa/` (`config/`, `cpa-model-sync/`);
  - `services/routing/` (`apisix-models/`, `aisix/`, `aisix-status/`, `model-catalog/`);
  - `services/middleware/` (`ai-sse-keepalive-proxy/`, `openai-api-cache-keeper/`, `responses-ws-sse-bridge/`);
  - `services/firewall/` (`apisix/` front door, `egress-proxy/`, `one-way-relay/`).
- **BREAKING (operations)**: `models-enricher` is renamed `model-catalog` everywhere: directory, Compose service and network names, aliases, GHCR image, Ansible tags and variables, recovery tooling and docs. Production is migrated in steps.
- **BREAKING (operations)**: `model-catalog-sidecar` and its relays are removed. The front-door APISIX reaches `apisix-models` directly for the catalog leg of the `/v1/models` intersection. It serves `/models-table` and its refresh action on its own localhost/Tailscale listener (`:9083`), which goes straight to `model-catalog`.
- `apisix-models` routes only `/v1/models` to `model-catalog`. The models table is no longer routed through it.
- Localhost and Tailscale are a trusted domain: the 9083 listener has no source-address restriction, the same as the CPA and Sub2API operator ports.
- The architecture diagram shows `model-catalog` reading CPA and AISIX.
- **BREAKING (operations)**: a root `ops/` directory holds the tooling that operates the stack: `ansible/` → `ops/ansible/` (private config at `ops/ansible/private-config/`), and `scripts/` → `ops/scripts/`, with `scripts/ops/` → `ops/scripts/host/` and `scripts/test-*` → `ops/scripts/tests/`.
- Dated records move to `docs/records/` and `services/cpa/cpa-model-sync/docs/records/`. The two root reports also move to `docs/records/`. The completed `scripts/enricher-shadow-diff.mjs` is removed.
- openai-api-pi-extension: the source files move to `src/`, `index.ts` stays at the root as the Pi entry, and the analysis drafts move to `docs/records/`. The gateway's vendored build follows the new path.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `apisix-cpa-gateway`: `/v1/models` is routed to `model-catalog`; the models table is no longer routed through `apisix-models`; the front door's catalog leg is defined.
- `models-table-force-refresh`: the diagnostic surface moves to the front door's trusted listener and goes straight to `model-catalog`.
- `production-operations-control`: the configuration deployment requirements name `model-catalog` instead of `models-enricher`.

## Impact

- **Compose**:
  - service, network and volume names;
  - build contexts and bind-mount paths;
  - the front-door APISIX gains a second listener and two networks (to `apisix-models` and `model-catalog`);
  - `model-catalog-sidecar`, `models-table-relay`, `models-table-host-netns`, `apisix-models-enricher-relay` and related networks are removed.
- **APISIX**: front-door routes and the intersection Lua upstream; `apisix-models` routes.
- **Ansible** (now `ops/ansible/`): `ops.yml` tags (`deploy-model-catalog-config`, rollback), protected-service lists, role tasks and defaults, private-config paths (`ops/ansible/private-config/services/...`), the recovery unit path, and the production deploy-root layout.
- **CI and supply chain**: workflows (paths, image name `ai-gateway-model-catalog`), Dependabot directories, `.gitmodules` paths, `ops/scripts/validate.sh` and tests, recovery tooling (`ops/scripts/host/gateway-recovery.py`) and its systemd unit.
- **Docs**: all READMEs and `docs/` links. Historical records keep their content.
- **openai-api-pi-extension**: file moves, `package.json` entry, test imports and docs. The gateway submodule pointer and the Dockerfile check of `vendor/.../metadata-core.ts` change.
- **Production**: services are recreated in batches, and `models-enricher-cache` is migrated to `model-catalog-cache`. `data/` runtime state stays where it is.
