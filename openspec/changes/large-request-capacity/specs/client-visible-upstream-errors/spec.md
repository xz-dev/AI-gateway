## ADDED Requirements

### Requirement: Every hop reports over-limit and inner failures explicitly

Every hop on the Responses path SHALL report an over-limit body, an exhausted spool or memory budget, or an inner transport failure as an explicit error in the client's protocol. Before commitment, the error SHALL be an HTTP non-success status with a JSON error body. After a WS turn or SSE stream has started, it SHALL be a terminal `error` event. No hop SHALL answer with an empty reply or a bare close frame. When an inner WebSocket closes or an inner HTTP request fails, keeper SHALL relay this to the client as a Responses `error` event that carries the inner close code or status, the reason and the hop name, and SHALL then finish the turn. When a frame or body exceeds its limit, the bridge SHALL send an `error` event naming the limit before it closes. WebSocket library read limits SHALL sit above the cap, so the hop's own code, not the library, answers an over-limit message. Sub2API SHALL treat an inner gateway-generated error as request-scoped. These errors carry a JSON `code` of `request_too_large`, `insufficient_storage`, `insufficient_memory`, `capacity_busy` or `capacity_ticket_*`, together with a `hop` field. Sub2API SHALL also treat every provider 413, for every request size, as request-scoped. For these inner gateway errors and provider 413s, Sub2API SHALL NOT fail over, SHALL NOT retry on another account, SHALL NOT record an account or pool statistic, and SHALL pass the status, headers and body to the client unchanged, with no message rewrite. As a consequence, an account with a smaller private ceiling no longer fails over to another account.

#### Scenario: Bridge rejects an oversized frame
- **WHEN** keeper sends the bridge a frame larger than the bridge limit
- **THEN** the client receives an `error` event with `request_too_large`, the limit and the hop, not a bare close

#### Scenario: Inner close is relayed as an error
- **WHEN** the inner WebSocket behind keeper closes during a turn
- **THEN** keeper sends the client an `error` event with the inner code and reason, then finishes the turn

#### Scenario: Component cannot admit a body
- **WHEN** a hop cannot admit a body because of the cap, a route ceiling, a fallback limit, the admission gate or the disk floor
- **THEN** the client receives an explicit 413, 503 or 507 in its API shape that names the limit or reason, not an empty reply

#### Scenario: Inner gateway error is not failed over
- **WHEN** AISIX or CPA answers Sub2API with a gateway-generated `request_too_large` naming a route ceiling
- **THEN** Sub2API makes no attempt on another account, records no account failure, and the client receives the same status and body naming the limit and the route

#### Scenario: Provider 413 is passed through
- **WHEN** the provider answers a request of any size with 413 and its own cause
- **THEN** Sub2API makes no attempt on another account, and the client receives the provider's status and body unchanged

#### Scenario: Refusal while the client is still uploading
- **WHEN** keeper refuses a request through the firewall while the client is still uploading its body
- **THEN** the client receives the JSON 503, not a 502 or a connection reset

#### Scenario: Fragmented oversized WebSocket message
- **WHEN** a fragmented or permessage-deflate-compressed WebSocket message exceeds the hop's limit after reassembly or inflation
- **THEN** the client receives an `error` event naming the limit, not a library close 1009
