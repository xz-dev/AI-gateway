## ADDED Requirements

### Requirement: Backend selection follows an ordered membership list

For classified inference, the entrance SHALL use one ordered backend list, initially AISIX then terminal CPA. Each entry SHALL provide upstream identity, credential mode and membership source; the terminal entry SHALL require no membership source. Slash and bare IDs SHALL use the same rule without model-name rewrites or provider-specific branches. A usable exact AISIX membership hit SHALL select AISIX with the caller key. A known absence from that usable set SHALL select CPA with `CPA_API_KEY`. Uncertain routing SHALL select AISIX with the caller key. No backend response SHALL trigger another attempt.

The dedicated membership-discovery key SHALL never be sent to inference endpoints. Missing CPA credentials SHALL fail closed with local 503 only when CPA is selected. Existing Sub2API authorization SHALL remain effective; AISIX ACL/auth denials SHALL remain terminal. Authorized CPA-only bare IDs SHALL be eligible for CPA selection while remaining catalog-hidden. Catalog omission SHALL NOT itself grant or deny access; catalog membership/order/priority SHALL remain unchanged.

#### Scenario: AISIX-defined slash ID has priority
- **WHEN** a usable exact snapshot includes a classified request's slash ID
- **THEN** AISIX receives it unchanged with the caller credential, as for a bare ID
- **AND** no separate selector-originated CPA inference attempt occurs

#### Scenario: CPA-only bare or prefixed ID
- **WHEN** a trustworthy extracted model is absent from a usable snapshot and the caller is authorized
- **THEN** terminal CPA receives the request once with its service credential, regardless of slash presence
- **AND** catalog membership remains unchanged

#### Scenario: AISIX permission denial remains terminal
- **WHEN** a model present in the discovery snapshot is forbidden to the inference caller
- **THEN** AISIX evaluates that caller's credential and its denial is returned without CPA selection or retry

#### Scenario: Missing CPA service key
- **WHEN** usable snapshot absence selects CPA but its service key is missing
- **THEN** the entrance returns a local 503 without sending the caller or discovery key to CPA

#### Scenario: Another backend is inserted
- **WHEN** a compatible backend is added to the ordered list
- **THEN** its entry supplies upstream, credential mode and bounded membership source without per-model naming branches

### Requirement: Background membership has bounded validity and explicit uncertainty

AISIX membership SHALL be an exact-ID set refreshed outside the inference path by one non-overlapping per-worker timer using a dedicated full-visibility service key. The initial proposed refresh interval is 3 seconds with a maximum age of 15 seconds; final engineering values SHALL be confirmed with the pipe prototype, not exposed as alternate routing mechanisms. Refresh duration, body bytes, ID count/length and retained generations SHALL be bounded. Only a complete valid discovery response SHALL atomically replace the current set; a valid empty set SHALL differ from no loaded set. A failed refresh SHALL NOT reset last-success age.

A still-young prior set MAY be used after a refresh failure only to the bounded maximum age. No loaded snapshot, expired/invalid snapshot or clock anomaly SHALL be uncertain. Model missing within `MAX_ROUTING_PREFIX_BYTES`, malformed JSON prefix, invalid/incomplete model or duplicate top-level model within the retained prefix SHALL also be uncertain. The request-prefix constant's **value remains pending client survey**. Uncertain requests SHALL go to AISIX with caller credentials, never to CPA on speculative absence. Warn at a bounded rate without logging credentials or bodies. No inference request SHALL synchronously fetch or wait for membership.

Wildcard AISIX model definitions SHALL be forbidden through both local and Ansible resource validation because the list does not enumerate them. Wildcard caller ACLs SHALL NOT be confused with wildcard model definitions. Discovery full visibility SHALL be provisioned and verified separately from inference authorization. The documentation SHALL explicitly permit this background snapshot and its bounded stale interval, superseding the old unconditional cache ban; `/routing-index`, catalog-derived inference decisions and unbounded last-known-good routing SHALL remain prohibited.

#### Scenario: Cold or expired membership
- **WHEN** no valid set is loaded or the last successful set exceeds maximum age
- **THEN** the request streams once to AISIX regardless of slash presence
- **AND** discovery refresh remains background work and warnings are rate-limited

#### Scenario: Refresh fails within maximum age
- **WHEN** refresh fails while the previous snapshot is still within its maximum age
- **THEN** that previous set remains usable only until its original age expires
- **AND** failure does not reset the age or publish partial membership

