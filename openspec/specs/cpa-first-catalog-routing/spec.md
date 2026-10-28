# CPA-first Catalog Routing Specification

## Purpose

Select the inference backend for each model ID before inference by the literal slash rule: an ID containing `/` goes to CPA, every other ID goes to AISIX. No catalog lookup, one backend per request, no cross-backend retry. The capability name predates the 2026-10-22 rule change and is kept for history; see `docs/model-routing.md`.

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

Existing streaming delivery, request and response body fidelity including model IDs, provider error behavior, Sub2API account and group authorization, and non-chat model filtering SHALL be preserved. Catalog listing visibility SHALL remain constrained by existing admission and authorization rules; identical counts across listing surfaces are not promised, and catalog listing is not proof of invocation eligibility.

#### Scenario: Streaming and body fidelity retained
- **WHEN** a request is classified and forwarded to the chosen backend
- **THEN** the request body, model ID, and streaming semantics are delivered as before the change, apart from upstream selection

#### Scenario: Unauthorized model stays invisible
- **WHEN** a client lacks Sub2API entitlement for a model ID that exists in a raw membership
- **THEN** existing authorization behavior applies and the catalog-visible listing remains admission-constrained

### Requirement: Backend selection follows the model ID slash rule

The entrance SHALL select the backend from the literal top-level `model` string only: a model ID that contains the character `/` anywhere SHALL route to CPA; every other model ID SHALL route to AISIX. The entrance SHALL NOT consult a catalog, routing index, provider registry or cache, and SHALL NOT validate the text on either side of the slash. The model ID SHALL be forwarded unchanged. Whether the model exists is decided by the selected backend.

#### Scenario: Bare aggregate ID routes to AISIX
- **WHEN** a classified request names `claude-opus-5-5` or `gpt-6.1-sol`
- **THEN** it is forwarded to AISIX with the caller credential unchanged

#### Scenario: Unknown bare ID still routes to AISIX
- **WHEN** a classified request names a bare ID that no catalog lists
- **THEN** it is forwarded to AISIX, and any error comes from AISIX

#### Scenario: Any slash routes to CPA
- **WHEN** a classified request names `xl/claude-opus-5-5`, `unknown-provider/unknown-model`, `/model`, `provider/` or `/`
- **THEN** it is forwarded to CPA with the CPA service credential

#### Scenario: Missing CPA credential fails closed
- **WHEN** a slash ID is classified and no CPA service credential is configured
- **THEN** the entrance returns a 503-class error without forwarding

### Requirement: Client WebSocket terminates before Sub2API

The public chain SHALL be public APISIX → keepalive → keeper → responses-ws-sse-bridge → Sub2API (HTTP/SSE only) → inner `apisix-models` slash selector → AISIX/CPA. Keeper SHALL remain a pure single-upstream cache with `IGNORE_CLIENT_STORE_FALSE=true`: forward and record a turn without a parent; expand a cached parent and remove `previous_response_id` before forwarding and recording; forward an unknown-parent request unchanged without recording that chain. The bridge SHALL be stateless and convert WS turns to POST/SSE without history storage or retries. Sub2API SHALL NOT be placed in front of keeper or the bridge. Reasons and native-WS tradeoffs are recorded in `docs/websocket-termination.md`.

After the front bridge is live, every Sub2API OpenAI API-key account SHALL set `extra.openai_apikey_responses_websockets_v2_mode` to `off`, not `http_bridge`, `ctx_pool` or `passthrough`. Accounts using the unified CPA/AISIX entrance SHALL keep `credentials.base_url=http://cli-proxy-api:8317/v1`; third-party upstream accounts SHALL retain their own URLs. The retired `/rk/v1` base URL SHALL NOT be used. This requirement SHALL be met without modifying Sub2API code for gateway requirements.

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

### Requirement: Classification scope is the model-bearing HTTP seam

Classification SHALL apply only to the enumerated model-bearing JSON HTTP inference paths of the unified entrance: at minimum `POST /v1/responses` and `POST /v1/chat/completions`, including client WS turns converted by the front stateless bridge and forwarded as HTTP inference through Sub2API. The selector routes SHALL take precedence over the CPA-direct routes for those paths, so the original requested model ID reaches the selected backend untouched. A direct WebSocket upgrade to the entrance SHALL NOT be classified; it keeps forwarding to CPA and is therefore correct only for model IDs containing `/`. A request on a classified path whose JSON body is malformed or whose model field cannot be extracted SHALL fail explicitly with a protocol-shaped error, never silently fall through to a backend.

#### Scenario: Direct WebSocket stays outside the selector
- **WHEN** a client opens a direct WebSocket upgrade to the entrance
- **THEN** it is forwarded to CPA without model classification or frame inspection

#### Scenario: Malformed body on a classified path
- **WHEN** a classified path receives a body that is not valid JSON or lacks an extractable model ID
- **THEN** the entrance returns an explicit protocol-shaped error
- **AND** no backend receives the request

#### Scenario: Non-classified endpoint unchanged
- **WHEN** a request arrives on an endpoint outside the enumerated classification scope
- **THEN** it keeps its current matching and forwarding behavior, distinct from a failed classification

### Requirement: CPA runs an official image with default model-prefix behavior

CPA SHALL run an official upstream release image, pinned by digest, without gateway-specific source patches. Its `routing.force-model-prefix` setting SHALL be `false`, the upstream default.

Gateway routing and catalog membership SHALL NOT depend on that setting:
- the inner selector keeps sending IDs without `/` to AISIX and IDs with `/` to CPA unchanged;
- AISIX targets that use the CPA pool SHALL name CPA IDs that carry a CPA prefix or a CPA-registered ID containing `/`.

#### Scenario: Prefixed ID reaches only its prefix's credentials

- **WHEN** a client requests `xl/gpt-6.1-sol`
- **THEN** the selector forwards it to CPA unchanged, and CPA serves it only with the credential whose prefix is `xl`

#### Scenario: Bare ID still routes to AISIX

- **WHEN** a client requests `gpt-6.1-sol` and CPA also registers a bare `gpt-6.1-sol`
- **THEN** the selector forwards it to AISIX, not CPA

#### Scenario: Unprefixed slash ID served by its registering credential

- **WHEN** a client requests `devin/claude-opus-5-5`, which only the Devin account registers
- **THEN** CPA serves it with that account, as before the setting change

#### Scenario: AISIX target no longer registered by CPA

- **WHEN** a CPA image change stops registering an unprefixed slash ID that an AISIX direct model names, such as `devin/deepseek-v4-1-flash` in v8.0.45
- **THEN** that direct model and every logical-route target naming it are removed from the AISIX resources in the same deployment window
- **AND** every AISIX target that CPA registered at pre-capture remains registered, except accepted upstream removals removed from AISIX in that window; no new unregistered target appears
- **AND** pre-existing targets absent from CPA registration are recorded separately, not deleted as part of this change

#### Scenario: Setting drift is visible

- **WHEN** an operator inspects CPA's `force-model-prefix` setting after deployment
- **THEN** it reads `false` through the Management API and in the persisted CPA configuration
