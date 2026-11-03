## Purpose

Keep pure forwarding components from becoming additional request timeout or concurrency-policy owners, while preserving bounded resource use, cancellation, data integrity and the owner's deliberate backend and security policies.

## ADDED Requirements

### Requirement: Pipes do not invent established-request deadlines

Keeper, SSE keepalive and the WS-to-SSE bridge SHALL NOT terminate an otherwise-valid established request or connection solely because a pipe-local queue, processing phase, upstream-silence timer or destination-write timer elapsed. They SHALL follow real peer termination, I/O failure, client cancellation and operational shutdown. Heartbeat probe deadlines SHALL NOT become session-eviction decisions.

#### Scenario: The backend remains silent while processing
- **GIVEN** an isolated non-billable backend remains connected and is still processing a valid request
- **WHEN** its delay exceeds a removed pipe-local timeout
- **THEN** the pipe does not independently return a timeout/capacity error or disconnect it
- **AND** the backend's eventual output is forwarded without replay

#### Scenario: A destination temporarily stops accepting bytes
- **WHEN** forwarding encounters destination backpressure beyond the former pipe-local write budget
- **THEN** forwarding remains backpressured with bounded buffering rather than timing out or dropping application data
- **AND** subsequent peer cancellation releases the blocked forwarding work

### Requirement: Unrelated sessions are not coupled through a global turn-processing gate

Pure forwarding components SHALL NOT serialize unrelated sessions through a global materialization token or reject valid lightweight traffic solely because a fixed global turn/socket count is reached. Necessary short storage-atomicity locks and per-WebSocket turn ordering SHALL remain distinct from global flow admission. Removing a count limit SHALL NOT replace it with unbounded whole-message buffering or a new time-based gate.

#### Scenario: A session's history processing is slow
- **WHEN** one session experiences slow history/storage work while an unrelated valid session progresses
- **THEN** the unrelated session is not rejected by a global phase-wait timeout
- **AND** storage ownership, atomic continuation acceptance and output ordering remain valid

#### Scenario: More than the previous socket count is active
- **GIVEN** an isolated backend can serve at least 65 lightweight concurrent sessions within the unchanged resource envelope
- **WHEN** those sessions use the pipe
- **THEN** the pipe does not reject the additional sessions solely because of the former 64-slot admission rule
- **AND** cancellation returns connection, spool and working-buffer resources

### Requirement: Lifecycle cleanup preserves deliberate policy and resource safety

The cleanup SHALL preserve AISIX's explicitly configured 120-second upstream-read policy, Squid's configured policies, public-ingress authentication/security controls and existing container resource limits. It SHALL preserve request/response integrity, owner isolation and client cancellation. A proxy that cannot express a true disabled duplicate deadline SHALL be reported as unresolved, not represented as fixed by a large finite value or a fallback default.

#### Scenario: AISIX reaches its intended deadline
- **WHEN** the approved AISIX upstream-read policy expires
- **THEN** that existing backend-policy outcome remains effective
- **AND** the pipe adds no independent competing request deadline or retry

#### Scenario: A stock proxy does not support a disabled timeout
- **WHEN** exact-version evidence shows no native way to disable a duplicate proxy deadline
- **THEN** the remaining limitation and any required structural change are presented for a separate decision
- **AND** zero, omission or a very large finite timeout is not claimed to mean disabled without supporting semantics

#### Scenario: Cancellation or resource regression occurs
- **WHEN** a client cancels under backpressure or a regression test exceeds the unchanged memory/resource envelope
- **THEN** resources are released promptly for cancellation
- **AND** a resource regression blocks publication/deployment rather than triggering a larger resource limit or hidden test waiver
