# Model routing and front WebSocket bridge

Read this before you change `services/routing/apisix-models/`, the AISIX `*` row, Sub2API account settings, or the keeper chain.

> Deployed 2026-10-23, keeper cutover 02:07:35 UTC; correlated acceptance completed afterwards. Keeper and the ws→sse bridge end WebSocket before Sub2API; Sub2API sees only HTTP/SSE. Read [websocket-termination.md](websocket-termination.md) for the decision, tradeoffs and validation limits.

## The rule

`apisix-models` never reads a request body. `GET /v1/models` goes to model-catalog, `/healthz` is answered locally, and everything else goes to AISIX with the caller key unchanged. AISIX picks the backend by priority:

| Requested ID matches | Served by | Example |
| --- | --- | --- |
| an exact AISIX logical route or direct row | that route or row | `claude-opus-5-5` → `xl/claude-opus-5-5`; `codex/gpt-6-luna` |
| a narrower AISIX glob row (longest literal wins) | that glob row | `devin/*` |
| nothing else | the single `*` row → CPA, ID unchanged | a CPA-only bare or slash ID |

- AISIX implements the precedence itself (`resolve_model`: exact names first, then the most specific wildcard). `ops/scripts/check-aisix-resource-policy.py` requires exactly one `*` direct row (`model_name: "*"`, `provider_key: cpa-pool`, shared cooldown) and rejects any route that targets it.
- No catalog lookup, membership snapshot or cache sits on the request path. Unknown IDs are forwarded, and the backend returns the error.
- `apisix-models` has no CPA upstream, credential or network path; `validate.sh` walks its networks and relays and fails if CPA is reachable.

Do not reintroduce `/routing-index` lookups, last-known-good caches, a "registered provider" check or body classification in `apisix-models`.

CPA runs with its default `force-model-prefix: false`. Each model of a prefixed credential is registered twice, as `x` and `P/x`:

- `P/x` is served only by the credential whose prefix is `P`;
- a bare `x` that AISIX does not define reaches CPA through `*` and is served by any credential registering it;
- an unprefixed slash ID (`devin/<model>`, or an upstream original such as `deepseek/…`) reaches CPA unchanged and is served by the credential that registers it.

The catalog lists only the IDs in model-catalog's [membership rule](model-catalog.md#membership). AISIX's `/v1/models` never lists `*`, so the fallback row adds no catalog member. Sub2API still enforces each group's model allowlist before a request reaches this hop.

### Behaviour through `*`

- **Unknown everywhere.** Responses returns 400 `{"error":{"message":"<CPA error JSON as a string>","type":"upstream_error"}}`; Chat returns 400 with `type: upstream_error`, `code: model_not_found` and `unknown provider for model X`.
- **Attribution.** The AISIX access log keeps the requested ID; Prometheus series for fallback traffic carry `model="*"`. The status page may show `*` cooling after an unknown-ID 400 or a 429. That is cosmetic: cooldown filters only routing-group targets.
- **Body fidelity.** AISIX re-serializes JSON. Fields are preserved, but bodies are not byte-identical (Chat gains `stream:false`).
- **Retention.** No gateway layer strips `prompt_cache_retention`. CPA's `requests.payload.filter` removes it for the upstreams that reject it (initially Kimi).
- **Memory.** AISIX holds about 4.6–7× a request body; one 64 MiB Responses body peaks near 460 MB. AISIX runs with `mem_limit: 512m` and no swap. Two concurrent 64 MiB bodies can still OOM it, after which Docker restarts AISIX and every in-flight AISIX request fails. Observed bodies are far smaller (max about 4 MB).

## Topology

```text
Pi ─WS/HTTP─► public APISIX ─► keepalive ─► openai-api-cache-keeper
   ─WS/HTTP─► responses-ws-sse-bridge ─HTTP/SSE─► Sub2API (account WS mode off)
Sub2API account base_url http://cli-proxy-api:8317/v1
   └─► sub2api-cpa-relay ─► apisix-models :9080
          GET /v1/models ─► model-catalog
          everything else ─► AISIX ─► exact | glob | * ─► aisix-cpa-relay ─► CPA
```

`cli-proxy-api:8317` on the Sub2API network is the relay to `apisix-models`, not CPA itself. The front door reads the same `/v1/models` over `front-catalog`.

`apisix-models` publishes no host port. To read the internal catalog on the host:

```sh
docker exec ai-gateway-apisix-models-1 bash -c \
  'exec 3<>/dev/tcp/127.0.0.1/9080; printf "GET /v1/models HTTP/1.0\r\nHost: localhost\r\n\r\n" >&3; cat <&3'
```

## Why WebSocket must be bridged

APISIX passes WebSocket frames through without inspection, and AISIX has no `GET /v1/responses` WebSocket endpoint. A direct WebSocket upgrade therefore fails at AISIX. Our stateless `responses-ws-sse-bridge` turns each client WebSocket turn into `POST /v1/responses` before Sub2API. Keeper runs before that bridge with `UPSTREAM_URL=http://127.0.0.1:8086` and `IGNORE_CLIENT_STORE_FALSE=true`; known parents are expanded there. Do not restore Sub2API `http_bridge`, `ctx_pool` or `passthrough` for this chain.

