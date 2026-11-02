## MODIFIED Requirements

### Requirement: Codex manifest route split

Requests to `GET /v1/models` SHALL be split by the `client_version` query parameter:
- With a non-empty value, the request goes to the `model-catalog` service for the Codex manifest.
- With an absent or empty value, the request also goes to the `model-catalog` service for the standard OpenAI `data[]`/`id` projection.

No request on this path is proxied unchanged to CPA. `apisix-models` SHALL route only `/v1/models` to `model-catalog`. It SHALL NOT route `/models-table` or `/models-table/refresh`.

#### Scenario: client_version present

- **WHEN** a request arrives for `GET /v1/models?client_version=1.0.0`
- **THEN** it is routed to `model-catalog` and answered with the Codex manifest

#### Scenario: client_version absent or empty

- **WHEN** a request arrives for `GET /v1/models` without `client_version` or with an empty value
- **THEN** it is routed to `model-catalog` and answered with the standard OpenAI `data[]`/`id` projection, not proxied to CPA

#### Scenario: Models table is not routed through apisix-models

- **WHEN** a request for `/models-table` or `/models-table/refresh` reaches `apisix-models`
- **THEN** it is not forwarded to `model-catalog`

## ADDED Requirements

### Requirement: Front-door catalog leg reaches apisix-models directly

The front-door APISIX SHALL obtain the full catalog for the public `/v1/models` intersection directly from `apisix-models`, with no intermediate catalog proxy. It SHALL use only `GET /v1/models` on that edge, and it SHALL NOT route any public inference or other request to `apisix-models`.

#### Scenario: Public model list is intersected

- **WHEN** a client with a valid key requests `GET /v1/models` through the public front door
- **THEN** the front door intersects the key's Sub2API entitlement list with the catalog it reads from `apisix-models` and returns the intersection

#### Scenario: Public inference does not reach apisix-models from the front door

- **WHEN** a public inference request arrives at the front door
- **THEN** it follows the Sub2API path and is never sent to `apisix-models` directly
