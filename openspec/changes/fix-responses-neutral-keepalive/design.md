## Context

See `proposal.md` for motivation and `specs/responses-sse-keepalive/spec.md` for the behavior contract. Design is needed because a small wire-format repair crosses a published submodule, a production image pin, existing WebSocket maintenance fixes, and ingress/client compatibility gates.

Verified baseline on 2026-11-15:

- The gateway checkout is `204c8b4`; its proxy gitlink is `1c489390927a986b7aacd3ebdfef3ea33f51d15d`.
- The running proxy is instead maintenance revision `5ff3e338b1ed301ce72832b9e06893e9975dbd51`, image digest `sha256:b12366714e08463765266ec60a1f402d045febd4955edcbef590ddffc2bf20ec`. It is a descendant of that gitlink and includes additional WebSocket/backpressure fixes. The gateway checkout alone is therefore not a safe production source baseline.
- On that exact running revision, `main.go:824` returns `data: {"type":"response.in_progress"}\n\n` for Responses heartbeats. `openSlow`, the delayed-response path, and idle forwarding share that heartbeat helper. Chat already uses `: keepalive\n\n`.
- Production uses `HEADER_WAIT=10s` and `IDLE_INTERVAL=15s`. A raw public-path probe observed a type-only synthetic progress event at 10.478 s, real creation at 15.659 s, and real completion at 16.194 s. The upstream need not lose any event for the SDK to fail.
- A deterministic replay through the Bot's real adapter with OpenAI Python SDK 3.49.0 and httpx2 2.16.0 passes a valid creation/completion stream, fails when the proxy heartbeat is prepended, and passes with a comment prefix.
- Existing `TestSlowStartupAllShapes` expects the fabricated progress event. The proxy README describes it as the protocol-visible heartbeat; both need correction. Its Chat heartbeat table is also stale relative to the current comment implementation.
- `docs/records/keepalive-header-wait-10s-deployment-2026-10-01.md` explains the existing early-flush constraint: the public `Accept: application/json` path failed around a 30 s first-byte wait. This repair must preserve early output, not raise that timeout.

## Goals / Non-Goals

**Goals:**

- Use one transport-only heartbeat representation for Responses startup and idle silence.
- Prove both the server's wire behavior and a real client's parsing against a delayed valid upstream.
- Deliver a fixed immutable image without losing behavior already deployed from the maintenance line.

**Non-Goals:**

- No new heartbeat mode, feature flag, event repair state machine, or response buffering architecture.
- No change to the Bot, native provider events, response/session identity, routing, credentials, timeouts, memory settings, or existing WebSocket maintenance behavior.
- No claim that comments reset every application's own event-level idle timer, or that this repair fixes separate provider/CPA premature termination.

## Decisions

### 1. Reuse the existing SSE comment heartbeat for all Responses heartbeat sites

Change the Responses case in `keepaliveFrame` to the same comment representation already used for Chat. Both startup and post-creation idle calls use it; do not keep a second business-event heartbeat after creation. Preserve the existing writers, flushes, timers, and terminal detector. Real provider events and existing safe synthetic terminal errors are not heartbeat events and retain their current handling.

Alternatives rejected: inventing a `response.created` requires fabricating response identity and state; teaching the Bot to ignore malformed progress hides a gateway defect for only one client; raising `HEADER_WAIT` recreates the ingress exposure; using different startup/idle event types introduces unnecessary state and inconsistent semantics.

### 2. Base the fix on the running maintenance line, not the older root gitlink

Create the source repair from the verified running `5ff3e338` baseline, or a reviewed descendant that demonstrably retains its fixes. Review the candidate against the actual live baseline. Preserve `maintenance/transparent-ws-write` behavior and its publication safeguards; do not replace it with an image built from the older integration gitlink merely because it is checked out.

Use the existing maintenance publication workflow on that line for a reviewed new commit: `.github/workflows/publish-maintenance-transparent-ws-write.yml` already tests exact source, guards immutable tags, publishes the full-SHA image, and verifies OCI source identity. Any publication-path adjustment needs its own reviewed diff; never overwrite the existing SHA tag. Update the AI-gateway gitlink and immutable image reference together after the fixed source is published.

### 3. Add a deterministic parser regression at the real HTTP/SSE seam

