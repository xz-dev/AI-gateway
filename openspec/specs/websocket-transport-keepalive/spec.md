# websocket-transport-keepalive Specification

## Purpose

Keep client-facing WebSocket connections usable during silent AI turns through the existing fixed-upstream keepalive proxy, without moving authentication or AI session semantics out of Sub2API. Define compatible transport behavior and the evidence required to publish, deploy, and accept that capability.

## Requirements

### Requirement: WebSocket keepalive is an explicit transport option

The existing proxy SHALL offer an explicitly enabled WebSocket keepalive mode for RFC 6455 upgrades to its fixed configured upstream. With the option disabled, existing Upgrade passthrough SHALL remain available. Ordinary HTTP, SSE, non-stream heartbeat, and non-WebSocket Upgrade behavior SHALL remain unchanged. Neither request headers nor query parameters SHALL select a different upstream or bypass the existing network/security boundary.

#### Scenario: Keepalive is disabled
- **WHEN** a client opens a WebSocket with the option disabled
- **THEN** the existing transparent Upgrade path handles the connection without proxy-generated WebSocket heartbeats

#### Scenario: Other protocols share the proxy
- **WHEN** ordinary HTTP, SSE, non-stream HTTP, or a non-WebSocket Upgrade request arrives with WebSocket keepalive enabled
- **THEN** it follows its existing behavior without WebSocket handling or new payload inspection

#### Scenario: A request attempts to select another target
- **WHEN** an incoming URL or header contains an alternative destination
- **THEN** the proxy still uses only its configured upstream and does not follow an upstream redirect to another destination

### Requirement: The upstream decides admission before downstream upgrade

The proxy SHALL validate the downstream WebSocket handshake and obtain a valid upstream upgrade before sending downstream HTTP 101. Upstream handshake rejection SHALL retain the applicable HTTP status, end-to-end headers, and response body through the existing public security boundary; it SHALL NOT become a successful upgrade, an arbitrary library error body, or a silently truncated response. Authentication/session headers, request path and query semantics, and approved forwarded-header behavior SHALL be preserved. Handshake-only fields SHALL be generated separately for each leg. A selected subprotocol SHALL have been offered by the client and accepted upstream; unsupported extensions SHALL NOT be advertised as negotiated.

#### Scenario: Upstream rejects authentication
- **WHEN** the fixed upstream rejects a WebSocket handshake
- **THEN** the client receives the corresponding HTTP rejection through existing public error policy
- **AND** no downstream HTTP 101 or second attempt is sent

#### Scenario: Rejection body exceeds a library diagnostic buffer
- **WHEN** an upstream non-101 response contains an error body larger than 1 KiB
- **THEN** the response is forwarded without silent truncation to a diagnostic prefix and its body is closed after forwarding

#### Scenario: Subprotocol and session identity are negotiated
- **GIVEN** the client offers subprotocols and supplies supported authentication and session headers
- **WHEN** the upstream accepts one offered subprotocol
- **THEN** the downstream selects the same subprotocol and the upstream receives the intended identity exactly once

#### Scenario: Downstream upgrade fails after upstream acceptance
- **WHEN** the downstream disconnects or its upgrade cannot complete
- **THEN** the newly opened upstream connection is released without leaving a session or relay running

### Requirement: Application messages preserve their transport semantics

The relay SHALL preserve application message type, content, order, and message boundaries in both directions. It SHALL support valid fragmented messages and any extensions it negotiates without confusing control frames with application data. Reframing, masking, and compression on individual legs are transport details, not byte-for-byte wire transparency. The relay SHALL NOT parse AI request bodies, discard pipelined data, synthesize Responses/SSE events on a WebSocket, merge messages, replay a request, reconnect an interrupted session automatically, or alter model/session selection.

#### Scenario: Multi-turn and pipelined input
- **WHEN** several application messages arrive while a preceding turn is still active
- **THEN** their bytes and ordering are retained or backpressured until delivery
- **AND** none is consumed as disposable keepalive data

#### Scenario: Binary and fragmented messages
- **WHEN** valid text or binary messages contain fragmentation and interleaved control frames
- **THEN** the peer receives the same application messages and control frames do not appear as extra business events

#### Scenario: Large supported prompt
- **WHEN** the client sends a message larger than 32 KiB but within the approved deployed message-size limit
- **THEN** the proxy does not reject it because of a library default read limit

### Requirement: Downstream heartbeat does not depend on AI turn progress

