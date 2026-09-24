# Implementation-time verification — add-websocket-keepalive-to-sse-proxy

## Source / CI / image identities

| Item | Value |
|---|---|
| Proxy source | `xz-dev/ai-sse-keepalive-proxy` main @ `f2e420ca04c8e4e2f949c647041ad74d720557d8` |
| Commits | `bae7935` WS terminate+keepalive · `8b19df2` shutdown-window races · `4467d5a` close-code fidelity · `f2e420c` transparent permessage-deflate |
| Tests | 61 green with `-race` (58 + 3 compression-passthrough subtests); `gofmt`/`vet` clean |
| CI run | `35511338156` (test + image jobs, 4m10s) — success |
| Published image | `ghcr.io/xz-dev/ai-sse-keepalive-proxy:f2e420ca04c8e4e2f949c647041ad74d720557d8@sha256:21f153655b48121ba808204dcb0396398155105e27c6944415a096027999bd2c` (multi-arch index) |
| amd64 manifest | `sha256:44a06a638cb2681fa13461a94624b3be0efad4bb170f9a03732c19c38fa9e824` |
| OCI revision label | `f2e420ca04c8e4e2f949c647041ad74d720557d8` (verified via registry API) |

## Deployment evidence

| Item | Value |
|---|---|
| Host | `rainyun-la`, `/root/AI-gateway`, project `ai-gateway`, docker compose |
| Operation | `deploy-ws-keepalive` tag, `ai_ops_mode=plan` then `apply` |
| Approved digest | `sha256:21f153655b48121ba808204dcb0396398155105e27c6944415a096027999bd2c` |
| Expected revision | `f2e420ca04c8e4e2f949c647041ad74d720557d8` |
| Success receipt | `receipt-ws-keepalive-1789910906.txt` — verified_container `94d26a70d0af`, verified_image `sha256:21f153…`, verified_revision `f2e420c`, ready_attempts=1 |
| Running container | `94d26a70d0af`, image index digest `sha256:21f153…`, `Up (healthy)` |
| Live env | `WS_PING_INTERVAL=15s WS_PING_TIMEOUT=5s WS_WRITE_TIMEOUT=120s WS_HANDSHAKE_TIMEOUT=10s` |
| Protected | `ai-sse-keepalive-proxy-netns` (34h, unchanged), `apisix-ai-sse-relay`, `ai-sse-sub2api-relay`, `sub2api` — IDs unchanged |
| Idempotency | third apply = clean no-op (`.env: no effective change`, drift gate quiet) |
| Recovery (real) | first apply rescued on a `RepoDigests`-on-container bug — pre-deploy `.env` restored, old container reactivated, honest `recovery-required` report |

## Acceptance evidence (LIVE)

- **LIVE-03 (ops)**: deploy records exact runtime image/revision/config; readiness verified; protected container IDs unchanged; recovery exercised for real.
- **Real-traffic observation (~20 min)**: ~40 WS sessions through public ingress, real Responses payloads ~1MB, durations 10–38s. 5 sessions had upstream silence >15s; each logged `pings=1 pongs=1 missed=0` — the downstream Ping fired during the silent gap, the client Ponged, the session completed on the same connection. Zero `missed`, zero abnormal-close signatures.
- **Suppression correctness**: sessions with continuous application flow logged `pings=0` — recent-write suppression working as designed.
- **Fixture matrix** (`scripts/test-ansible-ws-keepalive.sh`): plan-no-mutation, unapproved-digest refusal, scoped recreate, protected-drift rejection, no-op idempotency, candidate-unready recovery (`recovery verification=passed`), recovery-fails escalation (`recovery-required`), stale-runtime convergence. All green.

## Skipped / deferred gates

- **8.1/8.2/8.3** (isolated real-WS topology rehearsal, 600s idle-policy rehearsal, public-path canary hostname): superseded by direct production deploy authorized by the operator; the ~20-min real-traffic window provides equivalent or stronger evidence than a synthetic canary for the hypothesis under test.
- **9.3** (explicit multi-turn/cancel Responses client script): real production traffic already exercises initial turn + silent gap + next turn through the same ingress; explicit cancel-path not separately scripted — **partial**.
- **9.5** (scoped recovery execution): only exercised implicitly by the first failed apply's real rescue; no deliberate rollback run — evidence is real but single-instance.
- **9.6** (sub2api keepalive retirement): deferred pending proxy acceptance observation period + bypass-ingress inventory; no sub2api change made.

## Residual risks

- The identity-comparison chain (manifest digest → image `.Id` → container `.Image`) needed four real-docker falsifications before it was right; the fixture cannot distinguish index vs config vs manifest digests — hardened but not foolproof.
- `WS_PING_INTERVAL=15s` keeps transport alive during silence; whether it eliminates the specific production 1006s is now testable but only confirmed over a longer observation window (the 20-min sample showed zero abnormal closes, but the original defect manifested over longer AI thinking turns).
- Compression passthrough verified at handshake level (`Sec-WebSocket-Extensions` observed); end-to-end compressed frame flow through production not separately instrumented.
