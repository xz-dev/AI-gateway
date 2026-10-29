# CPA-first Catalog Routing Specification

## Purpose

Resolve model IDs inside AISIX by exact match, most-specific glob and one `*` fallback to CPA, with no catalog lookup or entrance retry. Preserve client authorization, protocol behavior and the front WebSocket bridge. The capability name is retained for history; see `docs/model-routing.md`.

## Requirements

### Requirement: One backend per request with no cross-backend retry

The entrance SHALL select exactly one backend per request. A selected backend's 429, 5xx, timeout, or stream error SHALL NOT trigger retry against or re-routing to the other backend; the failure surfaces to the client through the existing error-propagation path with its provider error behavior preserved. This constraint is scoped to the new entrance selection: existing Sub2API account pool retries and AISIX internal routing budgets remain applicable within their own boundaries.

#### Scenario: Rate limit on the chosen backend
- **WHEN** the chosen backend returns 429
- **THEN** the client receives that failure without any attempt on the other backend

#### Scenario: Stream error after forwarding
- **WHEN** the chosen backend's stream disconnects or errors after response commitment
- **THEN** the failure surfaces per the existing stream error behavior
- **AND** the other backend is never invoked for the same request

### Requirement: Preserved behavior outside the routing seam

Existing streaming delivery, request and response field fidelity including model IDs, provider error behavior, Sub2API account and group authorization, and non-chat model filtering SHALL be preserved. AISIX re-serializes JSON, so fields SHALL be preserved but bodies are not promised to be byte-identical. Catalog listing visibility SHALL remain constrained by existing admission and authorization rules; identical counts across listing surfaces are not promised, and catalog listing is not proof of invocation eligibility.

#### Scenario: Streaming and body fidelity retained
- **WHEN** a request is forwarded through AISIX, including through `*`
- **THEN** every request field, the model ID and streaming semantics are delivered as before, apart from upstream selection

#### Scenario: Unauthorized model stays invisible
- **WHEN** a client lacks Sub2API entitlement for a model ID that exists in a raw membership
- **THEN** existing authorization behavior applies and the catalog-visible listing remains admission-constrained

### Requirement: AISIX resolves backend priority with one `*` fallback to CPA

Every model ID SHALL first resolve in AISIX by its native order: an exact logical route or direct model, then the most specific single-`*` glob row, then one direct model named `*` that targets the CPA pool with `model_name: "*"`. A `*` match SHALL forward the requested ID to CPA unchanged. The gateway SHALL NOT consult a catalog, routing index, membership snapshot or naming rule. AISIX resources SHALL contain exactly one `*` row with that shape and the shared direct-model cooldown block, no routing target SHALL name `*`, and `*` SHALL NOT appear in AISIX `/v1/models` or catalog membership. Adding a backend SHALL mean adding AISIX rows, with no entrance change.

#### Scenario: AISIX-defined ID wins
- **WHEN** a request names `claude-opus-5-5` (logical route) or `codex/gpt-6-luna` (direct row)
- **THEN** AISIX serves it through that exact row, not `*`

#### Scenario: More specific glob wins
- **WHEN** AISIX defines `devin/*` and a request names `devin/swe-2`
- **THEN** the `devin/*` row serves it, not `*`

#### Scenario: CPA-only ID reaches CPA unchanged
- **WHEN** a request names an ID AISIX does not define, bare or containing `/`
- **THEN** AISIX forwards it through `*` to CPA with the same ID and its configured CPA credential

#### Scenario: Unknown everywhere
- **WHEN** neither AISIX nor CPA knows the ID
- **THEN** the client receives CPA's model-not-found cause wrapped by AISIX as a 400 `upstream_error`, with no retry against another backend

#### Scenario: Fallback row cannot silently disappear
- **WHEN** resources lack the `*` row, contain two, give it another `model_name` or provider key, or route a target to `*`
- **THEN** local validation and the Ansible AISIX deploy fail before activation

#### Scenario: Wildcard stays out of the catalog
- **WHEN** model-catalog reads AISIX `/v1/models`
- **THEN** the list contains no `*` row and catalog bodies are unchanged by the fallback

### Requirement: Client WebSocket terminates before Sub2API

