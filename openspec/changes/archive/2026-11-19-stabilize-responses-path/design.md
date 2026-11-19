> **SUPERSEDED — 2026-10-23.** Historical design only. “Current” and “today”
> below refer to the original investigation, not the deployed front keeper/bridge.
> See [SUPERSEDED.md](SUPERSEDED.md); do not restore `/rk` or keeper routing.

## Context

See proposal.md (Why) for the production evidence. Current topology, from `apisix-models/apisix.yaml` and `compose.yaml`:

```text
Sub2API ──socat──► apisix-models :9080
   /rk/v1/responses ──► keeper 127.0.0.1:8085 ──► 127.0.0.1:9080 (self) ──► routing_selector ──► cpa | aisix
   /v1/responses    ──► routing_selector ──► cpa | aisix          (non-/rk accounts; today none)
```

- keeper runs with `network_mode: service:apisix-models-netns`. It shares apisix's network namespace, so it already resolves `cpa` (extra_hosts → 172.30.35.3 relay), `models-enricher` (172.30.27.3 relay) and `aisix:3000` (relay `apisix-models-aisix-relay`, 172.30.70.3). No new networks or relays are needed for keeper itself.
- `routing_selector.run` does four things: reads and JSON-decodes the body (≤16 MiB), looks up `/routing-index`, strips `prompt_cache_retention`, and sets `Authorization: Bearer <CPA_API_KEY>` only for CPA. AISIX keeps the caller's credential.
- `catch-all` also strips `prompt_cache_retention`, but APISIX's WebSocket proxy skips body plugins, so WebSocket frames are not stripped today.
- The keeper owner scope is `sha256(Authorization)` as received. Today keeper receives Sub2API's account key, before the selector substitutes the CPA key.
- The keeper streaming-memory core (PLAN/keeper-oom.md) lands first. This change builds on that keeper code line.

## Goals / Non-Goals

**Goals:**
- One gateway traversal per Responses turn. The expanded body goes only keeper → backend.
- No 503 for brief routing-index stalls while a recent authoritative decision exists.
- Cross-socket continuation in the Pi extension.
- Scoped rollout: only `response-keeper` and `apisix-models` are recreated.

**Non-Goals:**
- Host capacity planning and memory quota rebalancing (option C). They are tracked separately.
- Changing models-enricher, CPA, AISIX or Sub2API configuration.
- Changing chat/completions or images routing beyond fallback A.
- Persisting keeper history across restarts.
- Classifying non-Responses WebSocket traffic.

## Decisions

### D1. Keeper owns Responses classification (B)

Keeper already decodes the top-level request fields during its streaming scan. It adds a small routing client:

- `GET http://models-enricher:8090/routing-index?model=<id>`, with the same 300 ms / 300 ms / 1 s budget.
- The four-outcome parser and generation validation are identical to the Lua parser.
- An upstream map from configuration: `UPSTREAM_CPA_URL=http://cpa:8317`, `UPSTREAM_AISIX_URL=http://aisix:3000`, `ROUTING_INDEX_URL=http://models-enricher:8090/routing-index`. `UPSTREAM_URL` stays a single-upstream mode for tests and rollback.
- WebSocket: classification runs per `response.create` turn, inside keeper's existing per-turn preparation, before the upstream request.

*Alternatives considered:*
- (a) Keep the second hop but pass the model in a trusted header so apisix skips body parsing. This still costs a second apisix traversal and duplicates the route config. The operator rejected it: "keeper should sit in front of the backends".
- (b) Move keeper physically before apisix-models (Sub2API → keeper). That changes Sub2API wiring and relays, and apisix would still parse the expanded body.

### D2. apisix routes Responses to keeper without body access

- `/v1/responses` and `/rk/v1/responses` (GET+POST, `enable_websocket`) both map to upstream `response-keeper`, with `proxy-rewrite` to `/v1/responses`.
- Neither the selector nor any body-reading plugin runs on these routes.
- The `/rk/*` passthrough to `self` is kept only for non-Responses `/rk/...` paths. The `self` upstream is no longer used by Responses.
- The route-level body limit is raised to 32 MiB to match keeper. It is set per route via `client-control.max_body_size` if available in 3.19, otherwise in `nginx_config.http.client_max_body_size`. Because apisix forwards the body without reading it, no extra Lua memory is used.

### D3. Credentials and owner scope

- Keeper computes the owner from the incoming `Authorization` (Sub2API account key, unchanged).
- For CPA it replaces the header with `Bearer ${CPA_API_KEY}`, from the existing `.env` secret injected as keeper environment. For AISIX it forwards the original header.
- The owner key therefore stays per Sub2API account, as today, and backend credentials never reach logs.

*Alternative:* let apisix inject the CPA key before keeper. Rejected because it would collapse all owners onto one CPA key and break isolation.

### D4. Last-known-good routing fallback (A), in both classifiers

