## Purpose

Select the inference backend for each model ID before inference using raw source catalog memberships: CPA first, AISIX second, honest failure otherwise — via a four-outcome routing-index contract, a bounded HTTP route seam, no cross-backend retry, and explicit source-failure semantics at the unified entrance.

## ADDED Requirements

### Requirement: Backend selection follows raw-catalog precedence

The entrance SHALL classify each requested model ID against raw source catalog memberships before any inference attempt: an ID present in the CPA raw membership SHALL route to CPA (including IDs also present in the AISIX raw membership); an ID absent from the CPA raw membership but present in the AISIX raw membership SHALL route to AISIX; an ID absent from both under authoritative snapshots SHALL fail as model-not-found. Raw memberships SHALL be captured from the distinct raw source inventories, not derived from the enriched catalog union, so AISIX-only or alias IDs never become CPA members. The entrance SHALL NOT use an inference probe to decide membership. Routing AISIX requests onward to their configured CPA targets is AISIX's existing behavior and is not a re-classification by the entrance.

#### Scenario: Overlapping ID routes to CPA
- **WHEN** a requested model ID exists in both raw memberships
- **THEN** the request is forwarded to CPA exactly once
- **AND** the AISIX backend is not attempted

#### Scenario: AISIX-only ID routes to AISIX
- **WHEN** a requested model ID is absent from the CPA raw membership and present in the AISIX raw membership
- **THEN** the request is forwarded to AISIX without a CPA attempt
- **AND** AISIX applies its own configured target selection and bounded fallback toward CPA as today

#### Scenario: Unknown ID fails as model-not-found
- **WHEN** a requested model ID is absent from both raw memberships under authoritative snapshots
- **THEN** the entrance returns a model-not-found classification in the served protocol's error shape
- **AND** no backend receives the request

### Requirement: Routing-index lookup returns one of four outcomes

The internal lookup `GET /routing-index?model=<exact ID>` SHALL return exactly one of `cpa`, `aisix`, `not_found`, or `unavailable`, together with the snapshot generation. `not_found` SHALL be returned only when both required raw memberships are authoritative for the generation and the ID is absent from both; it maps to the served protocol's model-not-found error. `unavailable` SHALL be returned when a membership required to decide the ID is not authoritative in the current generation, and, with any lookup transport failure, maps to a service-unavailable (503-class) response retaining the existing public opacity layer. The entrance SHALL perform one bounded lookup per inference request and SHALL NOT cache lookup results in a second entrance-side cache.

#### Scenario: Authoritative absence returns not_found
- **WHEN** both raw memberships are authoritative for the current generation and the ID is absent from both
- **THEN** the lookup returns `not_found` with the generation
- **AND** the entrance maps it to the protocol model-not-found error without forwarding

#### Scenario: Missing required membership returns unavailable
- **WHEN** the membership required to decide the ID is failed or not authoritative in the current generation
- **THEN** the lookup returns `unavailable` with the generation
- **AND** the entrance maps it to a 503-class response, never model-not-found

#### Scenario: Lookup transport failure is unavailable-class
- **WHEN** the routing-index lookup itself times out, errors, or returns a malformed body
- **THEN** the entrance returns a 503-class response
- **AND** it does not fall back to forwarding to either backend or to a not-found answer

#### Scenario: One bounded lookup per request
- **WHEN** an inference request is classified
- **THEN** the entrance performs a single bounded lookup and selects one upstream from its outcome

### Requirement: Source failure is distinct from empty membership

Unknown source availability SHALL NOT be treated as empty membership. Known CPA-membership IDs SHALL remain routable when only the AISIX snapshot is unavailable. A failed or unusable CPA snapshot SHALL NOT cause a request to be guessed as AISIX-only. A valid empty raw inventory is a membership decision, distinct from an unavailable snapshot.

#### Scenario: Cold start before any snapshot
- **WHEN** no authoritative snapshot has completed for either source
- **THEN** inference requests receive a service-unavailable classification
- **AND** model-not-found is not claimed

#### Scenario: Only the AISIX snapshot is unavailable
- **WHEN** the CPA membership is authoritative and the AISIX membership is failed or stale beyond policy
- **THEN** IDs in the CPA raw membership route to CPA normally
- **AND** IDs absent from the CPA membership return `unavailable`, not `not_found`

#### Scenario: CPA snapshot failure does not guess
- **WHEN** the CPA membership is unavailable and the AISIX membership lists the requested ID
- **THEN** the entrance does not conclude AISIX-only routing from the missing CPA membership
- **AND** the request receives a service-unavailable classification