The public chain SHALL be public APISIX → keepalive → keeper → responses-ws-sse-bridge → Sub2API (HTTP/SSE only) → inner `apisix-models` → AISIX (→ CPA through `*`). Keeper SHALL remain a pure single-upstream cache with `IGNORE_CLIENT_STORE_FALSE=true`: forward and record a turn without a parent; expand a cached parent and remove `previous_response_id` before forwarding and recording; forward an unknown-parent request unchanged without recording that chain. The bridge SHALL be stateless and convert WS turns to POST/SSE without history storage or retries. Sub2API SHALL NOT be placed in front of keeper or the bridge. Reasons and native-WS tradeoffs are recorded in `docs/websocket-termination.md`.

After the front bridge is live, every Sub2API OpenAI API-key account SHALL set `extra.openai_apikey_responses_websockets_v2_mode` to `off`, not `http_bridge`, `ctx_pool` or `passthrough`. Accounts using the unified entrance SHALL keep `credentials.base_url=http://cli-proxy-api:8317/v1`; third-party upstream accounts SHALL retain their own URLs. The retired `/rk/v1` base URL SHALL NOT be used. This requirement SHALL be met without modifying Sub2API code for gateway requirements.

#### Scenario: Bare ID over client WebSocket
- **WHEN** a client sends a Responses WS turn naming a bare ID through the public chain
- **THEN** keeper forwards or expands it to the stateless bridge, which sends HTTP to Sub2API
- **AND** Sub2API sends `POST /v1/responses` to the entrance and the request reaches AISIX

#### Scenario: Cached continuation after reconnect
- **WHEN** a client reconnects and sends a cached parent plus new input only
- **THEN** keeper expands the full history and removes the parent before the bridge sends the HTTP turn to Sub2API

#### Scenario: Account left on a WebSocket mode or /rk
- **WHEN** an account uses `http_bridge`, `ctx_pool`, `passthrough` or a `/rk/v1` base URL after this cutover
- **THEN** this violates the HTTP-only account contract, and `/rk` requests receive 404
- **AND** the front bridge SHALL NOT be bypassed to accommodate that configuration

### Requirement: CPA runs an official image with default model-prefix behavior

CPA SHALL run an official upstream release image, pinned by digest, without gateway-specific source patches. Its `routing.force-model-prefix` setting SHALL be `false`, the upstream default.

Gateway routing and catalog membership SHALL NOT depend on that setting:
- AISIX exact rows and routes take priority, and every other ID reaches CPA unchanged through `*`;
- AISIX targets that use the CPA pool SHALL name CPA IDs that carry a CPA prefix or a CPA-registered ID containing `/`.

#### Scenario: Prefixed ID reaches only its prefix's credentials

- **WHEN** a client requests `xl/gpt-6.1-sol`
- **THEN** AISIX forwards it to CPA unchanged, and CPA serves it only with the credential whose prefix is `xl`

#### Scenario: Bare ID still routes to AISIX

- **WHEN** a client requests `gpt-6.1-sol` and CPA also registers a bare `gpt-6.1-sol`
- **THEN** the AISIX logical route serves it, not the CPA bare copy

#### Scenario: Unprefixed slash ID served by its registering credential

- **WHEN** a client requests `devin/claude-opus-5-5`, which only the Devin account registers and AISIX does not define
- **THEN** AISIX forwards it through `*` and CPA serves it with that account

#### Scenario: AISIX target no longer registered by CPA

- **WHEN** a CPA image change stops registering an unprefixed slash ID that an AISIX direct model names, such as `devin/deepseek-v4-1-flash` in v8.0.45
- **THEN** that direct model and every logical-route target naming it are removed from the AISIX resources in the same deployment window
- **AND** every AISIX target that CPA registered at pre-capture remains registered, except accepted upstream removals removed from AISIX in that window; no new unregistered target appears
- **AND** pre-existing targets absent from CPA registration are recorded separately, not deleted as part of this change

#### Scenario: Setting drift is visible

- **WHEN** an operator inspects CPA's `force-model-prefix` setting after deployment
- **THEN** it reads `false` through the Management API and in the persisted CPA configuration
