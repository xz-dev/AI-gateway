## MODIFIED Requirements

### Requirement: Transparent CPA passthrough

The internal instance SHALL forward requests according to catalog-based backend selection on the classified model-bearing JSON HTTP inference paths (at minimum `POST /v1/responses` and `POST /v1/chat/completions`, including Sub2API `http_bridge` HTTP output): a request whose model ID is in the CPA raw membership (including overlap with AISIX) goes to the CPA upstream without altering semantics, including SSE streaming; a request whose model ID is AISIX-only goes to the AISIX upstream. Classification SHALL happen before inference with no cross-backend retry. Where classification cannot produce a usable outcome the entrance SHALL fail explicitly (model-not-found only under authoritative absence; 503-class otherwise), never silently default to a backend. Requests outside the classified scope — including the legacy direct-WebSocket upgrade used by the Codex Responses transport and endpoints without a model-bearing JSON body — keep their existing forwarding behavior. Raw source discovery traffic never traverses this entrance; client catalog delivery through the entrance is retained.

#### Scenario: Plain chat completion passes through
- **WHEN** Sub2API sends `POST /v1/chat/completions` naming a model in the CPA raw membership
- **THEN** the request reaches CPA and CPA's response body and status are returned unchanged

#### Scenario: SSE streaming passes through
- **WHEN** Sub2API sends a streaming `POST /v1/responses` request
- **THEN** SSE chunks flow from the selected backend to Sub2API incrementally without full-response buffering

#### Scenario: WebSocket passes through
- **WHEN** Sub2API opens a WebSocket connection for the Codex Responses WS transport (`/v1/responses` upgrade)
- **THEN** the upgrade and bidirectional frames pass through with the existing behavior for that transport, outside model classification, and the session behaves as a direct connection (APISIX does not terminate or interpret frames)

#### Scenario: AISIX-only model forwarded to AISIX
- **WHEN** a request on a classified path names a model absent from the CPA raw membership and present in the AISIX raw membership
- **THEN** it is forwarded to the AISIX upstream once, with no CPA attempt

#### Scenario: Chosen backend failure does not re-route
- **WHEN** the selected backend returns 429/5xx, times out, or errors mid-stream
- **THEN** the failure surfaces without any attempt on the other backend

#### Scenario: Unclassifiable request fails honestly
- **WHEN** no usable classification decision exists for the model ID on a classified path
- **THEN** the entrance returns the mapped explicit error (503-class, or model-not-found only under authoritative absence) without forwarding

#### Scenario: Malformed classified payload does not fall through
- **WHEN** a classified path receives a malformed body or one without an extractable model ID
- **THEN** the entrance returns an explicit protocol-shaped error without forwarding to any backend

### Requirement: Codex manifest route split

Requests to `GET /v1/models` SHALL be split by the `client_version` query parameter: with a non-empty value, the request goes to the models-enricher sidecar for the Codex manifest; with an absent or empty value, the request also goes to the models-enricher sidecar for the standard OpenAI `data[]`/`id` projection — no request on this path is proxied unchanged to CPA anymore. The models-table route keeps its current sidecar handling.

#### Scenario: client_version present

- **WHEN** a request arrives for `GET /v1/models?client_version=1.0.0`
- **THEN** it is routed to the models-enricher sidecar and answered with the Codex manifest

#### Scenario: client_version absent or empty

- **WHEN** a request arrives for `GET /v1/models` without `client_version` or with an empty value
- **THEN** it is routed to the models-enricher sidecar and answered with the standard OpenAI `data[]`/`id` projection, not proxied to CPA
