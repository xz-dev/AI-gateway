## ADDED Requirements

### Requirement: Backend selection follows an ordered priority list

For every classified model-bearing JSON inference request, the entrance SHALL use one ordered backend list, initially AISIX then terminal CPA. It SHALL attempt AISIX first for both bare and slash IDs, including AISIX-defined slash direct models. It SHALL advance to CPA at most once and only on the exact proven AISIX miss defined below. Invalid credentials, permission denial, rate limiting, target failure, transport failure and stream failure SHALL NOT establish absence. The original model ID SHALL reach each attempted backend unchanged.

AISIX SHALL receive the caller credential unchanged. CPA SHALL receive the configured CPA service credential, never the caller credential. Existing Sub2API authorization SHALL remain in force. Authorized CPA-only bare IDs SHALL be eligible for fallback despite remaining catalog-hidden; catalog omission SHALL NOT itself grant or deny access. Catalog membership/order/priority SHALL remain unchanged.

Each ordered backend entry SHALL supply its upstream identity, credential mode and miss predicate. Terminal CPA SHALL never advance. A future backend SHALL require an entry with its own verified miss signature, not a new per-model naming convention. Each actual fallback SHALL emit one visible INFO record with bounded escaped model ID and non-secret request ID, without credentials or request bodies.

#### Scenario: Model defined in AISIX
- **WHEN** an authorized classified request names an AISIX-defined bare or slash ID
- **THEN** AISIX receives the caller credential and unchanged model ID
- **AND** the selector does not send a separate inference request to CPA unless AISIX returns the exact proven local miss

#### Scenario: Model only CPA knows
- **WHEN** AISIX returns the exact eligible local miss for an authorized prefixed or bare ID registered only by CPA
- **THEN** the entrance replays the identical validated body once to CPA with the CPA service credential
- **AND** a visible INFO fallback log identifies the model and request ID without leaking credentials

#### Scenario: Model unknown everywhere
- **WHEN** AISIX returns the exact eligible local miss and CPA also does not know the ID
- **THEN** CPA's final status, body and end-to-end headers reach the existing downstream error-handling path unchanged
- **AND** no further backend attempt occurs

#### Scenario: CPA-only bare ID remains catalog-hidden but callable when authorized
- **WHEN** an authorized request names a bare ID absent from AISIX but registered by CPA
- **THEN** the exact AISIX miss permits terminal CPA fallback
- **AND** catalog membership and Sub2API authorization remain unchanged

#### Scenario: Missing CPA credential fails closed
- **WHEN** an exact AISIX miss requires CPA and no CPA service credential is configured
- **THEN** the entrance returns a 503-class error without forwarding to CPA

#### Scenario: A backend is added later
- **WHEN** an operator inserts a backend with a compatible, verified miss predicate into the ordered list
- **THEN** that entry supplies its upstream identity, credential mode and predicate without changing the selection rule for other entries

### Requirement: Exact pre-commit AISIX absence is the only fallback signal

The entrance SHALL perform no model-list lookup, routing-index lookup, membership cache or last-known-good routing decision. It SHALL use AISIX's actual inference response as the sole absence authority: HTTP 404 AND a complete JSON body whose top-level `error.type` is exactly `model_not_found`, before response commitment. Neither generic 404, nested message text nor `error.code` SHALL qualify. Malformed, truncated, ambiguous or oversized candidate bodies SHALL NOT authorize fallback. Candidate inspection SHALL be bounded to 64 KiB and preserve every inspected non-miss response byte when forwarding.

The gateway SHALL buffer replayable validated request bytes before the first attempt, bounded to 64 MiB for Responses and 16 MiB for other classified JSON. Existing nginx/route over-limit behavior SHALL remain 413 before forwarding, including chunked requests; the standalone selector's defensive invalid/over-limit-reader 400 SHALL remain if those controls are bypassed. Existing `prompt_cache_retention` removal SHALL occur once before AISIX, and both AISIX and CPA SHALL receive identical sanitized bytes, including file-backed requests. It SHALL NOT decode/re-encode between attempts, truncate bodies or retry after response commitment.

The discarded eligible AISIX error SHALL NOT be emitted to the client. All other AISIX responses SHALL pass through unchanged at the inner seam, preserving status, end-to-end headers, error payloads and incremental SSE bytes. Existing timeout/connection/stream failure behavior SHALL remain effective without fallback; no whole successful stream SHALL be buffered. CPA's response SHALL be terminal, including a CPA 404 or model-not-found envelope. Existing downstream safe-error translation and internal backend retry budgets SHALL remain unchanged.

#### Scenario: Confirmed local absence
- **WHEN** AISIX returns complete pre-commit 404 JSON with top-level `error.type=model_not_found`
- **THEN** the entrance suppresses that miss response and replays the validated request once to CPA with approved credential substitution
- **AND** no model-list lookup or cache is consulted

#### Scenario: Existing target fails with model-not-found code
- **WHEN** an AISIX-defined target fails with `error.type=upstream_error`, including `error.code=model_not_found` or provider HTTP 404
- **THEN** that failure passes through unchanged and the selector does not try CPA separately

#### Scenario: Non-miss or ambiguous 404
- **WHEN** AISIX returns 404 with another type, invalid/truncated JSON or a candidate body exceeding the inspection bound
- **THEN** that response cannot authorize fallback
- **AND** non-miss response bytes are not lost or duplicated by inspection

