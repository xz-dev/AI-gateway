## Why

The operator wants backends tried in priority order: AISIX first, and CPA only when AISIX does not define the model. Adding a backend later must not need special gateway configuration. The governing principle is that routing is a streaming pipe, not a bigger bucket.

AISIX already implements this priority. Its resolver (`crates/aisix-proxy/src/model_resolve.rs`, the same in 1.5.0 and 1.6.0) tries an exact name first, then the most specific single-`*` glob. A bare `*` matches any name, and `model_name: "*"` passes the requested ID upstream unchanged. One AISIX direct model `*` that targets the CPA pool therefore gives "AISIX's own models first, otherwise CPA" with no gateway body parsing.

Today `apisix-models` decodes every classified JSON body in Lua (`routing_selector.lua`) to apply a slash rule. That selector:
- OOMs at about 20 MiB under its 128 MiB limit;
- strips `prompt_cache_retention` in two places;
- keeps a direct network edge to CPA.

With AISIX deciding, all three go away. `apisix-models` stays, but only as a constant two-route entrance. Sub2API reads the Codex manifest and the model list from its account base URL, so `GET /v1/models` must keep reaching model-catalog. AISIX cannot serve that.

## What Changes

- **BREAKING:** remove the slash rule. Every model ID first resolves in AISIX: exact route or direct model, then a more specific glob, then one `*` direct model → CPA pool with the ID unchanged. CPA-only bare IDs become callable (decision C). The 53 AISIX slash direct models keep going through AISIX (decision B).
- Add exactly one `*` direct model to the private AISIX resources, with the shared cooldown block. Extend the AISIX resource-policy validator so the `*` fallback row must exist, exactly once, with the correct shape.
- Slim `apisix-models` to constant routes:
  - `/healthz` answered locally;
  - `GET /v1/models` → model-catalog;
  - everything else → AISIX with the caller credential, SSE unbuffered.

  There is no body read and no Lua selector (`routing_selector.lua` and its test are deleted). There are no route body-limit plugins and no `cpa` upstream, and the CPA service key is removed from this service.
- Cut the `apisix-models` → CPA container-network path. Remove:
  - `apisix-models-cpa-relay`;
  - its two networks;
  - the `cpa` hosts entry;
  - the dependency;
  - the CPA namespace's membership on that target network.

  `validate.sh` gains a negative test that no relay chain leads from `apisix-models-netns` to `cpa-netns`.
- Remove the unused `127.0.0.1:9081` host relay (`apisix-models-host-netns`, `apisix-models-host-relay`, two networks). No runbook uses it.
- Raise AISIX `mem_limit` from 192m to 512m and remove its swap (`memswap_limit: 512m`). The 64 MiB Responses contract stays.
- Align the tracked AISIX image pin with production: `ghcr.io/api7/aisix:1.6.0@sha256:26eae08b5c94291050f179d3c0a56ca154f40751f5c755b5922b044efc523690`.
- Remove gateway `prompt_cache_retention` stripping: both the selector strip and the catch-all `dkjson` strip. CPA strips the field instead, through targeted `requests.payload.filter[].params` rules, initially for Kimi. This needs a reviewed live-config step, because `deploy-config.yml` does not manage CPA.
- Gateway recovery stops reading `CPA_API_KEY` from the `apisix-models` environment. Its CPA probe uses model-catalog's `CPA_CLIENT_KEY`.
- Unchanged:
  - Sub2API passthrough stays OFF;
  - Sub2API accounts keep `base_url=http://cli-proxy-api:8317/v1`;
  - no Sub2API, CPA source, AISIX source or keeper change.
- Production rollout is one reviewed manual window, with backups, rollback and full post-checks.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cpa-first-catalog-routing`: backend priority is resolved inside AISIX through a single `*` fallback to CPA. It replaces the slash rule and the gateway classification seam.
- `apisix-cpa-gateway`: the entrance becomes a constant two-route pipe with no CPA upstream and no network path to CPA.
- `aisix-model-routing`: the `*` fallback row is guarded by validation. AISIX memory admits one 64 MiB Responses body, and the concurrency limit of that guarantee is stated.

## Impact

- Code:
  - `services/routing/apisix-models/` (`apisix.yaml`, `config.yaml`, `start.sh`, delete `routing_selector.lua` and its test, README);
  - `compose.yaml`, `.env.example`;
  - `services/routing/aisix/resources.example.yaml`;
  - `ops/scripts/check-aisix-resource-policy.py`;
  - `ops/scripts/validate.sh` and `ops/scripts/tests/*` (catalog routes, standard cache topology, gateway recovery);
  - `ops/scripts/host/gateway-recovery.py`.
- Private config, never committed: AISIX `resources.yaml` (`*` row), private Compose/.env (AISIX limits and pin), CPA `config.yaml` (`requests.payload.filter`).
- Ansible:
  - `apisix-models` stays a component, and no removed service appears in any component or protected-identity list;
  - verify and test the lists;
  - Compose structure changes run in the manual window, outside the image/env component gate.
- Docs:
  - `README.md` row (short);
  - `docs/model-routing.md` rewritten;
  - architecture, security, layout, health/recovery and WebSocket docs reconciled;
  - AISIX and `apisix-models` READMEs;
  - a deployment record.
- Client-visible changes:
  - an ID unknown to both backends now returns CPA's error wrapped by AISIX (400 `upstream_error`), not AISIX's 404 or CPA's native 400;
  - AISIX Prometheus series for CPA-only traffic carry `model="*"`; the access log keeps the requested ID.
