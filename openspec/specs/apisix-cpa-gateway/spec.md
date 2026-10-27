# APISIX CPA Gateway Specification

## Purpose

A new dedicated APISIX instance inserted into the Sub2API → CPA path. It routes classified HTTP inference by the slash rule (ID with `/` → CPA, otherwise → AISIX), passes direct WebSocket upgrades to CPA, and splits Codex models-manifest requests to the models-enricher sidecar. The existing public-ingress APISIX instance is untouched.

## Requirements

### Requirement: Codex manifest route split

Requests to `GET /v1/models` SHALL be split by the `client_version` query parameter:
- With a non-empty value, the request goes to the `model-catalog` service for the Codex manifest.
- With an absent or empty value, the request also goes to the `model-catalog` service for the standard OpenAI `data[]`/`id` projection.

No request on this path is proxied unchanged to CPA. `apisix-models` SHALL route only `/v1/models` to `model-catalog`. It SHALL NOT route `/models-table` or `/models-table/refresh`.

#### Scenario: client_version present

- **WHEN** a request arrives for `GET /v1/models?client_version=1.0.0`
- **THEN** it is routed to `model-catalog` and answered with the Codex manifest

#### Scenario: client_version absent or empty

- **WHEN** a request arrives for `GET /v1/models` without `client_version` or with an empty value
- **THEN** it is routed to `model-catalog` and answered with the standard OpenAI `data[]`/`id` projection, not proxied to CPA

#### Scenario: Models table is not routed through apisix-models

- **WHEN** a request for `/models-table` or `/models-table/refresh` reaches `apisix-models`
- **THEN** it is not forwarded to `model-catalog`

### Requirement: Sub2API-facing compatibility

The listener SHALL accept the exact requests Sub2API already sends to CPA (plain OpenAI/Anthropic-style calls with `Authorization` header), requiring no Sub2API changes beyond the upstream address.

#### Scenario: No new client headers

- **WHEN** Sub2API sends any request on this hop
- **THEN** it succeeds without any APISIX-specific headers from Sub2API

### Requirement: Deployment isolation conventions

The changed data path SHALL follow existing ai-gateway conventions: digest-pinned images, one-way connection initiation per directed edge via dedicated relay pairs on two-member internal networks, and no wildcard host port binding.

#### Scenario: Listener reachability

- **WHEN** the stack is up
- **THEN** the new instance is reachable only from the Sub2API-side relay network, with no `0.0.0.0` host binding

### Requirement: Future model routing headroom

The design SHALL keep APISIX-native routing capabilities (ai-proxy-multi fallback strategies, upstream chash/roundrobin balancing, sticky hashing, health checks) available for later inference-traffic routing work without restructuring this insertion.

#### Scenario: Routing features remain available

- **WHEN** future work adds model routing
- **THEN** it can use built-in `fallback_strategy`, `max_retries`, upstream `type`/`hash_on`, and health-check configuration on this dedicated instance

### Requirement: Slash-rule passthrough to CPA or AISIX

The internal instance SHALL forward classified model-bearing JSON HTTP inference requests (at minimum `POST /v1/responses` and `POST /v1/chat/completions`, including front-bridge WS turns forwarded through Sub2API as HTTP) by the model ID slash rule: an ID containing `/` goes to the CPA upstream with the CPA service credential, and every other ID goes to the AISIX upstream with the caller credential. Request semantics, including SSE streaming, SHALL be preserved. Classification SHALL happen before inference with no cross-backend retry. Malformed classified payloads SHALL fail explicitly without forwarding. Requests outside the classified scope, including direct WebSocket upgrades and endpoints without a model-bearing JSON body, keep forwarding to CPA. Raw source discovery traffic never traverses this entrance; client catalog delivery through the entrance is retained.

#### Scenario: Slash ID chat completion reaches CPA
- **WHEN** Sub2API sends `POST /v1/chat/completions` naming an ID that contains `/`
- **THEN** the request reaches CPA and CPA's response body and status are returned unchanged

#### Scenario: Bare ID reaches AISIX
- **WHEN** a classified request names an ID without `/`
- **THEN** it is forwarded to the AISIX upstream once, with no CPA attempt

#### Scenario: SSE streaming passes through
- **WHEN** Sub2API sends a streaming `POST /v1/responses` request
- **THEN** SSE chunks flow from the selected backend to Sub2API incrementally without full-response buffering

#### Scenario: Direct WebSocket passes through to CPA
- **WHEN** a client opens a direct WebSocket upgrade on `/v1/responses`
- **THEN** the upgrade and frames pass through to CPA without classification
- **AND** bare IDs on this direct-inner path are not supported; public WS turns SHALL instead terminate at keeper → stateless responses-ws-sse-bridge before reaching HTTP-only Sub2API, whose API-key accounts use WS mode `off`
- **AND** keeper SHALL retain `IGNORE_CLIENT_STORE_FALSE=true`; Sub2API SHALL NOT move in front of keeper/bridge or use `passthrough`/`ctx_pool` for this chain, as explained in `docs/websocket-termination.md`

#### Scenario: Chosen backend failure does not re-route
- **WHEN** the selected backend returns 429/5xx, times out, or errors mid-stream
- **THEN** the failure surfaces without any attempt on the other backend

#### Scenario: Malformed classified payload does not fall through
- **WHEN** a classified path receives a malformed body or one without an extractable model ID
- **THEN** the entrance returns an explicit protocol-shaped error without forwarding to any backend

### Requirement: Front-door catalog leg reaches apisix-models directly

The front-door APISIX SHALL obtain the full catalog for the public `/v1/models` intersection directly from `apisix-models`, with no intermediate catalog proxy. It SHALL use only `GET /v1/models` on that edge, and it SHALL NOT route any public inference or other request to `apisix-models`.

#### Scenario: Public model list is intersected

- **WHEN** a client with a valid key requests `GET /v1/models` through the public front door
- **THEN** the front door intersects the key's Sub2API entitlement list with the catalog it reads from `apisix-models` and returns the intersection

#### Scenario: Public inference does not reach apisix-models from the front door

- **WHEN** a public inference request arrives at the front door
- **THEN** it follows the Sub2API path and is never sent to `apisix-models` directly