#### Scenario: Valid empty inventory
- **WHEN** a source reports a valid empty raw inventory
- **THEN** that membership is authoritative-empty and absence from it participates in `not_found` decisions, distinct from an unavailable snapshot

### Requirement: Classification covers a bounded model-bearing HTTP route seam

Classification SHALL apply only to the enumerated model-bearing JSON HTTP inference paths of the unified entrance: at minimum `POST /v1/responses` and `POST /v1/chat/completions`, including requests Sub2API's `http_bridge` emits as HTTP inference calls. The selector routes SHALL take precedence over the legacy HTTP alias-rewrite and CPA-direct routes for those paths and sources, so the original requested model ID reaches the selected backend untouched. Non-classified endpoints (including the legacy direct-WebSocket transport and any endpoint outside the enumerated paths) SHALL keep their existing compatibility behavior outside the selector's scope, and the selector SHALL NOT add upgrade-time model inference or raw-frame classification for them. A request on a classified path whose JSON body is malformed or whose model field cannot be extracted SHALL fail explicitly with a protocol-shaped error, never silently fall through to CPA or another backend.

#### Scenario: Selector runs before legacy alias routes
- **WHEN** a request on a classified path carries a model ID that a legacy HTTP alias route would rewrite
- **THEN** the selector's higher-precedence route classifies the original ID first and forwards the original ID untouched to the selected backend
- **AND** no legacy alias rewrite is applied inside the classified scope

#### Scenario: Legacy direct-WS stays outside the selector
- **WHEN** a client opens the legacy direct-WebSocket transport
- **THEN** its existing routing behavior is preserved without model classification at upgrade time and without frame inspection

#### Scenario: Malformed body on a classified path
- **WHEN** a classified path receives a body that is not valid JSON or lacks an extractable model ID
- **THEN** the entrance returns an explicit protocol-shaped error
- **AND** no backend receives the request

#### Scenario: Non-classified endpoint unchanged
- **WHEN** a request arrives on an endpoint outside the enumerated classification scope
- **THEN** it keeps its current matching and forwarding behavior, distinct from a failed classification

### Requirement: One backend per request with no cross-backend retry

The entrance SHALL select exactly one backend per request. A selected backend's 429, 5xx, timeout, or stream error SHALL NOT trigger retry against or re-routing to the other backend; the failure surfaces to the client through the existing error-propagation path with its provider error behavior preserved. This constraint is scoped to the new entrance selection: existing Sub2API account pool retries and AISIX internal routing budgets remain applicable within their own boundaries.

#### Scenario: Rate limit on the chosen backend
- **WHEN** the chosen backend returns 429
- **THEN** the client receives that failure without any attempt on the other backend

#### Scenario: Stream error after forwarding
- **WHEN** the chosen backend's stream disconnects or errors after response commitment
- **THEN** the failure surfaces per the existing stream error behavior
- **AND** the other backend is never invoked for the same request

### Requirement: Membership lookup is bounded and raw collection is non-recursive

Per-inference classification SHALL use a bounded lookup against the published routing index, not a full catalog or upstream inventory fetch on the hot path, and not a distributed commit or index-replica protocol. Raw source collection SHALL fetch source inventories directly from the CPA and AISIX raw endpoints and SHALL NOT loop through the unified entrance; client catalog delivery through the entrance is not restricted by this requirement.

#### Scenario: Hot-path lookup shape
- **WHEN** an inference request is classified
- **THEN** the lookup consumes a bounded index view without fetching either backend's full inventory
- **AND** no recursive collection cycle occurs between the entrance and the snapshot owner

#### Scenario: Discovery path
- **WHEN** the snapshot owner refreshes raw inventories
- **THEN** CPA inventory is read from the CPA source and AISIX inventory from the AISIX source
- **AND** neither raw source read traverses the unified entrance

### Requirement: Preserved behavior outside the routing seam

Existing streaming delivery, request and response body fidelity including model IDs, provider error behavior, Sub2API account and group authorization, and non-chat model filtering SHALL be preserved. Catalog listing visibility SHALL remain constrained by existing admission and authorization rules; identical counts across listing surfaces are not promised, and catalog listing is not proof of invocation eligibility.

#### Scenario: Streaming and body fidelity retained
- **WHEN** a request is classified and forwarded to the chosen backend
- **THEN** the request body, model ID, and streaming semantics are delivered as before the change, apart from upstream selection

#### Scenario: Unauthorized model stays invisible
- **WHEN** a client lacks Sub2API entitlement for a model ID that exists in a raw membership
- **THEN** existing authorization behavior applies and the catalog-visible listing remains admission-constrained
