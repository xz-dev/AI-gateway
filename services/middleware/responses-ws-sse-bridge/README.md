# responses-ws-sse-bridge

**Where:** Compose service `responses-ws-sse-bridge` in [`compose.yaml`](../../../compose.yaml). The image is built in its own repository ([xz-dev/responses-ws-sse-bridge](https://github.com/xz-dev/responses-ws-sse-bridge)) and pinned by `RESPONSES_WS_SSE_BRIDGE_IMAGE`; this directory has no build context. It shares the `ai-sse-keepalive-proxy-netns` namespace and listens on `127.0.0.1:8086`.

**What:** Stateless WebSocket termination in front of Sub2API. Each WS `response.create` becomes one `POST /v1/responses` with `stream:true`, and each SSE event becomes one WS frame. Plain HTTP passes through to `UPSTREAM_URL` (Sub2API through `ai-sse-sub2api-relay`). Spools are bounded, disposable files in the `responses-ws-sse-bridge-spool` volume.

**How:**

- Chain position and ownership rules: [Responses path](../../../docs/responses-path.md).
- WS termination design, pinned versions and rollout: [WebSocket termination](../../../docs/websocket-termination.md).
- Health probe (`GET :8086/healthz`): [gateway health and recovery](../../../docs/gateway-health-and-recovery.md).
