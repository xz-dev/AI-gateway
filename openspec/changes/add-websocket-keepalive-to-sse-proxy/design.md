## Context

See `proposal.md` for motivation and `specs/websocket-transport-keepalive/spec.md` for the behavior contract. This document is an implementation and delivery plan, not a record of successful implementation or production acceptance.

### Reviewed baselines and evidence

| Source | Verified current behavior | Design consequence |
|---|---|---|
| AI-gateway `ff82a4d890e07bc57915d128386c22e8783dd459` | Existing unrelated `compose.yaml` edits change egress-proxy/AISIX resource limits | Preserve those hunks; never stage the entire dirty file as this change |
| Proxy gitlink/worktree `a7ffc2f5b00dd2757b20819bbab8e420e176f660` | Contains route aliases and non-stream 103 heartbeat support | Start implementation from this version or an explicitly reviewed descendant, not the older separate local checkout `c427191` |
| Proxy `main.go:243-296` (`newProxy`, `ServeHTTP`, `inspect`) | Fixed `http` upstream, environment proxies disabled, automatic decompression disabled; WS GET falls through to `httputil.ReverseProxy` | Add an early, opt-in WebSocket dispatch without changing HTTP/SSE inspection |
| Proxy `main.go:90-117` (`serve`) | Uses `http.Server.Shutdown` with a 10-second budget | Explicitly track and close upgraded WS connections; HTTP shutdown does not own hijacked sockets |
| Proxy `main.go:781-853` (`joinURL`, header helpers, logging wrapper) | Existing target/header behavior and `Hijack`/`Unwrap` support | Reuse the transport/header conventions; ensure WS logs identify 101 rather than infer 200 from a hijack |
| Proxy `main_test.go:659` | `TestWebSocketStyleUpgradePassThrough` uses `Upgrade: testproto` and a line echo | Keep this regression test, but add real RFC 6455 clients/servers and control-frame observations |
| Proxy `.github/workflows/ci.yml` | PR/main CI already checks gofmt, `go vet ./...`, `go test -race ./...`; main image job depends on tests and publishes full-SHA tags, multi-platform images, revision labels and provenance | Extend existing tests and reuse the existing workflow; do not add a duplicate CI framework |
| Proxy `Dockerfile:4-5` | Copies only `go.mod main.go` into the builder | A new `websocket.go` or `go.sum` will otherwise be absent from the image build; update the copy/download inputs and verify the actual built image |
| `apisix/apisix.yaml:70-78,150-172` | Public WS routes already use the SSE proxy upstream; upstream read timeout is 600 seconds | No extra permanent network hop or public route is needed |
| `compose.yaml:499-563` and private counterpart | Proxy uses a separate netns owner and directed relays, 64 MiB memory limit, a local build context, and `pull_policy: build` | Retain topology; production must explicitly select a prebuilt immutable image and disable implicit builds |
| `ansible/roles/ai_ops/defaults/main.yml:35-45` | Component profiles do not include `ai-sse-keepalive-proxy` | Add this selected service to the existing operation surface |
| `ansible/ops.yml:356-452`, `deploy-file.yml` | Generic deploy uploads/baselines individual files before pull/up; runtime verification only checks service-name presence in `compose ps` | Merely adding a profile is insufficient for this rollout's artifact, readiness, and coherent recovery gates |
| Sub2API reference `backend/internal/service/openai_ws_forwarder_ingress.go:488-531,1988-2033` | Local mirror starts a downstream Ping loop; reads occur between turns; the loop returns on its first Ping error | Explains the local heartbeat defect, not every historical 1006 or an external middlebox's exact behavior |
| Sub2API reference `openai_ws_forwarder.go:366-384`, `config.go:1296-1299,1798-1799,2404,2416-2422` | Downstream Ping interval 0 disables that loop; default client read limit is 64 MiB, upstream read timeout 900 seconds, write timeout 120 seconds; env keys replace dots with underscores | Existing configuration can retire old downstream keepalive later without a source patch; preserve backend business timeouts |
| `coder/websocket` v1.8.14/v1.8.15 `conn.go`, `read.go`, `dial.go` | Ping waits for a matching Pong consumed by Reader; default read limit is 32 KiB; failed Dial retains only a 1 KiB diagnostic body | Maintain transport readers, set explicit read limits, and intercept non-101 HTTP responses before the Dial diagnostic path |

