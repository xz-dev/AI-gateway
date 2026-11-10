> Open change: requirements below are intended acceptance, not certification
> of full production capacity. See the [2026-11-09 disposition](../../../../../docs/records/maintenance-capacity-closeout-2026-11-09.md);
> residual size checks and public-template enablement remain pending.

## MODIFIED Requirements

### Requirement: Constant two-route entrance to AISIX

The internal `apisix-models` instance SHALL route without reading request bodies. `GET`/`HEAD /healthz` SHALL be answered locally with 200. `GET /v1/models` SHALL go to `model-catalog`. Every other request SHALL go to AISIX with the caller credential, headers and body unchanged by the gateway, and with response buffering disabled so SSE flows incrementally. The instance SHALL NOT run a Lua selector or serverless function, SHALL NOT define a CPA upstream, SHALL NOT hold a CPA credential, and SHALL NOT strip or rewrite any request field. Its nginx `client_max_body_size` SHALL be 256 MiB (the end-to-end cap) as the backstop for keeper-expanded Responses history. Request buffering SHALL be off (`proxy_request_buffering off`), so the body streams to AISIX with no temp file. Oversized bodies SHALL get a JSON 413 that names the limit. The instance SHALL forward the capacity ticket header unchanged, and it SHALL NOT add route or global body-limit plugins.

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
- **WHEN** a Responses body up to 256 MiB arrives
- **THEN** the entrance streams it to AISIX byte-identical with flat memory and no temp file; a larger body is rejected with an explicit 413 naming the limit before forwarding
