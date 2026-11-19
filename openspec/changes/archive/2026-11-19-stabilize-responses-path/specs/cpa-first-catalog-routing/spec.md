## MODIFIED Requirements

### Requirement: Routing-index lookup returns one of four outcomes

The internal lookup `GET /routing-index?model=<exact ID>` SHALL return exactly one of `cpa`, `aisix`, `not_found`, or `unavailable`, together with the snapshot generation.

- `not_found` SHALL be returned only when both required raw memberships are authoritative for the generation and the ID is absent from both. It maps to the served protocol's model-not-found error.
- `unavailable` SHALL be returned when a membership required to decide the ID is not authoritative in the current generation. It maps to a service-unavailable (503-class) response that keeps the existing public opacity layer.

Every classifying component (the apisix selector and response-keeper) SHALL make one bounded lookup per inference request or WebSocket turn.

A lookup **transport failure** is a timeout, connection or read error, non-200 status, or malformed or schema-invalid body. On a transport failure, the classifier SHALL use its bounded last-known-good decision for the same exact model ID when one exists:

- Only an authoritative `cpa` or `aisix` answer received by that classifier process within the last 15 minutes qualifies.
- Each successful lookup replaces the stored decision.
- An authoritative `not_found` or `unavailable` answer SHALL delete any stored decision for that ID and SHALL never be overridden by it.

When no qualifying stored decision exists, a transport failure SHALL map to a 503-class response. The stored decision is a failure fallback only. It SHALL NOT replace a lookup that succeeds, and SHALL NOT serve as a general cache. The fallback store SHALL be bounded in entries and memory and SHALL NOT persist across process restarts.

#### Scenario: Authoritative absence returns not_found
- **WHEN** both raw memberships are authoritative for the current generation and the ID is absent from both
- **THEN** the lookup returns `not_found` with the generation
- **AND** the classifier maps it to the protocol's model-not-found error without forwarding
- **AND** any stored decision for that ID is discarded

#### Scenario: Missing required membership returns unavailable
- **WHEN** the membership required to decide the ID is failed or not authoritative in the current generation
- **THEN** the lookup returns `unavailable` with the generation
- **AND** the classifier maps it to a 503-class response, never model-not-found, even if a stored decision exists

#### Scenario: Transport failure with a fresh stored decision
- **WHEN** the lookup times out, errors, or returns a malformed body
- **AND** the same classifier process received an authoritative `cpa` or `aisix` answer for the same exact model ID within the last 15 minutes
- **THEN** the request is forwarded to that backend exactly once
- **AND** a bounded warning records that the fallback was used, without request content or credentials

#### Scenario: Lookup transport failure is unavailable-class
- **WHEN** the lookup fails at the transport level (times out, errors, or returns a malformed body) and no qualifying stored decision exists, for example after a cold start or after expiry
- **THEN** the classifier returns a 503-class response
- **AND** it does not guess a backend or answer model-not-found

#### Scenario: One bounded lookup per request
- **WHEN** an inference request or WebSocket turn is classified
- **THEN** the classifier makes one bounded lookup and selects one upstream, either from its outcome or, on transport failure only, from the qualifying stored decision

### Requirement: Classification covers a bounded model-bearing HTTP route seam

Classification SHALL apply only to the enumerated model-bearing inference paths of the unified entrance, owned as follows:

- **apisix selector:** `POST /v1/chat/completions` and `POST /v1/images/generations`, including requests that Sub2API's `http_bridge` emits as HTTP inference calls.
- **response-keeper:** `POST /v1/responses` and each `response.create` turn of a Responses WebSocket.

The selector routes SHALL take precedence over the legacy HTTP alias-rewrite and CPA-direct routes for their paths and sources, so the original requested model ID reaches the selected backend untouched.

Responses routes SHALL be forwarded to response-keeper without apisix reading, decoding, or rewriting the body.

Non-classified endpoints SHALL keep their existing compatibility behavior outside the classification scope. This includes WebSocket upgrades other than Responses and any endpoint outside the enumerated paths.

A request on a classified path SHALL fail explicitly with a protocol-shaped error, and SHALL never silently fall through to CPA or another backend, when either:

- its JSON body is malformed, or
- its model field cannot be extracted.

#### Scenario: Selector runs before legacy alias routes
- **WHEN** a chat/completions or images/generations request carries a model ID that a legacy HTTP alias route would rewrite
- **THEN** the selector's higher-precedence route classifies the original ID first and forwards the original ID untouched to the selected backend
- **AND** no legacy alias rewrite is applied inside the classified scope

#### Scenario: Responses bypass body parsing in apisix
- **WHEN** a `POST /v1/responses` request or Responses WebSocket upgrade reaches apisix-models
- **THEN** apisix forwards it to response-keeper without reading or decoding the request body
- **AND** response-keeper performs the classification

#### Scenario: Legacy direct-WS stays outside the selector
- **WHEN** a client opens a non-Responses WebSocket transport through the entrance
- **THEN** its existing routing behavior is preserved without model classification at upgrade time and without frame inspection by apisix
- **AND** only Responses WebSocket turns are classified, by response-keeper, per `response.create` frame

#### Scenario: Malformed body on a classified path
- **WHEN** a classified path receives a body that is not valid JSON or lacks an extractable model ID
- **THEN** the owning classifier returns an explicit protocol-shaped error
- **AND** no backend receives the request

#### Scenario: Non-classified endpoint unchanged
- **WHEN** a request arrives on an endpoint outside the enumerated classification scope
- **THEN** it keeps its current matching and forwarding behavior, distinct from a failed classification