The code graph's metadata was stale for several files and unavailable for some Ansible files; these findings were checked against direct source. Failed delegated analysis runs supplied no successful review evidence. Primary references: [RFC 6455 control frames](https://www.rfc-editor.org/rfc/rfc6455.html#section-5.5), [NGINX WebSocket idle timeout](https://nginx.org/en/docs/http/websocket.html), and [coder/websocket v1.8.15 source](https://github.com/coder/websocket/tree/v1.8.15).

## Goals / Non-Goals

**Goals**

- Put downstream WS keepalive at the existing proxy's connection seam, independent of AI turn scheduling.
- Preserve the meaning of HTTP admission, message delivery, session reuse, cancellation, and failure.
- Bound memory and actual blocked I/O without imposing a new AI-generation or session-lifetime deadline.
- Deliver one traceable chain from published source through tested image and approved Ansible rollout to client-observed behavior.

**Non-goals**

- Moving Responses parsing, model routing, account/session affinity, retries, billing, or upstream drain into the proxy.
- Adding a new permanent service, arbitrary upstream selection, a new admin/metrics listener, custom WS framing, or another protocol-conversion layer.
- Renaming the repository/container/binary, upgrading unrelated dependencies, or refactoring all operations tooling.
- Removing PR7157/PR7217, rebuilding Sub2API, or disabling backend business idle/read timeouts.
- Claiming all 1006 causes are eliminated. Heartbeats cannot repair restarts, explicit backend limits, broken networks, or client/application failures.

## Decisions

### Review status

The operator selected **implementation in the existing proxy** and requested a detailed plan that includes source publication, deployment and real verification. Compatibility policies confirmed by the operator at apply time (task 1.1): generic RFC 6455 scope when enabled, disabled-by-default rollout, **no proxy-side message cap**, and **no missing-Pong-only eviction**. One policy was **revised on source evidence, then settled by the operator**: the operator initially chose to decline all extensions; on evidence that sub2api enables `CompressionContextTakeover` on both legs, the implementation briefly negotiated compression per-leg unconditionally; the operator then confirmed **transparent passthrough** as the final policy — permessage-deflate is offered on a leg only when the client offered it, preserving the end-to-end negotiation without widening or narrowing it. This also changes the proxy from an opaque Upgrade tunnel to two WS protocol endpoints; that compatibility responsibility is deliberate, not a claim of a zero-cost timer move.

### 1. One service, separate protocol handlers, disabled by default

Keep the existing `ai-sse-keepalive-proxy` name and deployment. In `ServeHTTP`, recognize a standards-compliant WebSocket Upgrade before body inspection and route it to `serveWebSocket` only when explicitly enabled. Apply this protocol behavior to WebSocket requests accepted by the fixed upstream, not a new proxy-maintained model/path allowlist. Non-WebSocket upgrades still use the existing fallback. Invalid WS handshakes fail as HTTP before an upstream session is unnecessarily established where validation permits.

Use `WS_PING_INTERVAL=0s` as the default/rollback-compatible disabled state. Production opts in at 15 seconds after staging. Retain SSE's existing `HEADER_WAIT`/`IDLE_INTERVAL`; neither changes WS behavior. An absent option must not accidentally enable a new connection policy on other installations.

**Rejected:** a separate WS container duplicates directed networks, lifecycle, and deployment; inserting Ping bytes into `ReverseProxy`'s opaque tunnel risks breaking frame boundaries and extensions; treating TCP keepalive as WS traffic does not reset an HTTP proxy's application read timer.

### 2. Use the maintained library and preserve the complete handshake contract

Use `github.com/coder/websocket` pinned to **v1.8.15**, the latest stable version returned by the public Go module proxy during this analysis. Verify checksum/advisory/Go compatibility at implementation time; a changed version requires rechecking the cited semantics. This introduces the first WS dependency to the otherwise standard-library proxy. Do not copy a frame parser or patch library internals. Gorilla supports write-only controls, but switching alone does not solve handshake-response preservation or lifecycle, and a second WS library is unnecessary.

