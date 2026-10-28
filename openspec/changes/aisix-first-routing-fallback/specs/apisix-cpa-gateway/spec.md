## ADDED Requirements

### Requirement: Priority passthrough to AISIX or CPA

The internal instance SHALL apply AISIX-first priority to every classified JSON HTTP inference request: `POST /v1/responses`, `POST /v1/chat/completions` and `POST /v1/images/generations`, including front-bridge WS turns forwarded through Sub2API as HTTP. Slash IDs and bare IDs SHALL follow the same priority. No model-list lookup, routing index or membership cache SHALL be consulted.

AISIX SHALL receive the caller credential unchanged. Only HTTP 404 with a complete pre-commit JSON body whose top-level `error.type` equals `model_not_found` SHALL allow one fallback to terminal CPA with the CPA service credential. Other AISIX responses SHALL pass through unchanged at the inner seam; neither other 404s nor `upstream_error` with `error.code=model_not_found` SHALL qualify. CPA's final status, body and end-to-end headers, including its unknown-model error, SHALL pass through unchanged without another attempt. Existing downstream safe-error handling SHALL remain in force.

The entrance SHALL buffer a replayable request before forwarding, within existing limits: 64 MiB for Responses and 16 MiB for the other classified JSON endpoints. It SHALL preserve file-backed handling. Normal nginx/route over-limit rejection SHALL remain 413 before any backend attempt; the selector's defensive invalid/over-limit reader behavior SHALL remain an explicit 400 if boundary controls are bypassed. It SHALL NOT truncate or partially send an over-limit body. Model IDs SHALL remain unchanged. Apart from the existing `prompt_cache_retention` removal performed once before forwarding, request bytes SHALL be preserved. Both attempts SHALL receive byte-identical validated/sanitized bodies without reserialization between attempts.

The entrance SHALL preserve status, end-to-end headers, incremental SSE bytes, cancellation and existing timeout/error behavior. It SHALL NOT buffer a whole successful stream, rewrite events or retry after response commitment. Ambiguous, invalid, incomplete or oversized candidate 404 bodies SHALL NOT authorize fallback; any inspected non-miss response bytes SHALL be forwarded without loss or duplication. Transport errors and timeouts SHALL retain existing client-visible failure semantics without invoking CPA. Each actual fallback SHALL produce one visible INFO log with bounded escaped model ID and non-secret request ID, without keys or bodies.

Malformed classified payloads SHALL fail explicitly before forwarding. Requests outside classification SHALL retain current behavior: multipart `POST /v1/images/edits` to AISIX, direct WebSocket upgrades and other catch-all endpoints (including `/v1/messages`) to CPA. Catalog routes, membership, ordering and priority SHALL remain unchanged. Raw source-discovery traffic SHALL NOT traverse this entrance. Sub2API authorization SHALL remain effective, including for catalog-hidden CPA-only bare IDs.

#### Scenario: AISIX-defined slash ID reaches AISIX
- **WHEN** an authorized classified request names an AISIX-defined slash direct model
- **THEN** AISIX receives the caller credential and unchanged model ID, as for a bare logical model
- **AND** its response passes through without a separate selector-originated CPA attempt

#### Scenario: Confirmed miss replays once to CPA
- **WHEN** AISIX returns HTTP 404 JSON with top-level `error.type=model_not_found` before response commitment
- **THEN** CPA receives the identical validated body with only the approved credential/header substitution
- **AND** AISIX's miss is suppressed, fallback is logged at INFO with model/request ID, and CPA's final response is terminal

#### Scenario: SSE streaming passes through
- **WHEN** Sub2API sends a streaming `POST /v1/responses` request and the serving backend emits delayed or split SSE frames
- **THEN** the same SSE bytes arrive incrementally before backend EOF, without full-response buffering or event rewriting
- **AND** no fallback occurs after response commitment

#### Scenario: File-backed request is replayed within bounds
- **WHEN** an accepted file-backed Responses request receives the exact AISIX miss envelope
- **THEN** both attempts receive identical sanitized bytes and the original model/session identifiers
- **AND** the 64 MiB Responses limit and 16 MiB other-JSON limit remain effective without duplicate whole-response buffering

#### Scenario: Over-limit request is rejected before forwarding
- **WHEN** a body exceeds its normal endpoint limit, including a chunked body crossing that limit
- **THEN** nginx or route body controls return 413 before either backend receives an inference attempt
- **AND** no truncation or fallback occurs

#### Scenario: Direct WebSocket passes through to CPA
- **WHEN** a client opens a direct WebSocket upgrade on `/v1/responses`
- **THEN** the upgrade and frames pass through to CPA without classification
- **AND** public bare-ID WS support SHALL instead use keeper → stateless responses-ws-sse-bridge → HTTP-only Sub2API with API-key account WS mode `off`
- **AND** keeper SHALL retain `IGNORE_CLIENT_STORE_FALSE=true`; Sub2API SHALL NOT move in front of keeper/bridge or use `passthrough`/`ctx_pool` for this chain, as explained in `docs/websocket-termination.md`

#### Scenario: Non-miss 404 is passed through
- **WHEN** AISIX returns 404 with another error type, including `upstream_error` whose code is `model_not_found`
- **THEN** its status/body pass through unchanged at the inner seam and CPA is not attempted by the selector

#### Scenario: Chosen backend failure does not re-route
- **WHEN** AISIX or terminal CPA returns auth/permission failure, 429/5xx, provider error, timeout, connection error or mid-stream failure
- **THEN** existing response/transport failure behavior is preserved without a separate attempt on the other backend

#### Scenario: Malformed classified payload does not fall through
- **WHEN** a classified path receives malformed JSON or lacks a valid top-level model ID
- **THEN** the entrance returns an explicit protocol-shaped error before forwarding

#### Scenario: Denied caller cannot gain service-key access
- **WHEN** authorization rejects invocation at Sub2API or AISIX returns an authentication/permission error
- **THEN** the denial remains effective and cannot trigger service-key fallback
- **AND** catalog hiding alone is not used as an authorization decision

## REMOVED Requirements

### Requirement: Slash-rule passthrough to CPA or AISIX

**Reason**: Approved pure priority replaces slash naming conventions for classified inference, including AISIX-defined slash direct models and authorized CPA-only bare IDs.

**Migration**: Replace with priority passthrough at the same HTTP seam after the local APISIX 3.19 replay/SSE/credential/limit prototype gate passes. Preserve validation, credential isolation, non-classified routes, catalog and downstream authorization/error handling. Only the exact pre-commit AISIX miss permits one retry; no lookup/cache or generic provider-error retry is introduced.
