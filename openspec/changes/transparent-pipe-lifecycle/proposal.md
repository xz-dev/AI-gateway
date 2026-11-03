## Why

Keeper currently couples unrelated turns through a single processing phase with an independent eight-second wait limit. SSE and bridge also impose established-WebSocket write deadlines and bridge admission limits. The owner confirmed these components are transparent pipes, not additional timeout or concurrency-policy owners; AISIX's intentional 120-second upstream-read policy and Squid policy must remain unchanged.

## What Changes

- Remove autonomous request/established-stream deadlines and arbitrary fixed global turn/socket admission limits from keeper, SSE keepalive and the WS-to-SSE bridge.
- Eliminate keeper's global materialization-phase head-of-line coupling, rather than merely deleting its timer or raising its capacity.
- Preserve bounded streaming, backpressure, cancellation, data integrity, continuation ownership and protocol ordering without inventing replacement time or count gates.
- Remove obsolete pipe timeout/concurrency configuration from source and gateway integration. Verify APISIX's real timeout-disable semantics before changing its configuration; do not substitute large finite values or silently fall back to defaults.
- Deliver small independently reviewed source slices, exact-revision CI and immutable images, then use protected staged rollouts.

## Capabilities

### New Capabilities

- `transparent-forwarding-lifecycle`: backend/client-owned lifecycle and cross-request independence for pure forwarding components, with explicit resource and cancellation acceptance.

### Modified Capabilities

- `websocket-transport-keepalive`: replace the former approved relay-stall termination rule with bounded backpressure that lasts until peer termination, real I/O failure, client cancellation or operational shutdown.

## Impact

- Source repositories: openai-api-cache-keeper, ai-sse-keepalive-proxy, responses-ws-sse-bridge. Gateway integration: source gitlinks where present, immutable image pins, obsolete environment settings, tests and sanitized records.
- Preserve AISIX's 120-second upstream-read timeout, Squid's policies, public ingress authentication/security policy, container memory/CPU limits, storage atomicity and per-WebSocket protocol ordering.
- No new dependencies, provider credentials, topology substitution, paid inference probes or Sub2API activation. CPA upgrade remains a separate already-authorized change with its own backup/activation gates.
- The owner accepted examples covering no pipe-invented timeouts, unrelated-session independence, more than the former 64 lightweight concurrent requests without count-based refusal, and prompt cancellation/resource cleanup. Per-component implementation plans and regression evidence precede each behavior slice.
