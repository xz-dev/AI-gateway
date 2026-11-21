## Context

Front APISIX already sends access log output to stdout and warnings to stderr. In the pinned image, enabling info on the normal destination also records entire matched routes. The incident record documents an offline alternative: both deliberately interrupted transfers yielded native diagnostic IDs that matched their access records; neither the diagnostics nor the route text appeared in the ordinary log.

## Goals / Non-Goals

**Goal:** Establish whether the front logged a native timeout on the next naturally interrupted catalog response. Keep storage and observation time bounded.

**Non-goals:** Do not diagnose by generating requests, capture packets, persist raw datagrams, lengthen a timeout, alter route matching or restart unrelated services. No automatic mitigation even on a positive result.

## Decisions

- Keep stderr at `warn` and add an HTTP-context syslog `info` destination on a private Unix datagram socket. Read single datagrams, drop truncated, malformed or unrelated events, and require a native nginx timeout, versioned catalog GET and validated request ID. No general-purpose syslog forwarding or raw fallback. Protect the socket from other local writers. Cap event storage; overflow is loss of evidence, not a reason to keep unbounded logs.
- Use the normal front access record to verify HTTP 200 and ID after its buffer flushes. Classify the tunnel’s natural catalog EOF separately, joining by ID only where available. A path/time match alone cannot attribute one connection to another. Do not limit failure detection to cases that produced a timeout event.
- Before changing logging, verify disk headroom, actual log rotation, container identity, protected backups on controller and host, a fresh candidate diff, and a working rollback independent of the session. Give rollback a hard deadline from activation; do not leave an unbounded setup interval. Prefer a syntax-tested graceful reload over recreation. Read back effective logging and timeout, receiver health and unrelated container identities before starting the clock.
- On first confirmed failed transfer, expiry or collection fault, remove the extra destination before stopping the receiver. Verify baseline hashes and effective settings and remove transient scheduler, socket and process state. Detect intervening drift rather than overwriting another operator’s work. The window owner must be able to escalate a failed or unsafe restoration.

## Risks / Trade-offs

Datagrams can drop; a collection gap, unmatched failure or quiet window is inconclusive. A graceful reload can leave old workers serving existing connections. Record coverage start, drain state and any gaps. Do not claim the production failure is fixed or infer a write stall duration from the time between a front access record and an edge EOF.
