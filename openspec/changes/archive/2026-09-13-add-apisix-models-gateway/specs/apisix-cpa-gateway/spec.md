## Purpose

A new dedicated APISIX instance inserted into the Sub2API → CPA path, transparently carrying all CPA-bound traffic (HTTP, SSE, WebSocket) while splitting Codex models-manifest requests to the models-enricher sidecar. The existing public-ingress APISIX instance is untouched.

## ADDED Requirements

### Requirement: Transparent CPA passthrough

The new instance SHALL forward every request that is not a Codex models-manifest request to the CPA upstream without altering semantics, including SSE streaming and WebSocket upgrades used by the Codex Responses transport.

#### Scenario: Plain chat completion passes through

- **WHEN** Sub2API sends `POST /v1/chat/completions` to the listener
- **THEN** the request reaches CPA and CPA's response body and status are returned unchanged

#### Scenario: SSE streaming passes through

- **WHEN** Sub2API sends a streaming `POST /v1/responses` request
- **THEN** SSE chunks flow from CPA to Sub2API incrementally without full-response buffering

#### Scenario: WebSocket passes through

- **WHEN** Sub2API opens a WebSocket connection for the Codex Responses WS transport (`/v1/responses` upgrade)
- **THEN** the upgrade and bidirectional frames pass through APISIX to CPA and the session behaves as a direct CPA connection (APISIX does not terminate or interpret frames)

### Requirement: Codex manifest route split

Requests to `GET /v1/models` SHALL be split by the presence of a non-empty `client_version` query parameter: with it, the request goes to the models-enricher sidecar; without it, the request is proxied to CPA unchanged.

#### Scenario: client_version present

- **WHEN** a request arrives for `GET /v1/models?client_version=1.0.0`
- **THEN** it is routed to the models-enricher sidecar

#### Scenario: client_version absent or empty

- **WHEN** a request arrives for `GET /v1/models` without `client_version` or with an empty value
- **THEN** it is proxied to CPA and CPA's response is returned without enrichment

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
