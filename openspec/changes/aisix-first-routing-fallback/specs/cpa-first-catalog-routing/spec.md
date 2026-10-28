> Planning status: blocked on design decisions A–D. Conditional clauses below are alternatives for review, not authorization to implement all variants. Before apply, task 0 SHALL resolve them into one concrete contract.

## ADDED Requirements

### Requirement: Backend selection follows an ordered priority list

For the operator-approved priority-routing scope, the entrance SHALL use one ordered backend list, initially AISIX then terminal CPA. It SHALL advance only on established absence of the requested model from the earlier backend. Unknown membership, invalid credentials, permission denial, rate limiting, target failure, transport failure and stream failure SHALL NOT establish absence.

Decision B SHALL explicitly determine whether AISIX-defined slash IDs enter priority selection or retain a slash-to-CPA compatibility exception. Decision C SHALL explicitly determine whether CPA-only bare IDs may reach terminal CPA. No naming or catalog-visibility exception SHALL be inferred without that decision. The original model ID SHALL reach the selected backend unchanged.

AISIX SHALL receive the caller credential unchanged. CPA SHALL receive the configured CPA service credential. Existing Sub2API authorization SHALL remain in force; a catalog-hidden name SHALL NOT be treated as either authorized or forbidden merely because it is hidden. A future backend SHALL require one ordered entry with its upstream identity, credential mode and proven membership/error predicate, not a new per-model routing convention.

#### Scenario: Model defined in AISIX within approved scope
- **WHEN** an authorized classified request names a model in the approved priority scope and AISIX establishes that it defines that ID
- **THEN** the request is served through AISIX with the caller credential
- **AND** the selector does not send a separate inference request to CPA

#### Scenario: Model only CPA knows
- **WHEN** AISIX absence is established for an authorized model eligible under decisions B and C
- **THEN** the entrance sends the unchanged request to CPA with the CPA service credential
- **AND** a bounded credential-free selection log identifies the model and backend transition

#### Scenario: Model unknown everywhere
- **WHEN** AISIX absence is established, terminal CPA is permitted by the approved policy, and CPA also does not know the ID
- **THEN** CPA's final error reaches the existing downstream error-handling path without another backend attempt

#### Scenario: Bare-ID policy is explicit
- **WHEN** a request names a bare ID absent from AISIX but registered by CPA
- **THEN** it reaches CPA only if decision C permits that fallback and existing authorization permits invocation
- **AND** the catalog membership is unchanged by that routing choice

#### Scenario: Missing CPA credential fails closed
- **WHEN** CPA is selected and no CPA service credential is configured
- **THEN** the entrance returns a 503-class error without forwarding to CPA

#### Scenario: A backend is added later
- **WHEN** an operator inserts a backend with a compatible, verified absence predicate into the ordered list
- **THEN** the entry supplies its address, credential mode and predicate without changing the selection rule for other entries

### Requirement: Absence evidence follows one approved mechanism

Before implementation, decision A SHALL select exactly one mechanism: live model-list lookup per request, bounded per-worker short-TTL membership, or error-based fallback. Only the selected mechanism SHALL be implemented. Decision D SHALL reconcile the no-lookup/index/cache and no-cross-backend-retry rules with that selection.

For a selected list mechanism, the list SHALL be obtained directly from AISIX, never from the aggregate entrance catalog or a routing index. The presence check SHALL use exact model IDs. A lookup SHALL have bounded total time, response bytes and connections. Timeout, transport failure, non-200 or invalid list/schema SHALL mean unknown membership, never an empty set. The approved uncertainty policy SHALL either dispatch natively to AISIX or return a local protocol-shaped unavailable response; it SHALL NOT skip to CPA.

A key-filtered list omission or wildcard-unenumerated model SHALL NOT prove absence. A list rollout SHALL be blocked unless live caller keys have full access and model definitions are concrete, or the operator separately approves a safe membership policy preserving denial behavior. If short TTL is selected, membership SHALL be partitioned by credential identity with bounded entries/bytes and an explicit freshness/refresh-failure policy. No shared raw-key cache, implicit stale-on-error behavior or last-known-good routing index SHALL be introduced. If per-request lookup is selected, no result SHALL be cached across requests.

For error fallback, eligible absence on the classified OpenAI endpoints SHALL be only a complete, bounded, pre-commit HTTP 404 JSON response whose top-level `error.type` is `model_not_found`. Neither a nested message/code match nor a generic 404 SHALL qualify. The gateway SHALL retain exact replayable request bytes within the existing size limits. It SHALL NOT commit the first response before deciding eligibility, or retry after commitment. Every non-absence response SHALL preserve existing incremental streaming and error behavior.

#### Scenario: Valid live list establishes absence
- **GIVEN** a list mechanism is selected and its full-definition safety conditions hold
- **WHEN** a valid AISIX list omits the requested exact ID
- **THEN** the selector may advance to CPA if decisions B and C permit it

#### Scenario: Lookup fails or caller is denied
- **GIVEN** a list mechanism is selected
- **WHEN** membership is uncertain, including invalid caller authentication or an unsafe key-filtered omission
- **THEN** no CPA fallback occurs on that evidence
- **AND** the approved AISIX-dispatch or local-unavailable policy applies without credential leakage

