# APISIX front door

**Where:** Compose service `apisix` (Apache APISIX, standalone YAML) in the `apisix-netns` namespace. Cloudflare Tunnel reaches it through `cloudflared-apisix-relay`; the host reaches `127.0.0.1:9080`, and the operator listener `9083` on `127.0.0.1` and Tailscale, through `apisix-host-netns`.

**What:** the gateway's firewall for client traffic.

- Allows only the supported public AI routes, applies rate and body limits, and answers everything else with an opaque `404`.
- Answers versioned `GET /v1/models` itself by intersecting Sub2API's entitled list with the catalog from `apisix-models` (`lua/model_list_intersection.lua`).
- Fetches plain `/v1/models` and `/models` from Sub2API and returns the same records in bare-ID-first, case-sensitive Unicode code-point order, with private final-entity cache validators. Unrecognized bodies and errors pass through; all model lists share one admission slot.
- Serves `/models-table` and `/models-table/refresh` from `model-catalog` on port `9083` only.

**How:**

- Routes and upstreams: [`apisix.yaml`](apisix.yaml); listeners: [`config.yaml`](config.yaml); custom Lua: [`lua/`](lua/). Lua tests run from [`ops/scripts/validate.sh`](../../../ops/scripts/validate.sh).
- Diagnose the catalog: `http://127.0.0.1:9083/models-table`.
- Details: [public API behavior](../../../docs/public-api.md), [model catalog path](../../../docs/catalog.md).
