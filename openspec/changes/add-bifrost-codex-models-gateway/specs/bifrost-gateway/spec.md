## Purpose

A Bifrost service inserted into the Sub2API → CPA data path that transparently carries all CPA-bound traffic (HTTP, SSE, WebSocket) while hosting the codex-models-enricher plugin, deployed with the project's standard isolation conventions.

## ADDED Requirements

### Requirement: Transparent CPA passthrough

The Bifrost service SHALL forward every request that is not a Codex models-manifest request to the configured CPA upstream without modifying semantics, including SSE streaming and WebSocket upgrades used by the Codex Responses transport.

#### Scenario: Plain chat completion passes through

- **WHEN** Sub2API sends `POST /v1/chat/completions` to the Bifrost listener
- **THEN** the request is proxied to CPA and CPA's response is returned byte-equivalent in status, headers (hop-by-hop excepted), and body

#### Scenario: Streaming passes through

- **WHEN** Sub2API sends a streaming `POST /v1/responses` request
- **THEN** SSE chunks flow from CPA through Bifrost to Sub2API without buffering the full response

#### Scenario: WebSocket passes through

- **WHEN** Sub2API opens a WebSocket connection for the Codex Responses WS transport
- **THEN** the upgrade and bidirectional frames pass through Bifrost to CPA and the session behaves as a direct CPA connection

#### Scenario: Models list without client_version is not enriched

- **WHEN** a client calls `GET /v1/models` without a `client_version` query parameter
- **THEN** the request is proxied to CPA and CPA's response is returned without enrichment

### Requirement: Sub2API-facing compatibility

The Bifrost listener SHALL accept the exact requests Sub2API already sends to CPA (plain OpenAI/Anthropic-style calls with `Authorization` header), requiring no Sub2API configuration or header changes beyond the upstream address.

#### Scenario: No gateway-specific headers required

- **WHEN** Sub2API sends any request without Bifrost-specific headers (no `x-bf-*` headers)
- **THEN** the request is routed to the CPA upstream successfully

### Requirement: Deployment isolation conventions

The Bifrost deployment SHALL follow the existing ai-gateway conventions: digest-pinned images, non-root and read-only container filesystem where the image supports it, one-way connection initiation per directed edge via dedicated relay pairs on two-member internal networks, and no wildcard host port binding.

#### Scenario: One-way initiation

- **WHEN** the compose stack is up
- **THEN** Bifrost can initiate connections to CPA and to the egress proxy, and no container in the CPA-side or Sub2API-side networks can initiate a new connection into the Bifrost network namespace beyond the designated relay listeners

#### Scenario: No public port

- **WHEN** inspecting published host ports
- **THEN** the Bifrost listener is reachable only from the Sub2API-side relay network, with no `0.0.0.0` host binding
