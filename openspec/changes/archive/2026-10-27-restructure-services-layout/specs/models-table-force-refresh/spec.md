## MODIFIED Requirements

### Requirement: Force-refresh action remains diagnostic-only
The models table and its force-refresh route SHALL be served by a dedicated front-door listener. That listener SHALL be bound only to localhost and the Tailscale address on port `9083`, and it SHALL forward these paths directly to `model-catalog`. Localhost and Tailscale are a trusted domain, so the listener SHALL NOT add source-address restrictions. The public front-door listener SHALL NOT route `/models-table` or `/models-table/refresh`.

#### Scenario: Diagnostic route is available on port 9083
- **WHEN** a client on localhost or the tailnet accesses `:9083/models-table`
- **THEN** it can view the table and the refresh form and submit the refresh action

#### Scenario: Public catalog surface does not expose refresh
- **WHEN** a caller accesses `/models-table` or `/models-table/refresh` through the public front-door listener
- **THEN** the request is not routed to `model-catalog`
