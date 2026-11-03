## MODIFIED Requirements

### Requirement: Backpressure is bounded without false peer-failure diagnosis

The relay SHALL use bounded streaming buffers rather than memory proportional to arbitrary message size or an unbounded message queue. It SHALL preserve backpressure and SHALL NOT discard data to service heartbeat. A Pong that cannot yet be processed because a relay is backpressured SHALL NOT by itself be reported as proof that the client disconnected. A missing matching Pong alone SHALL be recorded as an unconfirmed probe, not used as the sole reason to terminate an otherwise usable connection. A pipe-local destination-write or relay-stall timer SHALL NOT terminate an established session. Actual peer termination, read/write failure, protocol violation, client cancellation or operational shutdown SHALL end the session with an accurate cause classification.

#### Scenario: Slow upstream delays downstream reads
- **WHEN** forwarding client data encounters upstream backpressure and Pong processing is delayed
- **THEN** the proxy neither drops pending client data nor labels that delay a confirmed client disconnect
- **AND** elapsed backpressure alone does not terminate the session

#### Scenario: Peer stops accepting writes
- **WHEN** a peer temporarily cannot accept further forwarded bytes beyond the former I/O budget
- **THEN** the relay retains bounded streaming backpressure without an independently timed disconnect
- **AND** real peer termination or client cancellation releases both relay legs and blocked I/O

#### Scenario: Invalid limits are configured
- **WHEN** heartbeat configuration is invalid or internally inconsistent
- **THEN** startup fails with a non-secret configuration error rather than starting an uncontrolled loop
- **AND** a heartbeat probe deadline is not used as a replacement request or write-stall timeout