#### Scenario: Model is late, malformed or duplicated in prefix
- **WHEN** the bounded prefix cannot provide one trustworthy top-level model, including a duplicate within retained bytes
- **THEN** retained prefix plus the unread body stream once to AISIX
- **AND** the gateway does not grow/spool the request or retry CPA after an AISIX error

#### Scenario: Wildcard definition cannot be deployed
- **WHEN** local resources or an Ansible AISIX resources candidate contains a wildcard model definition in a supported naming field
- **THEN** policy validation rejects it before activation
- **AND** intentionally retained Axis entries and wildcard-access caller permissions are not removed

#### Scenario: Invalid discovery response preserves safety
- **WHEN** refresh returns auth failure, malformed/truncated/oversized JSON or invalid ID data
- **THEN** no partial set is published, prior age is preserved and expiry routes uncertain requests to AISIX

## MODIFIED Requirements

### Requirement: One backend per request with no cross-backend retry

The entrance SHALL select exactly one inference backend from bounded prefix classification and bounded-age membership, with AISIX as the uncertain default. A selected backend's 404/model-not-found, auth/ACL denial, 429, 5xx, timeout, connection failure or stream error SHALL NOT trigger retry or selection of another backend. Existing response/provider error behavior SHALL be preserved. This constraint applies to entrance selection; Sub2API account-pool retries and AISIX internal routing budgets remain within their own boundaries.

#### Scenario: Model not found after selection
- **WHEN** the selected backend returns model-not-found, including AISIX's precise 404/type signature after a stale-set race
- **THEN** that response is terminal and the other backend is not attempted

#### Scenario: Rate limit on the chosen backend
- **WHEN** the selected backend returns 429
- **THEN** that failure reaches the client without another backend attempt

#### Scenario: Stream error after forwarding
- **WHEN** the selected stream disconnects or errors after response commitment
- **THEN** existing stream failure behavior applies without replay or another backend attempt

### Requirement: Classification scope is the model-bearing HTTP seam

Classification SHALL apply only to POST `/v1/responses`, `/v1/chat/completions` and `/v1/images/generations`, including bridged WS turns sent through HTTP-only Sub2API. Selector routes SHALL take precedence over CPA-direct routes. Direct WS upgrades SHALL remain unclassified and forward to CPA without frame inspection. Non-classified paths SHALL retain their existing matching and compatibility behavior.

Classification SHALL use incremental bounded-prefix scanning, not a complete-body JSON decoder. Missing, invalid or ambiguous model information in that prefix SHALL be uncertain and go to AISIX. Full-document validation beyond the committed prefix SHALL be assigned explicitly through the user compatibility decision; the retired local-400/no-forward guarantee SHALL NOT be silently claimed for a streaming body.

#### Scenario: Direct WebSocket stays outside the selector
- **WHEN** a client opens a direct WebSocket upgrade at the entrance
- **THEN** it is forwarded to CPA without model classification or frame inspection

#### Scenario: Malformed body on a classified path
- **WHEN** classified prefix scanning encounters malformed JSON or cannot extract one trustworthy model
- **THEN** the request streams to AISIX, whose response is terminal
- **AND** the entrance does not buffer the rest of the body to restore the old local rejection behavior

#### Scenario: Non-classified endpoint unchanged
- **WHEN** a request is outside the enumerated classification paths
- **THEN** existing route and compatibility processing remain distinct from uncertain classified routing

### Requirement: Preserved behavior outside the routing seam

Existing response streaming, model IDs, provider errors, Sub2API account/group authorization and non-chat catalog filtering SHALL be preserved. Request bytes SHALL be preserved except for an explicitly user-approved pipe-compatible retention-field removal. Complete-body validation semantics at the classified seam SHALL be documented and approved separately; they SHALL NOT be preserved by reinstating full-body decoding. Catalog visibility SHALL remain admission/authorization constrained and SHALL NOT prove invocation eligibility.

#### Scenario: Streaming and body fidelity retained
- **WHEN** a request streams to its one selected backend
- **THEN** model bytes and response stream semantics remain intact
- **AND** only the explicitly chosen retention transformation may alter request bytes

#### Scenario: Unauthorized model stays invisible
- **WHEN** Sub2API denies entitlement or AISIX denies its caller access to a present definition
- **THEN** that denial remains effective without privilege escalation through discovery credentials
- **AND** catalog-visible listing remains constrained by existing admission and authorization rules

