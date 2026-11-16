## Why

The Responses SSE keepalive proxy emits a fabricated `response.in_progress` before the upstream has sent `response.created`, causing the OpenAI Python SDK to abort otherwise valid responses and exhaust the Bot's fallback retries. A production trace and a deterministic SDK replay confirmed the defect after the host migration; moving the same image to a larger host did not change this protocol behavior.

## What Changes

- Replace proxy-generated Responses startup and idle business-event heartbeats with protocol-neutral SSE comments on `/v1/responses`, `/responses`, and `/backend-api/codex/responses`.
- Preserve timely header commitment and flushing, existing `HEADER_WAIT`/`IDLE_INTERVAL` settings, and real upstream event bytes and ordering; do not fabricate a replacement `response.created`.
- Keep existing terminal/error handling, cancellation, background passthrough, Chat Completions, Anthropic, non-stream HTTP, and WebSocket behavior unchanged.
- Add delayed-startup and idle regressions, including a real OpenAI Python SDK compatibility check and a bounded public-ingress check with `Accept: application/json`.
- Correct the proxy documentation, publish the fixed submodule revision and immutable image, and update the gateway integration through the existing single-component release process. Production activation requires a separately approved window.

## Capabilities

### New Capabilities

- `responses-sse-keepalive`: transport-only Responses SSE heartbeats that preserve upstream protocol state, with SDK compatibility and public-ingress acceptance criteria.

### Modified Capabilities

None. Existing WebSocket transport and client-visible upstream-error requirements remain in force; this change does not modify their contracts.

## Impact

- **Source:** `services/middleware/ai-sse-keepalive-proxy/main.go`, its Go tests, README, and a bounded SDK compatibility fixture/check in the proxy's source repository.
- **Integration:** the proxy gitlink, immutable image reference, and relevant topology/pin checks in AI-gateway; no Compose structure or network changes.
- **Operations:** `component=ai-sse-keepalive-proxy` preview/apply and rollback, with exact image verification and unrelated-container identity checks. No full-stack recreation or production builds.
- **Clients:** Responses consumers receive SSE comments rather than synthetic progress events. Comments prove transport activity, not model progress; actual SDK parsing and clients with application-event idle timers must be checked separately.
- **Out of scope:** Bot retries/error wording, primary-endpoint connectivity, CPA/Sub2API missing-terminal failures, memory limits, model routing, credentials, and account policies.