#### Scenario: AISIX configuration changes under live lookup
- **GIVEN** per-request lookup is selected
- **WHEN** AISIX publishes a new resource snapshot
- **THEN** the next successful lookup reads that live list without an entrance cache
- **AND** if a later dispatch races another reload, AISIX's response surfaces without a second backend attempt

#### Scenario: Cache freshness is bounded and caller-specific
- **GIVEN** short-TTL membership is selected
- **WHEN** two callers have different membership or an entry expires
- **THEN** their membership is not shared and refresh follows the explicitly approved TTL and failure policy

#### Scenario: Confirmed local absence under error fallback
- **GIVEN** error fallback is selected
- **WHEN** AISIX returns the eligible pre-commit local-model-absence envelope
- **THEN** the entrance may replay the unchanged validated request once to CPA with only the approved credential substitution
- **AND** the discarded AISIX error is not emitted to the client

#### Scenario: Existing target fails with model-not-found code
- **GIVEN** error fallback is selected
- **WHEN** an AISIX-defined target returns `error.type = "upstream_error"`, including `error.code = "model_not_found"` or a provider 404
- **THEN** that failure is not treated as AISIX model absence and CPA is not tried separately

## MODIFIED Requirements

### Requirement: One backend per request with no cross-backend retry

The entrance SHALL make at most one inference-capable backend selection per request. If error fallback is selected by decision A, a confirmed AISIX local-model-absence response before dispatch and before response commitment SHALL be the sole exception allowing advancement to terminal CPA. A membership lookup is not an inference attempt. A selected backend's 429, 5xx, timeout, permission/authentication failure, provider error or stream error SHALL NOT trigger cross-backend retry. The existing error-propagation path and provider error behavior SHALL be preserved. Existing Sub2API account-pool retries and AISIX internal routing budgets remain applicable only within their own boundaries.

#### Scenario: Rate limit on the chosen backend
- **WHEN** the chosen backend returns 429
- **THEN** the client receives that failure without any attempt on the other backend

#### Scenario: Stream error after forwarding
- **WHEN** the chosen backend's stream disconnects or errors after response commitment
- **THEN** the failure surfaces per the existing stream error behavior
- **AND** the other backend is never invoked for the same request

### Requirement: Client WebSocket terminates before Sub2API

The public chain SHALL be public APISIX → keepalive → keeper → responses-ws-sse-bridge → Sub2API (HTTP/SSE only) → inner `apisix-models` selector → AISIX/CPA under the approved priority and compatibility policy. Keeper SHALL remain a pure single-upstream cache with `IGNORE_CLIENT_STORE_FALSE=true`: forward and record a turn without a parent; expand a cached parent and remove `previous_response_id` before forwarding and recording; forward an unknown-parent request unchanged without recording that chain. The bridge SHALL be stateless and convert WS turns to POST/SSE without history storage or retries. Sub2API SHALL NOT be placed in front of keeper or the bridge. Reasons and native-WS tradeoffs are recorded in `docs/websocket-termination.md`.

After the front bridge is live, every Sub2API OpenAI API-key account SHALL set `extra.openai_apikey_responses_websockets_v2_mode` to `off`, not `http_bridge`, `ctx_pool` or `passthrough`. Accounts using the unified CPA/AISIX entrance SHALL keep `credentials.base_url=http://cli-proxy-api:8317/v1`; third-party upstream accounts SHALL retain their own URLs. The retired `/rk/v1` base URL SHALL NOT be used. This requirement SHALL be met without modifying Sub2API code for gateway requirements.

#### Scenario: Bare ID over client WebSocket
- **WHEN** a client sends a Responses WS turn naming a bare ID through the public chain
- **THEN** keeper forwards or expands it to the stateless bridge, which sends HTTP to Sub2API
- **AND** Sub2API sends `POST /v1/responses` to the entrance under the approved HTTP selection policy

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
- the inner selector SHALL follow the approved priority policy and decisions B/C, forwarding the original ID unchanged;
- AISIX targets that use the CPA pool SHALL name CPA IDs that carry a CPA prefix or a CPA-registered ID containing `/`.

#### Scenario: Prefixed ID reaches only its prefix's credentials
- **WHEN** an authorized client requests `xl/gpt-6.1-sol`
- **THEN** CPA serves it only with the credential whose prefix is `xl`, whether reached directly or through the AISIX direct target under decision B

#### Scenario: Bare ID still routes to AISIX
- **WHEN** a client requests `gpt-6.1-sol`, which AISIX defines, and CPA also registers a bare copy
- **THEN** AISIX remains preferred and the selector does not separately invoke CPA

#### Scenario: Unprefixed slash ID served by its registering credential
- **WHEN** a client requests `devin/claude-opus-5-5`, which only the Devin account registers and AISIX does not define
- **THEN** CPA serves it with that account under the approved absence/compatibility policy

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

**Reason**: The requested priority rule replaces backend-specific naming conventions. Whether to retain a slash compatibility exception is explicitly blocked on decision B, not chosen by this draft.

**Migration**: Resolve A–D before apply. Replace with the approved ordered-selection and absence-evidence requirements. Preserve validation, exact model IDs, credential substitution, non-classified endpoints, downstream authorization and error/stream handling. Do not implement or archive unresolved alternatives.