After both handshakes succeed, the proxy SHALL generate periodic downstream Ping control frames during downstream application-data silence and process matching Pongs without waiting for an AI turn to finish or for a first upstream business frame. The interval SHALL be configurable independently from SSE heartbeat timing. Proxy-originated downstream Ping/Pong exchanges SHALL terminate on the downstream leg; the proxy SHALL NOT require an upstream Pong to keep the downstream connection alive. It SHALL distinguish a heartbeat attempt, a completed Ping/Pong round trip, and application progress.

#### Scenario: Upstream is silent before its first event
- **GIVEN** both WebSocket handshakes succeeded and the client remains responsive
- **WHEN** the upstream sends no application event for at least three heartbeat intervals
- **THEN** the client observes repeated Ping frames, the proxy observes matching Pongs, and the connection remains usable

#### Scenario: Upstream does not read during a turn
- **GIVEN** the upstream reads no client messages while processing an existing turn
- **WHEN** it produces no downstream business data for at least three heartbeat intervals
- **THEN** downstream heartbeat continues without requiring upstream reads or Pongs

#### Scenario: Business data resumes after silence
- **WHEN** the upstream resumes output after a heartbeat-protected silent interval
- **THEN** the application receives that output unmodified and can continue on the same WebSocket

### Requirement: Backpressure is bounded without false peer-failure diagnosis

The relay SHALL use bounded buffering and documented I/O limits rather than memory proportional to arbitrary message size or an unbounded message queue. It SHALL preserve backpressure and SHALL NOT discard data to service heartbeat. A Pong that cannot yet be processed because a relay is backpressured SHALL NOT by itself be reported as proof that the client disconnected. A missing matching Pong alone SHALL be recorded as an unconfirmed probe, not used as the sole reason to terminate an otherwise usable connection. Actual read/write failure, protocol violation, shutdown, or an approved relay-stall limit SHALL end the session with an accurate cause classification.

#### Scenario: Slow upstream delays downstream reads
- **WHEN** forwarding client data encounters upstream backpressure and Pong processing is delayed
- **THEN** the proxy neither drops pending client data nor labels that delay a confirmed client disconnect
- **AND** if the configured relay-stall limit is reached, termination is reported as a relay I/O limit rather than a missing-Pong diagnosis

#### Scenario: Peer stops accepting writes
- **WHEN** a peer cannot accept further forwarded bytes within the configured I/O budget
- **THEN** the relay ends within a bounded interval and releases both legs without growing an unbounded queue

#### Scenario: Invalid limits are configured
- **WHEN** heartbeat or I/O configuration is invalid or internally inconsistent
- **THEN** startup fails with a non-secret configuration error rather than starting an uncontrolled loop

### Requirement: Connection lifecycle remains visible and finite

Valid close codes SHALL be propagated with existing disclosure policy; reserved non-transmittable codes SHALL NOT be written on the wire. Abrupt upstream loss SHALL remain an abnormal transport outcome rather than synthetic application success or an indefinitely heartbeating orphan. Client cancellation/disconnection SHALL propagate to the upstream connection without moving Sub2API's drain or billing policy into the proxy. Graceful service shutdown SHALL explicitly account for upgraded connections and force release after the documented grace budget. Normal completion of one AI turn SHALL NOT be treated as completion of a reusable WebSocket session.

#### Scenario: Upstream closes cleanly
- **WHEN** the upstream sends a valid close frame
- **THEN** the downstream observes a compatible closure and the heartbeat loop and both relay directions stop

#### Scenario: Upstream disappears abruptly
- **WHEN** the upstream transport ends without a valid close handshake
- **THEN** the client observes a transport failure, not a successful synthetic business completion
- **AND** the proxy stops heartbeat and releases the connection rather than sending code 1006 as a wire close frame

#### Scenario: Client cancels mid-turn
- **WHEN** the client closes during an active response
- **THEN** the proxy releases its connection state and signals the upstream transport
- **AND** existing Sub2API cancellation/drain/billing behavior remains independently verifiable

#### Scenario: Service receives a shutdown signal
- **WHEN** the proxy shuts down with active HTTP/SSE and upgraded WebSocket connections
- **THEN** all connection classes are accounted for and the process exits within the documented grace/force-close budget

### Requirement: Heartbeat evidence excludes credentials and application data

Diagnostics SHALL distinguish successful upgrade, confirmed heartbeat round trip, unconfirmed probe, relay/backpressure failure, peer closure, and local shutdown. They SHALL be correlatable using locally generated non-secret connection identifiers and bounded counters/timing, without logging authorization headers, query secrets, session identifiers, prompt/output bodies, or unfiltered peer close reasons. Heartbeat telemetry SHALL NOT be presented as proof that an AI request completed.

