## Why

model-catalog now emits every catalog list in one fixed order (union-catalog-membership D7). The plain public `GET /v1/models` (and `/models`) is still answered by Sub2API through the front door and keeps Sub2API's own order. The user wants that list to follow the same order: bare IDs first, then the others, each in Unicode code-point order.

## What Changes

- The front-door APISIX answers the plain model list (route `ai-api-models-get`: `GET /models`, and `GET /v1/models` without a non-empty `client_version`) by fetching Sub2API's response itself and returning it in the fixed order. The order is:
  1. IDs without `/`, in code-point order;
  2. then every other ID, in code-point order.

  Comparison is case-sensitive, with no locale.
- The membership, the record content (byte-exact JSON fragments) and the envelope stay as Sub2API returned them; only the array order changes.
- Non-2xx Sub2API responses pass through unchanged, as they do today. A 2xx body the front door cannot recognize as one model collection with unique string identities is passed through unchanged with a WARN, never rejected.
- A reordered 200 gets the same response policy as the parameterized intersection:
  - `Cache-Control: private, no-cache`;
  - a strong ETag of the final body, with `If-None-Match` → 304 and `If-Match` → 412;
  - `Vary` including `Authorization`.

  Sub2API's own ETag and Cache-Control are dropped, because they describe the unordered body.
- The parameterized Codex manifest (`client_version` set) is unchanged. It already keeps model-catalog's order through the intersection.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `apisix-cpa-gateway`: adds the requirement that the front door's plain public model list uses the catalog output order.

## Impact

- Code: `services/firewall/apisix/lua/model_list_intersection.lua` (new `_M.list` plain-list handler and ordering; `_M.admit` unchanged) and its test `model_list_intersection_test.lua`. `services/firewall/apisix/apisix.yaml`: route `ai-api-models-get` calls `_M.list` (design D2).
- Docs: `docs/catalog.md` (front-door notes) and the front APISIX README if it describes the plain route.
- Production: one reviewed manual window that uploads the two tracked front-door files and recreates the public front APISIX once, at a low-traffic point (design D5). The recreate cuts in-flight public streams; the user accepted this. No image, Compose, `.env` or private configuration change. The existing `deploy.yml -e component=apisix --check --diff` must stay `changed=0` before and after.
- Clients: same members and records; only the order and the cache headers of the plain list change.