Upstream-first handshake sequence:

1. Validate the incoming HTTP/1.1 WS method/version/key/Upgrade requirements without committing HTTP 101. Preserve URL escaping/query semantics using the existing reverse-proxy behavior as the oracle; do not blindly reuse `joinURL` where its `RawPath` behavior differs.
2. Build the upstream handshake against the **fixed** target. Reuse a direct `http.Transport` with environment proxying and automatic decompression disabled. Preserve supported authorization, session, beta, and forwarded headers under the current ingress trust contract. Strip hop-by-hop and old handshake-generated fields; let the WS library generate its own key/accept/upgrade fields. Forward the client's subprotocol offers explicitly.
3. Preserve the upstream HTTP rejection before `websocket.Dial` can truncate/close its body. Use one narrow per-dial RoundTripper wrapper around the shared transport: retain a non-101 response/body for ordinary HTTP forwarding and return a typed internal rejection signal to the dialing path. Keep its request context alive until the rejection has been forwarded and closed. Do not retry the handshake to retrieve an error body, buffer an entire error, expose raw library errors, or follow redirects. Cover this adapter with an error body exceeding 1 KiB, redirect, and cancellation tests.
4. Only after a valid upstream 101, copy allowed end-to-end response headers and accept the client with the exact upstream-selected subprotocol. Reject an unoffered selection. Regenerate handshake-only headers on each leg. If downstream acceptance fails, close the acquired upstream connection.
5. Preserve Origin at the upstream admission boundary. The downstream acceptor must not invent a Host/Origin rule based on the internal target hostname. If library local Origin checking is disabled to preserve transparent upstream admission, document that this disables only the redundant local Origin check, not authentication, target restrictions, or the public boundary. Verify with the deployed clients' actual Origin/Host combinations.

Negotiate compression **transparently**: offer `permessage-deflate` on the upstream leg only when the client offered it, and accept it on the downstream leg only when the client offered it. The proxy still terminates compression per-leg (it decompresses/recompresses application messages; it never copies raw compressed frames), but it neither widens nor narrows the negotiation the client and upstream would have made end-to-end. `CompressionContextTakeover` is offered when the client asks (the library falls back to `NoContextTakeover` per the normal negotiation). Never copy a `Sec-WebSocket-Extensions` result that this leg does not implement. sub2api accepts WS with `CompressionContextTakeover` and its own upstream dialer requests it (openai_gateway_handler.go:2341, openai_ws_client.go:129). flate context memory is bounded per connection (~64KiB each direction under context takeover) and covered by the existing 64 MiB container limit at accepted concurrency; if a client requires a non-optional extension other than permessage-deflate, that is a compatibility blocker to resolve before enabling the feature.

The WS HTTP client must not have a whole-session `http.Client.Timeout`. Bound connect/header acquisition (proposed 10 seconds) separately; never cancel a temporary dial context while a retained rejection body is still being forwarded.

### 3. Two streaming relay directions, not whole-message queues

Each connection pair has exactly one reader per leg and one application writer per destination. Use the library's `Reader`/`Writer` interfaces with bounded copy buffers (initially 32 KiB per direction); consume the current message to EOF and close its writer before starting the next. No full-message `Read`/`Write` buffering, unbounded queues, temporary per-turn readers, `CloseRead`, or discarded application frames.