#### Scenario: Authentication and availability failures stop
- **WHEN** AISIX returns 401/403, 429 or 5xx, or its connection fails or times out
- **THEN** existing failure behavior is preserved without a CPA attempt

#### Scenario: Terminal CPA does not recurse
- **WHEN** terminal CPA returns any response, including the same status/type shape as an AISIX miss
- **THEN** that response remains terminal and unchanged at the inner seam

## MODIFIED Requirements

### Requirement: One backend per request with no cross-backend retry

The entrance SHALL use one inference-capable backend per request, with one narrow exception: AISIX HTTP 404 JSON with top-level `error.type=model_not_found` before response commitment SHALL permit one replay to terminal CPA. This exception SHALL NOT permit model-list discovery, a cache, generic status-based fallback or retry after response commitment. A selected backend's 429, 5xx, timeout, connection failure, permission/authentication failure, provider error or stream error SHALL NOT trigger cross-backend retry. Existing error propagation and provider error behavior SHALL be preserved. Existing Sub2API account-pool retries and AISIX internal routing budgets remain applicable only within their own boundaries.

#### Scenario: Rate limit on the chosen backend
- **WHEN** the chosen backend returns 429
- **THEN** the client receives that failure without any attempt on the other backend

#### Scenario: Stream error after forwarding
- **WHEN** the chosen backend's stream disconnects or errors after response commitment
- **THEN** the failure surfaces per the existing stream error behavior
- **AND** the other backend is never invoked for the same request

### Requirement: Client WebSocket terminates before Sub2API

The public chain SHALL be public APISIX → keepalive → keeper → responses-ws-sse-bridge → Sub2API (HTTP/SSE only) → inner `apisix-models` selector → AISIX first, then terminal CPA only on the exact pre-commit AISIX miss. Keeper SHALL remain a pure single-upstream cache with `IGNORE_CLIENT_STORE_FALSE=true`: forward and record a turn without a parent; expand a cached parent and remove `previous_response_id` before forwarding and recording; forward an unknown-parent request unchanged without recording that chain. The bridge SHALL be stateless and convert WS turns to POST/SSE without history storage or retries. Sub2API SHALL NOT be placed in front of keeper or the bridge. Reasons and native-WS tradeoffs are recorded in `docs/websocket-termination.md`.

After the front bridge is live, every Sub2API OpenAI API-key account SHALL set `extra.openai_apikey_responses_websockets_v2_mode` to `off`, not `http_bridge`, `ctx_pool` or `passthrough`. Accounts using the unified CPA/AISIX entrance SHALL keep `credentials.base_url=http://cli-proxy-api:8317/v1`; third-party upstream accounts SHALL retain their own URLs. The retired `/rk/v1` base URL SHALL NOT be used. This requirement SHALL be met without modifying Sub2API code for gateway requirements.

#### Scenario: Bare ID over client WebSocket
- **WHEN** a client sends a Responses WS turn naming a bare ID through the public chain
- **THEN** keeper forwards or expands it to the stateless bridge, which sends HTTP to Sub2API
- **AND** Sub2API sends `POST /v1/responses` to the entrance under the AISIX-first HTTP policy

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
- the inner selector SHALL try AISIX first for all classified IDs and replay once to terminal CPA only on the exact pre-commit local miss, forwarding the original ID unchanged;
- AISIX targets that use the CPA pool SHALL name CPA IDs that carry a CPA prefix or a CPA-registered ID containing `/`.

#### Scenario: Prefixed ID reaches only its prefix's credentials
- **WHEN** an authorized client requests `xl/gpt-6.1-sol`
- **THEN** CPA serves it only with the credential whose prefix is `xl`, reached through the AISIX direct target when defined or through terminal fallback after the exact AISIX miss

#### Scenario: Bare ID still routes to AISIX
- **WHEN** a client requests `gpt-6.1-sol`, which AISIX defines, and CPA also registers a bare copy
- **THEN** AISIX remains preferred and the selector does not separately invoke CPA

#### Scenario: Unprefixed slash ID served by its registering credential
- **WHEN** a client requests `devin/claude-opus-5-5`, which only the Devin account registers and AISIX does not define
- **THEN** AISIX's exact miss permits one fallback and CPA serves it with that account

#### Scenario: AISIX target no longer registered by CPA
- **WHEN** a CPA image change stops registering an unprefixed slash ID that an AISIX direct model names, such as `devin/deepseek-v4-1-flash` in v8.0.45
- **THEN** that direct model and every logical-route target naming it are removed from the AISIX resources in the same deployment window
- **AND** every AISIX target that CPA registered at pre-capture remains registered, except accepted upstream removals removed from AISIX in that window; no new unregistered target appears
- **AND** pre-existing targets absent from CPA registration are recorded separately, not deleted as part of this change

#### Scenario: Setting drift is visible
- **WHEN** an operator inspects CPA's `force-model-prefix` setting after deployment
- **THEN** it reads `false` through the Management API and in the persisted CPA configuration

## REMOVED Requirements

### Requirement: Backend selection follows the model ID slash rule

**Reason**: Approved pure AISIX-first priority replaces backend-specific naming conventions, including AISIX-defined slash IDs and authorized CPA-only bare fallback.

**Migration**: Replace with ordered selection and the exact pre-commit 404/type miss contract after the local APISIX 3.19 prototype gate passes. Preserve validation, model bytes, credential isolation, downstream authorization, catalog, non-classified routes and stream/error behavior. Retain no-index/cache rules and document the sole narrow retry exception.
