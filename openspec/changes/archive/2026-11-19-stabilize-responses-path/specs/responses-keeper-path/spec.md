## Purpose

Make response-keeper the single Responses hop between the unified entrance and the inference backends (CPA, AISIX). Every Responses turn then crosses the gateway once, keeps its stored history, and never re-enters the entrance with an expanded full-history body.

## ADDED Requirements

### Requirement: All Responses traffic terminates at keeper

apisix-models SHALL forward every Responses request to response-keeper. That covers HTTP `POST` and WebSocket upgrade `GET`, on both `/v1/responses` and the compatibility alias `/rk/v1/responses`.

No Responses request SHALL reach CPA or AISIX except through keeper. Removing `/rk` from a Sub2API account `base_url` SHALL NOT bypass keeper.

#### Scenario: Plain path goes through keeper
- **WHEN** Sub2API sends `POST /v1/responses` to apisix-models
- **THEN** response-keeper receives the request
- **AND** no backend receives it directly from apisix

#### Scenario: Alias path still works
- **WHEN** an account configured with `base_url .../rk/v1` sends `POST /rk/v1/responses` or opens the WebSocket on that path
- **THEN** response-keeper receives it with the same behavior as the plain path

### Requirement: Keeper forwards directly to the selected backend

For each HTTP request or WebSocket `response.create` turn, keeper SHALL classify the original top-level `model` per `cpa-first-catalog-routing`. It SHALL then forward the expanded request to exactly that backend: the CPA endpoint or the AISIX endpoint.

Keeper SHALL NOT send Responses traffic back into apisix-models, and SHALL NOT retry a turn against the other backend.

Classification outcomes SHALL map as follows:

- `not_found` → model-not-found (404-class) error in the served protocol's shape.
- `unavailable`, or a transport failure with no qualifying stored decision → 503-class error in the served protocol's shape.

These errors SHALL be returned before any upstream request and before any history is stored.

#### Scenario: Single gateway traversal
- **WHEN** a continuation turn with `previous_response_id` is processed
- **THEN** the expanded full-history body is sent once, directly to the selected backend
- **AND** apisix-models processes the request at most once, before keeper, with the client's original small body

#### Scenario: WebSocket turns are routed independently
- **WHEN** two consecutive `response.create` turns on one WebSocket carry different model IDs that classify to different backends
- **THEN** each turn is forwarded to its own selected backend
- **AND** history continuity on the socket is preserved per the existing keeper contract

#### Scenario: Unknown model on a WebSocket turn
- **WHEN** a WebSocket turn names a model classified `not_found`
- **THEN** keeper sends one protocol-shaped error frame for that turn
- **AND** the socket stays open, and the previous accepted history stays continuable

### Requirement: Backend credentials and owner scope are preserved

Keeper SHALL set the upstream credential per backend, as follows:

- **CPA:** the configured CPA API key.
- **AISIX:** the caller's original `Authorization`, unchanged.

The configured CPA key SHALL be read from a secret source, never from logs or a committed file.

Keeper's history owner scope SHALL still derive from the caller's original `Authorization` as received from apisix. It SHALL NOT derive from the substituted backend credential. Credentials SHALL NOT appear in logs.

#### Scenario: CPA credential substitution does not merge owners
- **WHEN** two callers with different `Authorization` values both route to CPA
- **THEN** both upstream requests carry the CPA key
- **AND** neither caller can continue the other's `previous_response_id`

#### Scenario: AISIX keeps caller credential
- **WHEN** a turn classifies to AISIX
- **THEN** the upstream request carries the caller's original `Authorization` header

### Requirement: Request transformations formerly done by the selector are preserved

Before forwarding, keeper SHALL remove a top-level `prompt_cache_retention` field from Responses requests, both HTTP bodies and WebSocket `response.create` frames. Keeper SHALL otherwise preserve its accepted bridge writer semantics. Those semantics are: history expansion; `previous_response_id` removal; wrapping a string `input` as one user message; writing `input: null` as `[]`; for WebSocket frames, consuming `type` and sending `stream: true`; and writing the top-level JSON envelope in keeper's canonical key order and punctuation. Every other top-level field value SHALL be copied from the original encoded value without re-encoding. Input array items SHALL be copied unchanged except for expansion.

#### Scenario: Unsupported field stripped on both transports
- **WHEN** an HTTP or WebSocket Responses turn contains `prompt_cache_retention`
- **THEN** the upstream request does not contain that field
- **AND** the result equals the accepted bridge writer output for the same request with only that top-level field removed
- **AND** every other top-level field value keeps its original encoded value

### Requirement: Body limits match the keeper contract

Body limits SHALL be consistent along the path:

- apisix-models SHALL NOT impose a smaller body limit on Responses routes than keeper's client request limit (32 MiB).
- The keeper-expanded body never passes through apisix.

The existing keeper client request limit and expanded-history limits SHALL remain in force, along with their explicit `working_set_too_large` errors.

#### Scenario: Large legal request reaches keeper
- **WHEN** a Responses request body is between 16 MiB and 32 MiB
- **THEN** apisix-models forwards it to keeper instead of rejecting it with 413

### Requirement: Rollout and rollback are scoped and observable

The topology change SHALL be deployed by a scoped operation that recreates only `response-keeper` and `apisix-models`. Exact previous images and configuration SHALL be recorded for rollback.

After deployment, verification SHALL show all of the following:

- both containers are healthy;
- the keeper OOM and restart counts are recorded;
- Responses requests in access logs traverse apisix-models once per turn;
- other containers have unchanged IDs.

The keeper cache SHALL use a dedicated named Docker volume, not tmpfs and not the read-only root filesystem.

#### Scenario: Verified single hop after deploy
- **WHEN** the scoped deployment completes
- **THEN** the apisix-models access log shows no Responses requests whose upstream is `127.0.0.1:9080` (self-loop)
- **AND** the IDs of Sub2API, CPA, AISIX and models-enricher are unchanged

#### Scenario: Rollback restores prior topology
- **WHEN** the operator triggers rollback
- **THEN** the recorded previous keeper image, apisix configuration and compose service definition are restored using only the same two services
