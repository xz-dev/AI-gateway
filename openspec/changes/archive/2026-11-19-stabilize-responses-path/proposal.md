> **SUPERSEDED — 2026-10-23.** Closed as an implementation plan, not completed acceptance.
> See [SUPERSEDED.md](SUPERSEDED.md). The historical inner `/rk` keeper,
> keeper-side routing and last-known-good deltas MUST NOT be synced or deployed.
> Current authority: [front WS decision](../../../docs/websocket-termination.md)
> and the main routing/cache specs. Historical evidence below is preserved.

## Why

Since response-keeper went live, Responses traffic has kept failing with `Connection error`, `503 Model routing is temporarily unavailable` and frequent `WS/full` turns. Production evidence (2026-10-19, read-only):

- **The 503 has no fallback.** Its 127-byte body comes from `apisix-models/routing_selector.lua`. Every `POST /v1/responses` makes one `/routing-index` lookup to models-enricher (300 ms connect, 1 s read). On any timeout the request gets 503. There were 92 such 503s in 24 h, each taking 0.5–3.7 s. The same lookup answers in milliseconds when probed directly.
- **The host is thrashing.** It has 2 vCPU and about 2 GiB RAM. Load is 11–12 with about 24 % steal and about 46 % sys time. About 2.1 GiB of swap is in use and kswapd runs continuously. models-enricher has 5 MiB resident and 110 MiB swapped out. When its pages are swapped out, a lookup misses the 1 s budget.
- **Keeper adds a second gateway hop with the largest body.** Today the path is `Sub2API → apisix-models /rk → keeper → apisix-models :9080 selector → CPA/AISIX`. On that second hop, apisix reads the keeper-expanded full history (up to 64 MiB) into Lua and JSON-decodes it, in a 128 MiB, single-worker container with `client_max_body_size 16m`. The intended design never had this second hop: keeper sits directly in front of the backends.
- **Most `WS/full` turns follow errors.** The Pi extension sends `previous_response_id` only within one live WebSocket. Every error or idle close drops that state, so the next socket resends full history, even though keeper still holds the stored response.

## What Changes

- **A — Bounded last-known-good routing.** A routing-index transport failure (timeout, connection error, malformed body) reuses the last authoritative `cpa`/`aisix` decision for the same exact model ID, within a bounded age. This replaces the immediate 503. Authoritative `not_found`/`unavailable` answers are never overridden, and a cold start with no prior decision still returns 503. The rule applies to the apisix selector (chat/completions, images/generations) and to keeper's Responses routing.
- **B — Keeper is the single Responses hop in front of the backends.**
  - All Responses traffic terminates at keeper: HTTP `POST` and WebSocket upgrades, on both `/v1/responses` and the compatibility alias `/rk/v1/responses`.
  - Keeper classifies each turn through the routing-index contract and forwards it directly to CPA or AISIX. The `self` loop back into apisix :9080 is removed for Responses.
  - apisix-models proxies Responses routes to keeper without reading or decoding the body.
  - **BREAKING (internal topology only):** the apisix selector no longer classifies `/v1/responses`; keeper does.
- **D — Cross-connection continuation in the Pi extension.** A new WebSocket in the same session continues from the last completed, stored response with `previous_response_id` plus delta input, instead of full history. On `previous_response_not_found` the extension retries exactly once with full history, using the existing repair path.

## Capabilities

### New Capabilities
- `responses-keeper-path`: single-hop Responses topology. Keeper is the only Responses ingress in front of CPA/AISIX, with keeper-side classification, credential and owner-scope handling, body limits, and WebSocket per-turn routing.
- `ws-cross-connection-continuation`: client-side continuation across WebSocket reconnects, including bounded fallback to full history.

### Modified Capabilities
- `cpa-first-catalog-routing`: the routing-index lookup gains the bounded last-known-good fallback for transport failures. The classification seam moves Responses (HTTP and WebSocket turns) from the apisix selector to keeper. The no-entrance-cache clause is replaced by the bounded fallback rule.

## Impact

- **Code:**
  - `apisix-models/routing_selector.lua` and its tests; `apisix-models/apisix.yaml` (Responses routes, header handling, body limits); `apisix-models/config.yaml` (shared dict).
  - response-keeper (submodule `middleware/response-keeper`, canonical `/home/xz/Code/ai/response-keeper`): routing client, upstream map, header contract, `prompt_cache_retention` stripping for WebSocket frames.
  - Pi extension `xz-dev/openai-api-pi-extension` (`responses-websocket-fetch.ts`).
- **Config/deploy:**
  - `compose.yaml` keeper service: upstream/routing environment, named cache volume `STORE_DIR`, `GOMEMLIMIT`.
  - A scoped Ansible dispatch that recreates only `response-keeper` and `apisix-models`, never Sub2API or other services.
- **Unchanged:** Sub2API account `base_url` (`/rk/v1` keeps working as an alias), CPA, AISIX internals, models-enricher, chat/completions routing semantics apart from fallback A, and the public opacity of error bodies.
- **Ordering:** implemented immediately after the keeper streaming-memory core (PLAN/keeper-oom.md) is accepted and deployed. B ships together with that keeper image line.
