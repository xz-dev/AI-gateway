# apisix-models

**Where:** Compose service `apisix-models` (Apache APISIX, standalone YAML) in the `apisix-models-netns` namespace, published on `127.0.0.1:9081`. Sub2API reaches it through `sub2api-cpa-relay`; the front door reaches it over `front-catalog` for the catalog leg.

**What:** the inner model selector.

- Inference: `routing_selector.lua` sends model IDs containing `/` to CPA and all other IDs to AISIX.
- Catalog: every `GET /v1/models` goes to `model-catalog` (Codex manifest with a `client_version`, standard list without one). It has no models-table route.
- `GET /healthz` is a local route that returns 200 without contacting CPA.

**How:**

- Routes: [`apisix.yaml`](apisix.yaml); listeners: [`config.yaml`](config.yaml). [`start.sh`](start.sh) injects `CPA_API_KEY` into the route template at container start.
- Test: `routing_selector_test.lua` runs from [`ops/scripts/validate.sh`](../../../ops/scripts/validate.sh).
- Rules and deploy checklist: [model routing](../../../docs/model-routing.md).
