# apisix-models

**Where:** Compose service `apisix-models` (Apache APISIX, standalone YAML) in the `apisix-models-netns` namespace, with no host port. Sub2API reaches it through `sub2api-cpa-relay`; the front door reaches it over `front-catalog` for the catalog leg.

**What:** a constant pipe that never reads request bodies.

- `GET /v1/models` goes to `model-catalog` (Codex manifest with a `client_version`, standard list without one).
- Everything else goes to AISIX with the caller credential. AISIX resolves each model by priority and falls back through its single `*` row.
- `GET`/`HEAD /healthz` is answered locally with 200.

**How:**

- Routes: [`apisix.yaml`](apisix.yaml); listeners: [`config.yaml`](config.yaml); [`start.sh`](start.sh) installs the route file at container start.
- Tests: `ops/scripts/tests/test-catalog-routes.sh`, run from [`ops/scripts/validate.sh`](../../../ops/scripts/validate.sh).
- Rules and deploy checklist: [model routing](../../../docs/model-routing.md).
