# middleware

**Where:** `services/middleware/`. All three services run in the shared `ai-sse-keepalive-proxy-netns` namespace on the request path: front door → keepalive → keeper → bridge → Sub2API.

**What:**

| Directory | Compose service | Role |
|---|---|---|
| [`ai-sse-keepalive-proxy/`](https://github.com/xz-dev/ai-sse-keepalive-proxy) (submodule) | `ai-sse-keepalive-proxy` | Sends heartbeats on silent AI SSE streams; operator entrance `127.0.0.1:8087` |
| [`openai-api-cache-keeper/`](https://github.com/xz-dev/openai-api-cache-keeper) (submodule) | `openai-api-cache-keeper` | Keeps Responses history and replays background responses |
| [`responses-ws-sse-bridge/`](responses-ws-sse-bridge/README.md) | `responses-ws-sse-bridge` | Terminates WebSocket and turns each turn into HTTP/SSE |

The two submodules keep their own READMEs upstream. Clone with `--recurse-submodules`, or run `git submodule update --init`.

**How:** chain position, ownership and Pi Codex setup: [responses path](../../docs/responses-path.md). WebSocket design: [WebSocket termination](../../docs/websocket-termination.md). Guarded keeper and bridge deploys: [Ansible operations](../../docs/ansible-operations.md#guarded-component-rollout).
