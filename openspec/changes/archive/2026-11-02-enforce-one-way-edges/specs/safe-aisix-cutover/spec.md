## MODIFIED Requirements

### Requirement: Isolation and resource bounds are explicit

The final deployment SHALL route each of the Sub2API-to-AISIX, model-catalog-to-AISIX, apisix-models-to-AISIX and AISIX-to-CPA data paths through its own hardened one-way relay between disjoint source and target internal networks. AISIX SHALL share no internal network directly with any data-path peer. Every AISIX listener SHALL bind an explicit address: proxy only on its relay-target addresses, and Admin and metrics only on the dedicated unpublished internal management network. AISIX SHALL have no external/default-route egress. The operator status page SHALL use the approved loopback/Tailscale binding through a relay that binds only its host-ingress address. Every component SHALL use an explicit image tag and verified memory bounds against fresh host headroom.

#### Scenario: Memory gate
- **WHEN** fresh host capacity is measured before deployment
- **THEN** AISIX and its status components fit within the available headroom with margin
- **AND** no swap or system tuning is applied to force fit

#### Scenario: Unexpected egress or management access
- **WHEN** AISIX or the status renderer attempts unapproved outbound access, or the Admin API is reached through a host or Tailscale binding
- **THEN** access is denied without broadening the default egress policy

#### Scenario: AISIX cannot initiate toward its callers
- **WHEN** AISIX opens a TCP connection to any address of Sub2API, model-catalog, apisix-models or their relays on any port
- **THEN** the connection is refused or unroutable
- **AND** only AISIX's own outbound relay to CPA accepts its connections

#### Scenario: Peers cannot reach AISIX management surfaces
- **WHEN** any data-path peer or relay connects to AISIX on the status, Admin or metrics port
- **THEN** the connection is refused or unroutable