### Requirement: Client WebSocket terminates before Sub2API

The public chain SHALL be public APISIX → keepalive → keeper → responses-ws-sse-bridge → Sub2API (HTTP/SSE only) → inner `apisix-models` streaming selector → one AISIX/CPA backend. Keeper SHALL remain a pure single-upstream cache with `IGNORE_CLIENT_STORE_FALSE=true`: forward/record a turn without a parent; expand a cached parent and remove `previous_response_id` before forwarding/recording; forward an unknown-parent request unchanged without recording that chain. The bridge SHALL remain stateless and convert WS turns to POST/SSE without history storage or retries. Sub2API SHALL NOT move before keeper/bridge. Reasons and native-WS tradeoffs remain in `docs/websocket-termination.md`.

After front-bridge activation, every Sub2API OpenAI API-key account SHALL use `extra.openai_apikey_responses_websockets_v2_mode=off`, not `http_bridge`, `ctx_pool` or `passthrough`. Unified-entrance accounts SHALL retain `credentials.base_url=http://cli-proxy-api:8317/v1`; third-party accounts retain their own URLs. Retired `/rk/v1` SHALL NOT return. No Sub2API code modification is authorized.

#### Scenario: Bare ID over client WebSocket
- **WHEN** a client sends a bare-ID Responses WS turn through the public chain
- **THEN** keeper/bridge convert it to HTTP through Sub2API and the inner pipe applies membership selection or uncertain-to-AISIX

#### Scenario: Cached continuation after reconnect
- **WHEN** a client reconnects with a cached parent plus new input
- **THEN** keeper expands history and removes the parent before bridge HTTP forwarding

#### Scenario: Account left on a WebSocket mode or /rk
- **WHEN** an account retains an unsupported WS mode or retired `/rk/v1` URL after cutover
- **THEN** it violates the HTTP-only contract and the front bridge is not bypassed

### Requirement: CPA runs an official image with default model-prefix behavior

CPA SHALL remain an official upstream release pinned by digest without gateway source patches. `routing.force-model-prefix` SHALL remain `false`, the upstream default. Gateway routing and catalog membership SHALL NOT depend on toggling that setting: the selector uses exact AISIX membership or uncertain-to-AISIX for both bare and slash IDs; AISIX CPA-pool targets SHALL retain prefixed or CPA-registered slash IDs. No new CPA registration, catalog rewrite or provider patch is authorized.

#### Scenario: Prefixed ID reaches only its prefix's credentials
- **WHEN** an authorized `xl/gpt-6.1-sol` request reaches CPA through an AISIX direct target or terminal snapshot selection
- **THEN** CPA serves it only through the `xl` credential and the model bytes are unchanged

#### Scenario: Bare ID still routes to AISIX
- **WHEN** usable membership contains `gpt-6.1-sol` and CPA also registers a bare copy
- **THEN** AISIX is chosen with caller credentials and no separate CPA attempt

#### Scenario: Unprefixed slash ID served by its registering credential
- **WHEN** usable membership lacks `devin/claude-opus-5-5` and only Devin registers it at CPA
- **THEN** terminal CPA receives it unchanged and serves it with that account

#### Scenario: AISIX target no longer registered by CPA
- **WHEN** a CPA image change stops registering an unprefixed slash ID that an AISIX direct model names, such as `devin/deepseek-v4-1-flash` in v8.0.45
- **THEN** that direct model and every logical-route target naming it are removed from the AISIX resources in the same deployment window
- **AND** every AISIX target that CPA registered at pre-capture remains registered, except accepted upstream removals removed from AISIX in that window; no new unregistered target appears
- **AND** pre-existing targets absent from CPA registration are recorded separately, not deleted as part of this change

#### Scenario: Setting drift is visible
- **WHEN** CPA's model-prefix setting is inspected
- **THEN** Management API and persisted config both show `false`

## REMOVED Requirements

### Requirement: Backend selection follows the model ID slash rule

**Reason**: Revised user Decision A chooses a bounded streaming pipe and background exact membership; B/C retain pure priority and authorized CPA-only bare selection. Error replay is rejected.

**Migration**: Resolve client-survey prefix cap and compatibility decisions, then pass the APISIX 3.19 pipe gate. Explicitly update the cache prohibition, provision full-visibility discovery, validate wildcard-free definitions and keep selected-backend responses terminal. Catalog, authorization, default CPA registration and non-classified routes stay intact.
