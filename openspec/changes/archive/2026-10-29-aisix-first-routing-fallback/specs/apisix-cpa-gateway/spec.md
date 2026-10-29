## ADDED Requirements

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

## MODIFIED Requirements

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

## REMOVED Requirements

### Requirement: Slash-rule passthrough to CPA or AISIX

**Reason**: Backend priority now resolves inside AISIX through its `*` fallback; the entrance no longer classifies bodies, holds a CPA credential or forwards anything to CPA.

**Migration**: Replaced by "Constant two-route entrance to AISIX" here and "AISIX resolves backend priority with one `*` fallback to CPA" in `cpa-first-catalog-routing`. Direct WebSocket upgrades now reach AISIX instead of CPA; the supported WS path remains keeper → stateless bridge → Sub2API HTTP.

### Requirement: Future model routing headroom

**Reason**: Future backends are added as AISIX rows, not as entrance routing; the entrance is intentionally constant.

**Migration**: Add backends in AISIX resources. APISIX features remain available but are not part of this capability's contract.