- Stored on a successful authoritative `cpa`/`aisix` answer: `model → {decision, at}`.
- Used only on a transport failure, within `max_age = 15 min`.
- Removed on `not_found` or `unavailable`.
- Bounded:
  - Lua: `lua_shared_dict routing_lkg 1m` via `nginx_config.http_configuration_snippet` (or `custom_lua_shared_dict`), with TTL set on `set`.
  - Go: a map capped at 4096 entries with oldest-first eviction, mutex protected.
- Every fallback use logs one warning line containing only the model ID and the decision age.
- The spec's old "no second entrance cache" clause is replaced. This is a failure fallback, not a hot-path cache: every request still performs its lookup first.

*Alternatives considered:*
- Raise the timeouts only. That converts 503s into multi-second stalls and does not help when the enricher is fully paged out.
- Cache before lookup. That changes routing freshness semantics and violates "one lookup per request".

### D5. `prompt_cache_retention` stripping moves into keeper

Keeper's streaming request writer already re-emits top-level fields individually, so dropping one key costs nothing. This applies to both HTTP and WebSocket, and fixes the current WebSocket gap.

### D6. Pi extension cross-socket anchor (D)

- Keep a module-level `anchors: Map<connectionKey, Continuation>`, written when a turn completes with `store: true` acknowledged (the same moment `connection.continuation` is set today).
- On `acquire()` of a fresh connection, seed `connection.continuation` from the anchor when the identity key matches.
- `cachedBody` then works unchanged.
- The existing invalid-reference path, `rejectedContinuationKeys` and the one-shot full-history repair already implement the fallback. Add `previous_response_not_found` for bridge-owned anchors to the qualifying signals; it is already handled for the durable path.
- Clear the anchor on cooldown, on a recovery probe, and on rejection.

### D7. Deployment

- New compose for keeper:
  - `volumes: [response-keeper-cache:/var/tmp/response-keeper]` (named volume)
  - `STORE_DIR=/var/tmp/response-keeper`, `GOMEMLIMIT` per keeper-oom acceptance, the routing and upstream environment, and `CPA_API_KEY`
  - `read_only: true`, `pids_limit: 64` and `mem_limit` unchanged
- Add a new scoped Ansible dispatch, `deploy-responses-path`, modeled on `deploy-ws-keepalive`:
  - plan diff → approved digest gate → pull before mutation → snapshot of the previous image, apisix.yaml, routing_selector.lua and compose service → `up -d --no-deps response-keeper apisix-models` → verify → automatic rescue on failure
- Do **not** use the legacy `deploy-response-keeper` tag, which recreates Sub2API.

## Risks / Trade-offs

- [Keeper becomes a routing component; a keeper bug now affects routing too] → The routing client is small and uses the same contract tests as Lua (shared fixture of index bodies). The fallback reduces enricher sensitivity.
- [The last-known-good decision could be stale if a model moved backend in the last 15 minutes and the enricher is down] → Bounded age, removal on authoritative answers, warning log. The wrong backend gives an explicit upstream error, never a silent success with wrong data.
- [Keeper outage now blocks all Responses (already true for /rk accounts)] → Health check, `restart: unless-stopped`, scoped rollback to recorded images.
- [AISIX path under keeper loses nothing but gains history storage] → Intended. Owner scope stays the caller credential.
- [A 32 MiB apisix limit on Responses routes] → Bodies are streamed to keeper, not buffered in Lua. Keeper spools to disk.
- [Pi extension anchors outlive server state (keeper restart, TTL 10 m)] → One-shot full-history repair on `previous_response_not_found`. Anchor age is capped at the keeper TTL (configurable, default 10 m).
- [Host thrashing remains] → This change removes the duplicated CPA/apisix work and the 503 cliff, but does not add RAM. Track option C separately and re-measure after rollout.

## Migration Plan

1. Land and deploy the keeper streaming-memory core (keeper-oom). This change waits for that image.
2. Implement A in Lua plus tests. Implement D1/D3/D4/D5 in keeper plus tests. Change the apisix routes. Implement D in the extension.
3. Local integration: compose subset (apisix-models, keeper, fake CPA, fake AISIX, fake enricher). Verify single traversal, owner isolation, fallback, WebSocket per-turn routing and 16–32 MiB bodies.
4. Production, after per-item operator approval: run `deploy-responses-path` in plan mode, then apply. Verify:
   - health checks pass;
   - access log has no `/v1/responses` with upstream `127.0.0.1:9080`;
   - the 503 rate is compared over 24 h;
   - other container IDs are unchanged.
5. Rollback: the dispatch rescue restores the recorded keeper image, apisix.yaml, routing_selector.lua and keeper compose block, recreating the same two services. The named volume is disposable and can stay.
6. The extension release ships independently. It is safe against the old topology because the fallback handles any miss.