## Sub2API account contract

Every OpenAI API-key account in Sub2API:

| Field | Required value |
| --- | --- |
| `credentials.base_url` | `http://cli-proxy-api:8317/v1` (third-party upstreams such as SiliconFlow keep their own URL) |
| `extra.openai_apikey_responses_websockets_v2_mode` | `off` (only after the front bridge is live) |
| API key | private `sub2api` caller key from `services/routing/aisix/caller-keys.json` (CPA-backed accounts) |

Never use `/rk/v1`; that alias was removed with the old keeper and now returns 404. Never set `ctx_pool` or `passthrough` on these accounts.

Read-only check on the production host:

```sh
docker exec -i ai-gateway-postgres-1 sh -c \
 'exec psql -XAtq -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
BEGIN READ ONLY;
SELECT id, name, credentials->>'base_url', extra->>'openai_apikey_responses_websockets_v2_mode'
  FROM accounts WHERE deleted_at IS NULL AND platform='openai' AND type='apikey' ORDER BY id;
ROLLBACK;
SQL
```

Every row must show `off`. No row may contain `/rk/`. Change accounts only through the Sub2API admin API, never by direct SQL writes. On 2026-10-23 accounts 1,3,4,5,6,9,10,11,12 changed only this WS mode field; credentials and base URLs stayed unchanged. To roll back, restore their prior `http_bridge` mode before reverting keeper to its old image/upstream.

## Architecture boundary

Do not implement gateway requirements by modifying Sub2API code. Routing, aggregation and WebSocket handling belong to APISIX, AISIX and keeper layers, or to Sub2API's existing configuration. Patch Sub2API only for a genuine Sub2API bug that reproduces with Sub2API alone, with a minimal fix and explicit operator approval.

Sub2API's built-in bridge loses earlier assistant output on parent-plus-delta turns and silently drops unresolved parents. Front termination avoids that path without a Sub2API patch: keeper expands known parents before the HTTP request reaches Sub2API. Unknown WS parents receive the bridge's standard `previous_response_not_found`; unknown HTTP parents still receive Sub2API's non-standard owner-check 400.

## Incident 2026-10-22: how this broke

1. Before 08:53 UTC the inner APISIX sent all `/v1/responses` traffic (GET and POST) to the old `response-keeper`, which read the model of every WebSocket turn and chose CPA or AISIX.
2. Commit `b112be3` moved the cache keeper in front of Sub2API and stopped the old keeper. The replacement inner route was POST-only. The keeper's WebSocket classification was dropped with no replacement.
3. Sub2API accounts still used `ctx_pool`, which forwards the WebSocket. Upgrades fell to `catch-all` and reached CPA. Every bare aggregate ID failed over WebSocket.
4. Seven accounts still had `base_url …/rk/v1`. The cutover removed `/rk`, so those accounts returned 404 for everything.
5. Repair, 14:29–14:30 UTC, no container restart: the slash-rule selector, `http_bridge` on all accounts, and `/rk/v1 → /v1`.

## Checklist before pushing or deploying

- [ ] Changing a keeper, route or AISIX resource? List every responsibility of the component you remove (HTTP *and* WebSocket) and show where each one lives afterwards.
- [ ] Removing a path alias (`/rk`, …)? Find every Sub2API account and client still using it, and migrate them in the same rollout.
- [ ] Sub2API account settings are database state; Git does not track them. Run the read-only check above before and after each cutover.
- [ ] `ops/scripts/validate.sh` passes, including the AISIX resource policy (exactly one `*` row), the apisix-models CPA-cut check and `test-catalog-routes.sh`.
- [ ] Production `services/routing/apisix-models/apisix.yaml` and `config.yaml` hashes equal the repository files before you deploy the repository. A deploy from a stale checkout silently reverts the live routes.
- [ ] After deploy, test with the installed Pi over WebSocket: one AISIX route (`claude-opus-5-5`), one AISIX slash row (`xl/claude-opus-5-5`), one CPA-only ID through `*`, and a two-turn recall. Confirm in the AISIX access log that each requested ID arrived with the expected upstream model. HTTP 101 or a single `OK` alone is not proof.

## Known production drift (2026-10-22)

- The temporary `responses-websocket-auth` route (added 11:05 UTC) was removed from production at 14:46 UTC. Production `services/routing/apisix-models/apisix.yaml` again equals this repository.
- The unused `routing_lkg` shared dict was removed from `config.yaml` (repository and production file) at 14:5x UTC. nginx reads it only at startup, so the running `apisix-models` keeps the 1 MiB dict until its next restart; no restart was done for this.
- `openspec/changes/stabilize-responses-path` describes the retired keeper topology and catalog lookup. Do not archive it as-is.
