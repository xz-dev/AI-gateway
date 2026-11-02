# APISIX CPA Gateway Specification

## Purpose

Keep `apisix-models` a constant, body-independent entrance: answer health locally, send model lists to model-catalog and forward everything else to AISIX. Isolate the entrance from CPA while retaining Sub2API compatibility and the front-door catalog leg.

## Requirements

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

### Requirement: Sub2API-facing compatibility

The listener SHALL accept the exact requests Sub2API already sends to CPA (plain OpenAI/Anthropic-style calls with `Authorization` header), requiring no Sub2API changes beyond the upstream address.

#### Scenario: No new client headers

- **WHEN** Sub2API sends any request on this hop
- **THEN** it succeeds without any APISIX-specific headers from Sub2API

### Requirement: Deployment isolation conventions

The changed data path SHALL follow existing ai-gateway conventions: digest-pinned images, one-way connection initiation per directed edge via dedicated relay pairs on two-member internal networks, and no wildcard host port binding. `apisix-models-netns` SHALL have exactly four edges: inbound from Sub2API (`sub2api-cpa-target`), inbound from the front door (`front-catalog`), outbound to model-catalog and outbound to AISIX. It SHALL have no host port publication and no network or relay path to CPA, and validation SHALL fail if such a path is added.

#### Scenario: Listener reachability
- **WHEN** the stack is up
- **THEN** the instance is reachable only from the Sub2API-side relay network and the front-catalog pair, with no host or `0.0.0.0` binding

#### Scenario: CPA is unreachable from the entrance
- **WHEN** validation walks networks and socat relays from `apisix-models-netns` without traversing an application namespace
- **THEN** `cpa-netns` is not reachable, no `cpa` host entry or CPA address is configured, and a TCP connect from the entrance namespace to any CPA address fails in production

#### Scenario: Re-adding a CPA relay is rejected
- **WHEN** a Compose change adds a relay or network linking `apisix-models-netns` to `cpa-netns`
- **THEN** `ops/scripts/validate.sh` fails

### Requirement: Constant two-route entrance to AISIX

The internal `apisix-models` instance SHALL route without reading request bodies. `GET`/`HEAD /healthz` SHALL be answered locally with 200. `GET /v1/models` SHALL go to `model-catalog`. Every other request SHALL go to AISIX with the caller credential, headers and body unchanged by the gateway, and with response buffering disabled so SSE flows incrementally. The instance SHALL NOT run a Lua selector or serverless function, SHALL NOT define a CPA upstream, SHALL NOT hold a CPA credential, and SHALL NOT strip or rewrite any request field. Its nginx `client_max_body_size` SHALL remain 64 MiB as the backstop for keeper-expanded Responses history; it SHALL NOT add route or global body-limit plugins.

#### Scenario: Inference reaches AISIX once
- **WHEN** Sub2API sends `POST /v1/responses`, `POST /v1/chat/completions`, `POST /v1/images/generations` or multipart `POST /v1/images/edits` naming any model ID
- **THEN** the request is forwarded once to AISIX with the caller credential and no CPA attempt from the entrance

#### Scenario: SSE passes through
- **WHEN** a streaming Responses or Chat request is forwarded
- **THEN** SSE events reach Sub2API incrementally without full-response buffering

#### Scenario: Retention field is not touched by the gateway
- **WHEN** a request carries `prompt_cache_retention`
- **THEN** the entrance forwards it unchanged; CPA's targeted payload filter owns its removal

#### Scenario: Model list keeps reaching the catalog
- **WHEN** Sub2API requests `GET /v1/models` with or without `client_version`
- **THEN** the request goes to `model-catalog`, not AISIX

#### Scenario: Large expanded history is admitted
- **WHEN** a Responses body up to 64 MiB arrives
- **THEN** the entrance forwards it to AISIX; a larger body is rejected with 413 before forwarding

### Requirement: Front-door catalog leg reaches apisix-models directly

The front-door APISIX SHALL obtain the full catalog for the public `/v1/models` intersection with a non-empty `client_version` directly from `apisix-models`, with no intermediate catalog proxy. It SHALL use only `GET /v1/models` on that edge, and it SHALL NOT route any public inference or other request to `apisix-models`.

#### Scenario: Public model list is intersected

- **WHEN** a client with a valid key requests `GET /v1/models` with a non-empty `client_version` through the public front door
- **THEN** the front door intersects the key's Sub2API entitlement list with the catalog it reads from `apisix-models` and returns the intersection

#### Scenario: Public inference does not reach apisix-models from the front door

- **WHEN** a public inference request arrives at the front door
- **THEN** it follows the Sub2API path and is never sent to `apisix-models` directly

### Requirement: Public plain model list order

The front-door APISIX SHALL return the plain public model list in the catalog output order. This covers `GET /models` and `GET /v1/models` without a non-empty `client_version`, both answered from Sub2API. The order is:
1. IDs without `/`, sorted by Unicode code point;
2. then every other ID, sorted by Unicode code point.

Comparison SHALL be exact and case-sensitive, with no locale or case folding. Entries with equal IDs SHALL keep Sub2API's relative order.

The front door SHALL NOT add, remove or alter any entry. Each entry SHALL be emitted byte-for-byte as Sub2API returned it, and every other top-level field of the response SHALL be kept. The request SHALL reach Sub2API with the caller's path, query and credentials, and it SHALL share the existing front-door admission slot for external model-list requests.

A non-2xx Sub2API response SHALL pass through with its status and body unchanged. A 2xx body that the front door cannot recognize SHALL also pass through with its status and body unchanged and SHALL be logged at WARN. Neither case SHALL be turned into a gateway error. A body is not recognized when it is not a JSON object with exactly one `data` or `models` array of objects whose identities (`id`, or `slug` for `models`) are non-empty strings without conflicting `id`/`slug` values.

A reordered `200` SHALL carry:
- `Cache-Control: private, no-cache`;
- a strong ETag computed over the final body;
- `Vary` that includes `Authorization`.

It SHALL honor `If-None-Match` with `304` and a failed `If-Match` with `412`.

The parameterized Codex manifest SHALL keep the order of model-catalog's response.

#### Scenario: Bare IDs first, code-point order

- **WHEN** Sub2API returns `data` with IDs `z/a`, `b`, `A/b`, `B`, `a`, `a/b` in that order
- **THEN** the client receives them as `B`, `a`, `b`, `A/b`, `a/b`, `z/a`, each entry byte-identical to Sub2API's

#### Scenario: Non-ASCII IDs use code-point order

- **WHEN** Sub2API returns IDs `é`, `z`, `😀` and `Z`
- **THEN** the client receives `Z`, `z`, `é`, `😀`

#### Scenario: Same members as Sub2API

- **WHEN** any recognized 2xx list is reordered
- **THEN** the set of IDs, the number of entries, every entry's bytes and every other top-level field equal Sub2API's response

#### Scenario: Sub2API error passes through

- **WHEN** Sub2API answers `401`, `404`, `429` or `5xx`
- **THEN** the front door returns that status and body, and the existing public error shaping still applies

#### Scenario: Unrecognized body passes through

- **WHEN** Sub2API answers `200` with a body that is not one recognizable model collection
- **THEN** the client receives that status and body unchanged, and a WARN is logged

#### Scenario: Conditional request on the ordered list

- **WHEN** a client repeats the request with `If-None-Match` set to the ETag of the ordered list
- **THEN** it receives `304` with an empty body

#### Scenario: Codex manifest order unchanged

- **WHEN** a client requests `GET /v1/models?client_version=1.0.0`
- **THEN** the intersection keeps model-catalog's order and is not reordered again by the front door