- Keep application message type, order, boundaries, and bytes; wire masking/fragmentation may differ.
- Do **not** impose a proxy-side message size cap: both legs call `SetReadLimit(-1)` (operator decision: no proxy-side limit — the backend's own read limit remains authoritative). The relay streams, so memory stays bounded by copy buffers plus flate contexts, not by message size. Never preallocate message-size buffers.
- Keep backpressure: if the next peer cannot consume bytes, forwarding pauses rather than accumulating entire messages. A reader can therefore be temporarily occupied by a backpressured copy; it is independent of AI turns, not magically immune to transport backpressure.
- Bound actual blocked destination writes (proposed `WS_WRITE_TIMEOUT=120s`, aligned with the inspected backend default). Do **not** start a whole-message timer around `io.Copy`: that would also expire while waiting for new source bytes in a legitimate fragmented message. A small writer wrapper arms/stops cancellation around destination writer acquisition, each blocking write, and final close/flush; it is inactive while awaiting source data. Timeout terminates the pair and is classified as relay write stall.
- Do not add a new read-idle timeout or connection lifetime cap. Backend admission and business limits remain authoritative. Verify memory under the agreed concurrency inside the existing 64 MiB container limit; stop the rollout rather than silently increase limits or add an unreviewed global admission cap.

**Rejected:** one goroutine per message, whole 64 MiB message queues, and spool-to-disk all add lifecycle/resource problems. Full-message buffering would make the existing 64 MiB deployment unsafe under modest concurrency.

### 4. Heartbeat is transport activity, not a new eviction policy

Start the downstream heartbeat clock immediately after successful upgrade and initialization of the persistent relay readers, not after the first business frame. Track successful downstream application writes using a monotonic clock. On a tick, skip a probe if recent application output already kept that direction active; otherwise run at most one downstream `Ping` with the configured budget.

Proposed configuration:

| Setting | Default | Contract |
|---|---:|---|
| `WS_PING_INTERVAL` | `0s` | Disabled by default; production candidate uses `15s` |
| `WS_PING_TIMEOUT` | `5s` | Positive, less than an enabled interval; bounds one Ping/probe, not the AI turn |
| `WS_WRITE_TIMEOUT` | `120s` | Positive blocked-write budget, not a read/turn/session timeout |

There is no `WS_MAX_MESSAGE_BYTES` knob: the proxy-side read limit is fixed to unlimited (`SetReadLimit(-1)`) per the operator's no-cap decision; the backend's own read limit stays authoritative.

All invalid combinations fail startup. Keep the existing 10-second server shutdown grace unless a separately approved operational requirement changes it.

`Ping` returning nil is a **confirmed round trip**. A timeout/error is not automatically a confirmed disconnect: control processing can be delayed by backpressure, and the library combines write and Pong-wait errors. If the session is still live, record an unconfirmed probe and continue future ticks; do not copy the old ingress loop's `return on first error`. A transport error that closes the socket is observed by the persistent relay reader/writer and ends the session normally through its error path. Stop probing on session termination. Do not parse error strings to invent wire-delivery claims.

This deliberately does **not** add missing-Pong-only eviction. A nonresponsive peer is ultimately handled by actual I/O failure, the write-stall budget, backend policy, or shutdown. The aim is connection keepalive without introducing an aggressive new false-disconnect policy. The test suite must demonstrate repeated successful Pongs during silence and separately demonstrate that a delayed Pong under local backpressure is not mislabeled as client death.

Never send these probes upstream: Sub2API may legitimately not read while processing a turn. Upstream-originated controls are handled on the upstream leg by that leg's reader. Successful downstream heartbeat is not evidence that the backend is healthy or that generation progressed.

### 5. Explicit close, cancellation, and shutdown ownership

Use a small session-local coordinator for first terminal result and idempotent cleanup; no reusable transport framework. On normal peer close, forward a valid code with bounded, policy-safe reason handling. On abrupt upstream EOF/protocol failure, preserve abnormal failure semantics, using a valid error close when possible and never transmitting reserved code 1006 or fabricating an AI success event.

For client close, request cancellation, or shutdown: stop the ticker, attempt the appropriate bounded close handshake while the peer reader can still service it, then cancel remaining I/O and force-close both connections. Do not cancel the only reader before a close handshake that needs it. Forced cleanup must join relay/probe goroutines and close retained HTTP bodies. No reconnection or request replay occurs.

Add a private active-WS registry to `proxy`, with registration synchronized against shutdown. A pair being established during shutdown is either included or immediately closed; it must not escape the registry. `serve` stops new work, drains ordinary HTTP/SSE through existing shutdown behavior, and closes upgraded WS sessions within the same overall grace budget. Close sessions concurrently under that shared deadline rather than spending the grace period once per connection.

The proxy only exposes transport closure upstream. Sub2API retains its drain/usage-accounting decisions. Verify real cancellation and usage completion explicitly; forwarding a close frame alone is not proof of preserved billing behavior.

### 6. Minimal private observability

Use existing logs, not another listener. Generate a local opaque connection sequence/ID unrelated to API key or session ID. Log a bounded open/close summary with upgrade status, duration, app byte/message counts, heartbeat attempts, confirmed round trips, unconfirmed probes, and a fixed closure category. Optional debug events can expose per-probe timing; normal operation must not require unbounded per-frame logging.

Do not log raw URLs with query strings, credentials, request/response bodies, session identifiers, upstream addresses from library error strings, or unfiltered close text. A failed `Ping` is not logged as `client_disconnected` without a supporting transport result. The client-side acceptance probe, not a speculative `ping_sent` counter, is the oracle that frames reached the public client.

The old Sub2API `ingress_ws_downstream_ping_failed` messages may coexist until its old feature is disabled. New proxy counters/client wire evidence are the acceptance signal; do not demand that the old component suddenly emits `ingress_ws_downstream_ping_ok`.

### 7. File seams and existing checks

| Area | Planned minimum changes |
|---|---|
| Proxy `main.go` | Config parsing, early WS dispatch, shared direct transport access, active-session shutdown hook; preserve HTTP/SSE handlers |
| Proxy `websocket.go` (new) | Upstream-first handshake adapter, streaming relay, heartbeat, lifecycle and bounded summaries |
| Proxy `websocket_test.go` (new) | Real WS acceptance/component tests; use existing `testing`/`httptest` conventions, no new test framework |
| Proxy `main_test.go` | Config/dispatch/shutdown regression coverage where existing tests own those behaviors; keep the generic Upgrade test |
| Proxy `go.mod`, `go.sum`, `Dockerfile` | Pin the WS library and copy/download all required module/source inputs; preserve scratch/non-root image |
| Proxy `.github/workflows/ci.yml` | Reuse existing gate and main-only image publication; change only if a demonstrated coverage/build requirement demands it |
| Proxy `README.md` | Knobs, transport-vs-business boundaries, failure semantics, limits, and verification instructions |
| AI-gateway gitlink, Compose public examples | Published proxy revision and explicit WS knobs; production can select immutable prebuilt image without changing developer local-build defaults |
| Ansible role/entry/defaults + one fixture script | Scoped proxy deploy/rollback path, identity/readiness/invariant checks, coherent protected recovery, and sanitized receipts |
| Private desired state | Approved image/digest, pull policy and WS options only; remains gitignored |

### 8. Target-specific deployment transaction, not an ops rewrite

Continue using `ops.yml --tags deploy -e service=ai-sse-keepalive-proxy` and the corresponding rollback surface. Add the profile, but route this service through a focused role task that skips the existing generic per-file commit/pull/up path. Reuse existing preflight and per-file drift comparison; keep other services' operations unchanged.

The proxy path must:

1. Resolve the locally desired **effective** Compose service with both files and private `.env`; emit only a sanitized target/image/owned-field diff. Record the exact desired-input hashes, current remote hashes, baseline hashes, current proxy image/container identity, and protected unrelated container identities. Use a reviewed exclusive deployment window and fresh rechecks; do not claim a new distributed lock over older operations that do not participate.
2. In plan mode, perform no pulls, builds, uploads, starts, restarts, production probes, or baseline updates. Display readiness/live-acceptance and recovery requirements separately.
3. Before apply, bind approval to host/root/project, desired hashes, effective diff, selected image, recreation set, interruption, probes, and recovery. Recheck all relevant drift together before any upload. A changed input or unexpected remote edit requires a new plan. Never resolve drift by silently copying the current `.env` onto its baseline.
4. Prepare the exact candidate image **before** replacing serving files or stopping the proxy. Pull the desired digest directly, not a tag read from the old remote Compose configuration. Verify native platform image ID and OCI source revision. Keep the previous image available.
5. Capture an operation-specific protected copy of the complete affected file set and old image identity under `.ai-ops-state/`, including existence/mode/hash metadata. Preserve unchanged recovery material on a no-op run. This is necessary because generic `.predeploy` files can be overwritten by another invocation and generic per-file baselines advance before service verification.
6. Upload only reviewed file contents, activate only the application with the shared Compose prefix and `up -d --no-deps --no-build --pull never ai-sse-keepalive-proxy`, and verify identity plus readiness within a bounded interval (initially 60 seconds). Keep the namespace owner and relays intact. Reject an effective `pull_policy: build`; use the approved private override `pull_policy: never` after explicit artifact preparation. Keep public local-build defaults if desired.
7. Verify the running container's native image ID, configured digest selector, OCI revision label, healthcheck binary result, and approved effective WS knobs. Compare all protected container IDs. Only then commit deployment baselines and a `ready / live-acceptance-pending` receipt. A no-op compares files **and runtime identity/config**, so an old container with matching files is not mistaken for a completed rollout.
8. On activation/readiness failure, restore the coherent protected file set and prior image, then verify recovery. Guard every restore against this operation's actual last-written hashes, not an assumed successfully committed baseline. If only some files were uploaded, recover only the known touched set using the operation journal. Unknown drift/control loss means `recovery-required`, not a blind overwrite.
9. Keep later live acceptance as a separate approved stage. Its failure can trigger the approved rollback using the same operation-specific receipt; never reuse the generic rollback blindly when it cannot identify this complete deployment. Record readiness, live acceptance, and recovery independently.

Declare any new operator variables once under the existing `ai_ops_` role defaults namespace (for example approval/receipt selector and verification retry budget) and document them in existing examples/README. Reuse the fake-runtime fixture style from `scripts/test-ansible-models-enricher-config.sh`; add a focused `scripts/test-ansible-ws-keepalive.sh`. Do not make unrelated profile refactors a prerequisite.

## Risks / Trade-offs

- **An idle timeout is not the only cause of 1006** → keep a disabled negative control, classify observed closures, and report only bounded tested-path outcomes.
- **Streaming backpressure temporarily delays control reads** → use bounded streaming, report probe uncertainty truthfully, avoid missing-Pong-only eviction, and test delayed-Pong/slow-sink cases.
- **Default 32 KiB library limit breaks prompts** → both legs call `SetReadLimit(-1)` (operator-approved: no proxy-side cap); test >32 KiB and multi-MiB messages.
- **Dial truncates HTTP errors** → retain non-101 responses before its diagnostic consumption; test body/headers/context lifetime and single-attempt behavior.
- **Handshake termination changes wire details** → promise application semantics, not raw frame transparency; independently negotiate subprotocols/extensions and test actual client headers and Origin behavior.
- **Proxy restart drops active streams** → approved maintenance window, bounded drain, explicit disruption report; no zero-downtime claim.
- **64 MiB deployment plus concurrent large messages** → never buffer entire messages; test bounded memory at agreed concurrency, retaining container limits unless separately approved.
- **File rollback can clobber concurrent updates** → operation-specific snapshots, fresh hashes and conservative partial-state recovery; a deployment lock cannot replace drift checks.
- **The full public-path silent canary needs temporary resources** → obtain approval for its hostname/route/isolation/cleanup and keep production target routes unchanged.
- **Synthetic canary does not prove real backend semantics** → require separate real Responses multi-turn/cancel/usage evidence; neither test substitutes for the other.
- **Delegated review was unavailable** → record parent source verification honestly; an independent implementation review remains a pre-release gate, not an already completed fact.

## Migration Plan

### A. Freeze scope and establish failing examples

Obtain review of the acceptance examples before implementation. Refresh the proxy/submodule/upstream references and production-relevant limits; preserve dirty unrelated files and private configuration. Lock the candidate source revision and maintain a small source branch per implementation slice. Use real WS fixtures, not opaque `testproto`, to demonstrate missing repeated heartbeat against the current proxy before adding the behavior. No production mutation is needed for this red result.

### B. Implement and publish the proxy

Deliver handshake/streaming, heartbeat, and lifecycle slices through the existing Go tests; see the acceptance matrix below. Run gofmt check, `go vet ./...`, `go test -race ./...`, and a real container build/smoke test so the Dockerfile copy boundary is exercised. Verify the container emits real heartbeats, not merely that the host test binary does.

Obtain independent review of the functional changes and resolve findings. Commit/push reviewed proxy source to `xz-dev/ai-sse-keepalive-proxy` using the repository's normal branch/PR process. Merge only after exact-head CI passes; the existing main workflow then publishes the full-SHA tag and digest. Record the merged source SHA, workflow URL/result, image index digest, native-platform identity and revision label. Do not deploy `latest`, amend a pushed build source, or publish an AI-gateway gitlink pointing to an unavailable local commit.

### C. Publish and rehearse integration

Update AI-gateway's gitlink to the published proxy revision and commit only the narrowly required public Compose/Ansible/tests/docs changes. Keep documentation commits organized separately where repository conventions require it. Never include private `.env`, credentials, production dumps, unrelated resource-limit hunks, or detached scratch reports.

Use existing operations fixtures plus the new proxy fixture to prove no-op, pre-upload artifact failure, drift rejection, failed readiness recovery, partial upload recovery, rollback refusal over new drift, and proxy-only recreation. Build/validate locally or in CI, never on production. Prepare operator-owned private desired fields only after comparing the effective override chain.

### D. Stage a representative real-WS canary

First run an isolated local/staging topology with a real WebSocket fixture upstream and an ingress idle timer. The fixture accepts an initial synthetic request, remains application-silent, then sends a sentinel and accepts another message on the same connection. The fixture does not call an AI provider. Test with keepalive disabled and enabled using an accelerated idle timer; then use the production-representative 600-second timer and a **20-minute** application-silent interval. Observe client Ping frames and matching Pong results, not just connection openness.

For the public-path gate, use an explicitly approved isolated canary route/hostname through the relevant Cloudflare/cloudflared/APISIX path and a proxy built from the exact candidate image, with the same fixed-upstream/relay/isolation policy except for the synthetic backend. Do not repoint production Sub2API traffic at the fixture or silently add a debug endpoint to the service. Capture route/config identity and the deliberate backend difference. Removing the owned canary resources is part of the approved procedure. If this setup is not authorized, report this gate pending.

The canary proves idle transport behavior; it does not prove Sub2API can ignore its own 900-second business read limit. A real backend closing at its legitimate limit must not be concealed by continued proxy heartbeat.

### E. Apply through Ansible and verify the real target

The following are **future commands**, after the scoped operation is implemented and reviewed; they are not claims that the current playbook already supports the service:

```sh
cd "/home/xz/Code/ai/AI-gateway/ansible"
ansible-playbook ops.yml --tags deploy \
  -e service=ai-sse-keepalive-proxy -e ai_ops_mode=plan

# After explicit approval of the displayed target/diff/identities/interruption/recovery:
ansible-playbook ops.yml --tags deploy \
  -e service=ai-sse-keepalive-proxy -e ai_ops_mode=apply \
  -e ai_ops_ws_keepalive_approved_digest="<reviewed-plan-digest>"

# Only under the approved rollback scope, using this operation's protected receipt:
ansible-playbook ops.yml --tags rollback \
  -e service=ai-sse-keepalive-proxy \
  -e ai_ops_ws_keepalive_receipt="<operation-id>"
```

Before apply, inventory active traffic and obtain a bounded interruption window. The plan targets `rainyun-la`, root `/root/AI-gateway`, project `ai-gateway`, and only the proxy application. Both configuration and evidence are sourced from `ansible/private-config/` and current runtime observation, not repository-root `.env` or a stale image name. Snapshot protected namespace, relay, Sub2API, and unrelated service IDs.

After readiness/provenance passes, run only the approved verification traffic through the **actual production Responses entrance** with the real client and selected low-cost account/model. Verify an initial response, a heartbeat-bearing inter-turn gap within backend policy, a subsequent turn using the same connection/session, terminal-event parsing, and an explicit client cancellation. Correlate Sub2API drain/usage completion and check no unintended retry/replay. A test that could consume model credits needs its own approved request/token/cost budget; secrets come from operator-owned files/environment, never command-line arguments or committed fixtures.

Observe the deployed route for at least the planned 20-minute window, extending if the previously observed recurrence window is longer. Record session/turn counts, control-frame confirmations, errors/closures, and expected deployment-disconnection exclusions. A sequence of short turns only proves session longevity; it is not relabeled a long-silent-turn test. If an actual long silent model turn cannot safely be produced, state that boundary and keep the synthetic public-path silence evidence separate.

### F. Optional retirement of the old backend keepalive

The inspected Sub2API code supports `gateway.openai_ws.passthrough_downstream_ping_interval_seconds=0`; the corresponding env mapping is `GATEWAY_OPENAI_WS_PASSTHROUGH_DOWNSTREAM_PING_INTERVAL_SECONDS=0`. Verify this against the exact deployed image/effective configuration before using it.

After proxy acceptance, inventory entrances that bypass this proxy (including direct host/Tailscale access). Do not disable working passthrough keepalive for uncovered clients. If every required entrance is covered, propose a **separate approved configuration rollout/restart of Sub2API** to set the interval to zero. It is not part of the proxy-only recreation approval, and no Sub2API rebuild is required. Preserve its image patch-list comment and all business patches. Patch removal/rebuild can be a later maintenance change once independently requested.

### Acceptance and evidence matrix

| ID | Observable example / oracle | Gate |
|---|---|---|
| WS-01 | Enabled proxy, silent real-WS upstream, at least three client-observed Pings and confirmed Pongs, then sentinel delivery | Local acceptance red/green; container smoke |
| WS-02 | Upstream deliberately does not read during its simulated turn; downstream probes still work | Local regression for the diagnosed coupling |
| WS-03 | Auth/session/subprotocol handshake; non-101 4xx/429/5xx and >1 KiB body; redirect not followed; downstream acceptance failure cleans upstream | Local handshake compatibility |
| WS-04 | Text/binary, fragmentation/control interleaving, multiple messages/turns, >32 KiB and approved large message | Local semantic compatibility |
| WS-05 | Slow reader/writer and delayed Pong; no dropped data or false client-death diagnosis; bounded I/O failure and memory | Local resource/concurrency/race tests |
| WS-06 | Valid peer close, upstream abrupt EOF, client cancel, signal during handshake and active session | Local lifecycle, container shutdown; real cancellation smoke |
| WS-07 | Existing SSE terminal/error/startup behavior, non-stream 103 behavior, ordinary HTTP and `testproto` Upgrade | Existing suite plus focused regressions |
| OPS-01 | Fake-runtime evidence for artifact prep, drift, no-op, proxy-only up, readiness recovery, partial writes and guarded rollback | Ansible fixture; no production effects |
| PUB-01 | Published proxy SHA, passing exact-head CI, immutable image identity, published AI-gateway gitlink commit | Publication receipt |
| LIVE-01 | Approved public-path synthetic upstream silent for 20 min; repeated controls, unchanged connection, post-silence messages; disabled negative control | Public-ingress canary receipt |
| LIVE-02 | Real production client completes multiple turns on same session, receives heartbeat, cancels correctly; usage/drain checked | Authorized real inference receipt |
| LIVE-03 | Running digest/revision/config/health match, protected container IDs unchanged, bounded post-rollout closure observations | Deployment and observation receipt |

Final delivery needs **all applicable gates**, with unauthorized/unavailable gates marked pending rather than waived. Keep a sanitized `verification.md` in this change during implementation containing commands, UTC timestamps, source/image/route identities, results and artifact references; do not create fake results now. Technical test success is evidence for the operator's final acceptance, not a substitute for it.

## Open Questions

These are operational inputs that can be supplied at apply time without changing the design:

- Exact canary hostname/route ownership, fixture placement, approved observation time, and cleanup window.
- Real test account/model, client version, secret input location, and authorized request/token/cost budget.
- Refreshed production message-size limits, traffic concurrency and capacity margin, plus the acceptable brief restart window.
- Final published source/image/AI-gateway revisions and protected pre-deploy operation ID, which do not exist yet.

Any newly discovered requirement for mandatory WS extensions, different failure/eviction semantics, an enlarged recreation set, or a conflicting message/admission limit is **not** a deferrable implementation detail: stop and revise the proposal with the operator before changing behavior.