Extend existing Go cases for delayed response headers, immediate headers with delayed first body, supported path aliases, post-creation idle intervals, terminal suppression, and cancellation. Assert comments and unchanged real events rather than only checking for any output.

Add a small test-only Python SDK check against the actual candidate proxy and a controlled valid upstream. Pin the diagnosed SDK/transport pair (3.49.0/2.16.0) in a task-scoped environment and run it in source CI before image publication; no Python dependency enters the production image. The fixture covers startup silence and idle silence after real creation, then verifies a completed parsed response. On the unfixed image it must reproduce the exact missing-creation RuntimeError. Use the same valid upstream response with and without the proxy heartbeat as the ablation; do not depend on paid generation or random slowness for the regression.

Retain the existing Go formatting, vet, race tests, build, and maintenance immutable-tag/backpressure gates. A substring-only heartbeat test or a manually tolerant decoder is not sufficient.

### 4. Separate transport compatibility from client application timers

Ordinary SSE comments are ignored by the event parser while still producing transport bytes. Check the deployed Bot SDK and the selected deployed Codex/Hermes Responses clients, recording their versions and whether comment-only startup and post-creation silence is tolerated. A client-specific timeout failure blocks that client's compatibility claim; do not silently restore invented progress to make its timer reset.

Keep the existing fast-response, background passthrough, non-stream 103, Chat, Anthropic, late-error disclosure, and WebSocket regressions. Update the README's Responses row and explanations; reconcile its already-stale Chat row without changing Chat wire behavior.

### 5. Keep public-ingress acceptance and production activation explicit

Use an authorized bounded canary with `Accept: application/json` and a controlled valid upstream whose first real event is delayed beyond both the configured startup wait and the prior approximately 30 s first-byte window. Verify the flushed comment reaches the client and the real SDK subsequently consumes the terminal result. Use an isolated approved fixture; do not infer authorization to add production test routes or create oversized/billable stress requests. A fast catalog probe, HTTP 200, or an inconclusive fast generation does not satisfy this gate.

Source/image publication, application readiness, public-path acceptance, and selected-client acceptance are separate receipts. If a required live probe lacks authorization or access, leave it pending rather than treating local success as production proof.

## Risks / Trade-offs

- **An event-level client idle timer ignores comments** → Record actual client behavior and keep its acceptance pending on failure; resolve any required client policy separately instead of forging model progress.
- **A hop buffers or consumes comments** → Test the actual public ingress with the affected Accept header and a delayed valid response; preserve the 10 s private startup setting and existing flush behavior.
- **The stale integration baseline removes live WebSocket fixes** → Require candidate ancestry/equivalence evidence against `5ff3e338`, retain maintenance gates, and review the full image-to-image delta.
- **Existing tests validate the defective heartbeat** → First make the neutral-heartbeat and real-SDK cases fail on the verified live source, then require them to pass on the repair.
- **A proxy restart interrupts active streams** → Use the existing single-writer, low-traffic, application-only approved window, with protected rollback material and an honest interruption record.
- **Other upstream failures continue** → Report provider/CPA missing-terminal failures and primary connectivity separately; comments neither repair those faults nor prove model progress.

## Migration Plan

1. Implement and review the proxy repair and tests on the preserved maintenance baseline. Run all source/CI gates and publish a new immutable commit image through the reviewed workflow.
2. Advance the AI-gateway gitlink and approved image pin to that source/image pair; run integration pin/topology checks and strict OpenSpec validation. Do not copy the public template over private desired state.
3. Obtain separate production-window approval. Preview `playbooks/deploy.yml -e component=ai-sse-keepalive-proxy --check --diff` from `ops/ansible/`; verify the exact candidate image and a proxy-only recreation plan, with no timing/topology drift.
4. Apply only with the reviewed approval digest, existing capacity/drift/recovery gates, and protected prior files/image. Preserve namespace owners, relays, keeper, bridge, Sub2API, and every unrelated container.
5. Read back image/revision/settings, health and unrelated container identities; run the authorized public/SDK/client checks and record only non-secret timing/event metadata under `docs/records/`.
6. On change-caused failure, use the existing freshly previewed and approved component rollback, not a manual full-stack restart. The previous image restores the known heartbeat defect, so record rollback as recovery to the prior baseline, not successful resolution.

No implementation, source publication, image publication, or production activation is performed by creating this plan.
