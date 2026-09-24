## Why

Long silent Responses WebSocket turns can lose their downstream connection, and the locally added Sub2API ingress heartbeat cannot reliably verify Pong because it calls `coder/websocket.Conn.Ping` without a concurrent reader during a turn. Move downstream WebSocket keepalive into the existing fixed-upstream SSE proxy, where connection handling can remain independent of AI turn processing; validate whether this prevents the observed idle disconnections rather than assuming every close code 1006 has the same cause.

## What Changes

- Add an opt-in RFC 6455 WebSocket relay and downstream Ping/Pong keepalive to `ai-sse-keepalive-proxy`, reusing the existing service, fixed upstream, network isolation, and public ingress path. Preserve ordinary HTTP, SSE, non-stream 103 heartbeat, and non-WebSocket Upgrade behavior.
- Keep Sub2API responsible for authentication, authorization, quotas, billing, session state, retries, and WS/HTTP/SSE bridging. The proxy must not parse or synthesize Responses business events, drop application messages, replay requests, or select another backend.
- Establish the upstream WebSocket before committing downstream HTTP 101; preserve handshake rejection behavior, relevant headers, negotiated subprotocols, application message types/order/content, and protocol-correct closure. Handle control frames independently on each leg without relying on Sub2API answering a proxy-originated Ping.
- Add bounded I/O/backpressure and explicit shutdown handling for upgraded connections, with sanitized connection/heartbeat diagnostics. Successful Ping writes, received matching Pongs, application completion, and connection closure remain distinct evidence.
- Extend real WebSocket tests beyond the existing generic `testproto` Upgrade test. Cover silent upstreams, subsequent messages and turns, handshake failures, large/fragmented data, slow peers, close/cancel behavior, and HTTP/SSE regressions.
- Complete the delivery path: publish reviewed source and passing CI in `xz-dev/ai-sse-keepalive-proxy`, identify an immutable image, update the AI-gateway gitlink and appropriate public integration files, adopt only approved private deployment fields, and deploy through Ansible without building on production.
- Require post-deployment provenance, actual public-path WebSocket behavior, bounded long-silence verification, and scoped recovery evidence before reporting the feature operational. Container health, HTTP 101, Git push, or an image build alone are insufficient.
- After proxy verification, assess disabling the old Sub2API downstream heartbeat through existing configuration. Removing fork patches or rebuilding Sub2API is **not** an implicit part of this change; it requires a separately approved scope if configuration cannot safely retire that behavior. Preserve PR7157, PR7217, and existing upstream-drain/billing semantics.

## Capabilities

### New Capabilities

- `websocket-transport-keepalive`: Opt-in fixed-upstream WebSocket relay, downstream control-frame keepalive independent of AI turns, compatible lifecycle and bounded resource behavior, and release/live-acceptance evidence for the existing SSE proxy.

### Modified Capabilities

None. Existing `production-operations-control`, `production-ops-tooling`, `default-egress-policy`, `client-visible-upstream-errors`, and `session-affinity-propagation` requirements remain constraints on the implementation and rollout; this change does not relax or replace them.

## Impact

- Primary source repository: `https://github.com/xz-dev/ai-sse-keepalive-proxy`, consumed at `middleware/ai-sse-keepalive-proxy/`. The analyzed gitlink worktree is `a7ffc2f5b00dd2757b20819bbab8e420e176f660`; the separate local checkout at `/home/xz/Code/ai/ai-sse-keepalive-proxy` was older and is not the analyzed baseline.
- Expected proxy files: `main.go`, a small dedicated WebSocket implementation and its tests, `main_test.go`, `go.mod`/`go.sum`, `Dockerfile`, and `README.md`; reuse the existing CI/publishing workflow unless a demonstrated change is needed. Use a maintained RFC 6455 library rather than a handwritten frame parser.
- Integration repository: AI-gateway `ff82a4d890e07bc57915d128386c22e8783dd459`, its submodule pointer, narrowly necessary Compose/Ansible configuration and operation checks, and public documentation/examples. Preserve pre-existing unrelated `compose.yaml` resource-limit edits.
- Production desired state: the gitignored `ansible/private-config/` tree, not repository-root `.env`. Target remains `rainyun-la`, `/root/AI-gateway`, Compose project `ai-gateway`. Normal rollout replaces only the proxy application container, not namespace owners or relays; any expanded recreation scope requires a revised approval.
- A proxy restart affects existing proxied HTTP/SSE/WS connections. Deployment approval must include interruption, exact artifact/effective configuration, safe verification traffic, and recovery scope. Billable inference or temporary canary routing requires explicit approval.
- This proposal authorizes planning artifacts only. No implementation, Git commit/push, image publication, production deployment, or live inference has been performed by this change. Acceptance examples are proposed for operator review; execution starts only after a new apply request.