#### Scenario: A connection experiences repeated silent intervals
- **WHEN** it later closes
- **THEN** bounded diagnostics can show heartbeat attempts, confirmed round trips, probe failures, duration, and closure category for that connection
- **AND** no application payload or credential is included

### Requirement: Published source and deployed image are traceable

The delivered proxy source SHALL be reachable in its source repository and pass the existing formatting, static-analysis, and race-test gates before publication. The AI-gateway gitlink SHALL reference a published proxy commit. Production SHALL select an immutable, verified image identity built from the approved source, not a floating tag or an unreviewed local build. Operator private configuration SHALL remain excluded from publication. A repository push or successful image build SHALL be recorded separately from deployment and behavior acceptance.

#### Scenario: Source and integration revisions are published
- **WHEN** the implementation is released
- **THEN** evidence identifies the proxy commit, its exact-revision CI result, the image digest/platform identity, and the AI-gateway commit containing that proxy gitlink

#### Scenario: Only a build is complete
- **WHEN** source and image publication succeed but the live checks have not run
- **THEN** the report states publication succeeded and live behavior remains unverified

### Requirement: Proxy rollout uses scoped recoverable operations

Rollout SHALL use the existing Ansible operations entry point and reviewed private desired configuration, preserving the existing drift, approval, capacity, no-production-build, and recovery requirements. Exact candidate image availability SHALL be verified before serving disruption. The approved normal recreation set SHALL contain only the proxy application container; namespace owners, relays, Sub2API, and unrelated services SHALL remain unchanged. Recovery material SHALL identify the complete affected pre-deploy file set and prior image; rollback SHALL refuse unexplained later drift and SHALL report recovery-required if restoration cannot be verified. Application-only readiness and longer behavior acceptance SHALL have separate recorded outcomes.

#### Scenario: Remote private configuration drifted
- **WHEN** a relevant remote configuration file differs from its approved baseline and desired state
- **THEN** apply stops for reviewed reconciliation without overwriting state or bypassing the drift gate

#### Scenario: Candidate verification fails
- **WHEN** the candidate image or application readiness fails the approved deployment gate
- **THEN** serving state is preserved or the protected previous configuration/image is restored within the approved recovery scope
- **AND** the receipt records the actual outcome rather than a successful deployment based on container-name presence

#### Scenario: An already satisfied deployment is repeated
- **WHEN** desired files, image, effective configuration, and runtime state already match the verified deployment
- **THEN** the operation does not recreate the proxy or overwrite the earlier recovery material unnecessarily

### Requirement: Live acceptance demonstrates the actual client path

Acceptance SHALL include a deterministic real-WebSocket silence test, an approved public-ingress canary, and authorized real Responses client verification. The public-path silence duration SHALL exceed both the observed failure window and applicable ingress idle timeout; the initial planned observation window is 20 minutes and SHALL be extended if refreshed evidence shows a longer failure window. Evidence SHALL include repeated client-observed Ping frames, proxy-observed matching Pongs, unchanged connection identity, subsequent application-message delivery, and normal completion/continued use. Real Responses verification SHALL include multiple turns and cancellation/close behavior, with no proxy-synthesized business events. SSE/non-stream regression checks and unrelated-service invariants SHALL also pass. If any required probe is unauthorized, infeasible, or skipped, end-to-end acceptance SHALL remain pending.

#### Scenario: Deterministic silence test succeeds
- **GIVEN** an isolated non-billable upstream accepts a real WebSocket and intentionally sends no application data
- **WHEN** a responsive client remains connected through the approved public ingress for the complete observation window
- **THEN** repeated downstream heartbeat is observed and later sentinel messages in both directions are delivered correctly on the same connection

#### Scenario: A negative control cannot survive the idle window
- **WHEN** the same controlled idle-timeout fixture runs with keepalive disabled
- **THEN** it demonstrates the expected idle disconnection or is reported as an inconclusive fixture rather than a valid red/green proof

#### Scenario: Real client and backend complete multiple turns
- **WHEN** an authorized low-cost Responses session runs through the production path
- **THEN** real client parsing, session reuse, terminal events, cancellation, and Sub2API usage/drain behavior are verified separately from heartbeat

#### Scenario: Health and HTTP 101 pass without a long-silence probe
- **WHEN** container readiness and upgrade succeed but the required silence/real-client evidence is missing
- **THEN** the outcome is readiness-only or acceptance-pending, not 'working fine'

#### Scenario: Bounded observation reports no recurrence
- **WHEN** no unexpected 1006 occurs during the documented post-deployment observation window
- **THEN** the conclusion is scoped to the tested route, client, configuration, traffic, and time window
- **AND** it does not claim that all possible future code-1006 failures have been eliminated
