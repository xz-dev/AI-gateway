## Purpose

Introduce AISIX additively through an isolated real-CPA acceptance account, then make it the only production logical-model router while retaining data/account recovery evidence but no executable legacy-router fallback.

## ADDED Requirements

### Requirement: Candidate evaluation preserves serving state

AISIX deployment and tests SHALL preserve the existing production account, inference path, catalog path, and active connections. The candidate SHALL be selectable only through an isolated test authorization until explicit cutover approval.

#### Scenario: Candidate deployed
- **WHEN** AISIX and its relays are added
- **THEN** ordinary production routing cannot select them
- **AND** existing namespace owners are not stopped or recreated

#### Scenario: Isolated acceptance against real CPA
- **WHEN** the acceptance matrix runs via the isolated account
- **THEN** chat and Responses paths, cooldown isolation, Retry-After handling, and catalog difference are verified against the real CPA upstream through the deployed relays

### Requirement: Isolation and resource bounds are explicit

The final deployment SHALL retain direct pairwise internal networks without relays on the Sub2API-to-AISIX, AISIX-to-CPA, and enricher-to-AISIX data paths. AISIX SHALL have no external/default-route egress. Its Admin API SHALL be reachable only on a dedicated unpublished internal management network, while the operator status page SHALL use the approved loopback/Tailscale binding. Every component SHALL use an explicit image tag and verified memory bounds against fresh host headroom.

#### Scenario: Memory gate
- **WHEN** fresh host capacity is measured before deployment
- **THEN** AISIX and its status components fit within the available headroom with margin
- **AND** no swap or system tuning is applied to force fit

#### Scenario: Unexpected egress or management access
- **WHEN** AISIX or the status renderer attempts unapproved outbound access, or the Admin API is reached through a host or Tailscale binding
- **THEN** access is denied without broadening the default egress policy

### Requirement: Full cutover and cleanup retain recovery evidence

Production cutover SHALL require explicit approval, fresh complete recovery state, and stop thresholds. Every CPA-backed production account SHALL use the main-project AISIX hostname and caller key after migration. Failures SHALL be repaired in place or use captured data/account state without restoring New API or CPA model routing. After real-client and zero-routed-traffic verification, candidate test records/project and the superseded model-router edge SHALL be removed while private recovery snapshots remain.

#### Scenario: Approved full switch
- **WHEN** the operator approves full migration
- **THEN** every CPA-backed production account receives only the approved URL, caller-key, retry, pool, and WebSocket-mode changes with unrelated fields preserved
- **AND** real client requests verify Responses, catalog entitlements, and scheduling before cleanup

#### Scenario: Post-switch regression
- **WHEN** a stop threshold fails
- **THEN** the fault is repaired without weakening security, or captured data/account state is used to recover service without restoring a retired logical-model router

### Requirement: Private operator status is server rendered and truthful

The approved loopback/Tailscale management binding SHALL serve a server-rendered `/status` page and SHALL NOT expose the AISIX Admin API, Scalar UI, playground, metrics, or inference APIs. The page SHALL use authoritative AISIX model configuration and runtime exclusion state without exposing credentials, upstream URLs, request content, or raw API responses. It SHALL NOT claim or emulate the commercial AISIX Cloud dashboard.

#### Scenario: Operator opens routing status
- **WHEN** the operator sends `GET /status` through the approved management binding
- **THEN** the response is complete HTML without JavaScript and shows each routing combo's configured target order and strategy
- **AND** each direct target shows whether AISIX currently considers it eligible, cooling, unavailable, or unresolved
- **AND** available cooldown expiry or remaining duration and recent status reason are shown

#### Scenario: Status wording preserves routing semantics
- **WHEN** a routing combo or direct target is displayed
- **THEN** `eligible` means only that AISIX is not currently excluding that direct target
- **AND** no routing model is labelled independently healthy
- **AND** no persistent current or last-served target is claimed unless AISIX provides an authoritative retained value

#### Scenario: Non-page path reaches the management binding
- **WHEN** a caller requests an Admin API, Scalar, playground, metrics, inference, or unknown path through the page binding
- **THEN** the response does not proxy that request and reveals no protected AISIX data

### Requirement: Admin API has no standing external binding

The AISIX Admin API SHALL remain enabled only on the dedicated internal management network so the status renderer can read current model configuration and runtime state. It SHALL NOT have a standing host, loopback, Tailscale, wildcard, or public port publication. Operator access SHALL require an explicit temporary maintenance tunnel or relay and the AISIX admin key.

#### Scenario: Default deployment is inspected
- **WHEN** host listeners, Docker port publications, and Tailscale bindings are enumerated
- **THEN** only the status page is reachable on the management port
- **AND** no standing host route reaches the AISIX Admin API

#### Scenario: Operator performs temporary API maintenance
- **WHEN** the operator explicitly opens a temporary maintenance tunnel or relay
- **THEN** protected AISIX management requests still require the admin key
- **AND** closing the temporary path makes the Admin API externally unreachable again
