## Purpose

Keep Responses SSE connections active without inventing application progress or invalidating a client's response state, while preserving upstream results, existing error handling, and the public ingress's timely first-byte behavior.

## ADDED Requirements

### Requirement: Responses heartbeats are transport-only comments

For eligible JSON POST streaming requests on `/v1/responses`, `/responses`, and `/backend-api/codex/responses`, the proxy SHALL use SSE comment frames for its startup and idle heartbeats. It SHALL NOT synthesize `response.created`, `response.in_progress`, or a successful response outcome as a heartbeat. Real upstream business events SHALL retain their content and order. A comment heartbeat SHALL indicate transport activity only, not model progress or request completion.

#### Scenario: Upstream is silent before response creation
- **GIVEN** an eligible request whose upstream has not emitted `response.created`
- **WHEN** startup heartbeat becomes due
- **THEN** the downstream receives a flushed SSE comment rather than a Responses business event
- **AND** the first downstream Responses business event remains the upstream's real first event

#### Scenario: Each supported Responses path is used
- **WHEN** an eligible streaming POST uses any of the three supported Responses paths
- **THEN** startup and idle heartbeats follow the same transport-only comment rule

### Requirement: Startup heartbeat retains the configured first-byte protection

The proxy SHALL preserve the configured startup waiting threshold, early HTTP 200 SSE commitment, and prompt heartbeat flushing when an eligible upstream remains silent. An upstream that returns before that threshold SHALL retain the existing fast-response behavior. This repair SHALL NOT increase the waiting threshold or remove startup flushing to avoid the parser error.

#### Scenario: Upstream response headers are delayed
- **WHEN** the upstream sends no response headers before the configured startup threshold
- **THEN** the proxy commits its existing SSE response and flushes a comment at that threshold without waiting for response creation

#### Scenario: Headers arrive but first SSE body bytes are delayed
- **GIVEN** the upstream has returned valid SSE headers but no body bytes
- **WHEN** the configured startup threshold is reached
- **THEN** the downstream receives a flushed comment and can later receive the upstream's real events

#### Scenario: Upstream responds before the threshold
- **WHEN** the upstream supplies its response before startup heartbeat is due
- **THEN** its status, applicable headers, and body follow the existing fast-response path without an extra startup business event

### Requirement: Idle heartbeats do not replace application events

After forwarding upstream events, the proxy SHALL use comments for each due idle heartbeat under the existing idle interval. It SHALL NOT treat these comments as upstream response creation, output, usage, terminal success, or terminal failure. Frequent upstream body activity SHALL retain the existing heartbeat-suppression behavior.

#### Scenario: An active response pauses after creation
- **GIVEN** the client has received a real `response.created`
- **WHEN** upstream body silence spans multiple idle intervals before output resumes
- **THEN** the client receives comment heartbeats during that silence and resumes consuming the same real response without synthetic progress events

#### Scenario: Upstream keeps producing data
- **WHEN** upstream body activity occurs more frequently than the idle interval
- **THEN** the proxy does not inject unnecessary idle heartbeats

### Requirement: Existing failure and request-boundary semantics remain intact

The heartbeat repair SHALL preserve existing terminal-event detection, safe late-error handling, client cancellation, and cleanup. A complete terminal event SHALL suppress later proxy heartbeats under the existing rules without discarding trailing upstream bytes. Heartbeats SHALL NOT convert premature termination into success. Background/resume passthrough, non-stream HTTP, Chat Completions, Anthropic, and WebSocket behavior SHALL remain unchanged.

#### Scenario: A real terminal event arrives
- **WHEN** the upstream sends `response.completed`, `response.failed`, `response.incomplete`, or its existing terminal error
- **THEN** it is forwarded under existing behavior and no subsequent proxy heartbeat invents further response progress

#### Scenario: An upstream error arrives after commitment
- **GIVEN** the proxy already committed HTTP 200 with a startup comment
- **WHEN** the upstream later returns an error
- **THEN** existing bounded safe in-band error handling remains effective instead of a fabricated completion or an indefinitely heartbeating response

#### Scenario: The client cancels while the upstream is silent
- **WHEN** the client disconnects during startup or idle silence
- **THEN** existing cancellation and cleanup release the request without another heartbeat or generation attempt

#### Scenario: An ineligible request shares the proxy
- **WHEN** a background/resume request, non-stream request, another supported API protocol, or a WebSocket upgrade arrives
- **THEN** it retains its existing routing, heartbeat, error, and lifecycle behavior

### Requirement: Acceptance verifies parsing as well as ingress liveness

Acceptance SHALL demonstrate that a supported real Responses SDK consumes a valid upstream response after startup and idle comment heartbeats without a response-state exception. It SHALL also verify an authorized public-ingress path using `Accept: application/json`, with first real upstream output delayed beyond the configured startup threshold and the historically observed ingress first-byte failure window. Network heartbeat delivery SHALL NOT be reported as proof that a client-specific application-event idle timer is reset. Missing public-path or selected-client evidence SHALL remain explicitly pending.

#### Scenario: A real SDK consumes a delayed valid response
- **GIVEN** a deterministic upstream fixture emits valid creation and completion events after startup silence
- **WHEN** the real SDK consumes the proxied stream containing heartbeat comments
- **THEN** it returns the real completed response without a missing-creation error or an extra generation attempt

#### Scenario: The public path has a long initial silence
- **GIVEN** an authorized bounded canary uses the actual ingress behavior and `Accept: application/json`
- **WHEN** first real upstream output is held beyond the startup threshold and the previously observed first-byte failure window
- **THEN** flushed comments keep the transport usable and a real client can consume subsequent valid response events to their terminal outcome

#### Scenario: A client has its own application-event timeout
- **WHEN** compatibility is checked for a selected client with a timeout above the SSE transport reader
- **THEN** the result separately records whether that client tolerates comment-only silence
- **AND** a failure or missing check is not hidden by claiming that synthetic model progress is required

### Requirement: Release preserves the deployed service baseline

The fixed source and immutable image SHALL retain already deployed WebSocket and other unrelated fixes. Rollout SHALL use the existing reviewed single-component preview, approval, deployment, and rollback process, without production builds, Compose topology changes, or unrelated-container recreation. Source publication, image publication, application readiness, and end-to-end behavior acceptance SHALL be reported separately.

#### Scenario: The integration gitlink is older than the running image
- **WHEN** the fixed candidate is prepared
- **THEN** its source preserves the running image's unrelated behavior instead of silently reverting it to the older gitlink baseline

#### Scenario: A production window is approved
- **WHEN** the immutable fixed image is activated
- **THEN** only the approved keepalive application container is recreated and existing protected rollback material and unrelated-container identity checks are retained
