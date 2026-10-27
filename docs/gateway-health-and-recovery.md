# Gateway health and bounded recovery

Health has three separate meanings:

- **Local liveness:** this component can perform its own bounded operation.
- **Dependency readiness:** required upstream components respond. A readiness failure does not authorize restarting every downstream proxy.
- **Request success:** a real user request completes, including its terminal stream event. Local health and HTTP 200 alone do not prove this.

## Application recovery

`scripts/ops/gateway-recovery.py` runs on the Docker host through `ai-gateway-recovery.timer`. It uses Python's standard library, the local Docker Unix socket, and Linux network namespaces. Python 3.12 or later with `os.setns` is required. Probes use direct local sockets, not proxy environment variables, and never request provider inference.

| Application | Probe | Meaning |
| --- | --- | --- |
| cli-proxy-api | Authenticated `GET /v1/models`, HTTP 200 and a JSON `data` array | CPA can serve its local catalog read path |
| sub2api | `GET /health`, HTTP 200 | Application health endpoint; dependency semantics belong to Sub2API |
| openai-api-cache-keeper | `GET /healthz`, HTTP 200 | Keeper HTTP liveness, not proof of cache writes, hits or replay |
| responses-ws-sse-bridge | `GET :8086/healthz`, HTTP 200 | Bridge local liveness; deployed 2026-10-23, see the [rollout record](deployment-batch-20261023.md#recovery-guard--deployed) |
| models-enricher | `GET /healthz`, HTTP 200 | Enricher local liveness |
| ai-sse-keepalive-proxy | `GET /healthz`, HTTP 200 | Keepalive proxy local liveness |
| apisix | Local `GET /healthz`, expected HTTP 404 | APISIX's unmatched-route rejection responds; no upstream request |
| apisix-models | Local `GET /healthz`, HTTP 200 | APISIX's explicit local health route responds; no CPA request |

The CPA key comes from the existing `apisix-models` container environment; the front keeper does not hold it. It remains in process memory, not arguments or logs. A missing key or HTTP 401/403 is **unknown**, not a restart decision. An inaccessible namespace is also unknown. Deployment refuses unknown or failed probes.

Recovery policy:

- 120-second startup grace; three consecutive failed checks.
- Ten-minute cooldown; at most three attempts per application per hour.
- At most one application restart per invocation; no database, cache, namespace owner, relay, or provider-sidecar restart.
- Recheck Compose ownership, exact container ID, start time, and running/unpaused state immediately before mutation.
- Preserve cooldown/history across container recreation. Write the attempt to an atomic, mode-0600 state file **before** asking Docker to restart. An unknown restart outcome consumes its budget and is not retried immediately.
- Respect stopped, paused, or restarting containers. Docker's restart policy handles ordinary process exits.
- Probe socket timeout is three seconds; a six-second wall-clock alarm bounds each namespace probe, including slow response bodies. The original namespace is restored in `finally`.
- A nonblocking lock prevents overlap. The systemd service has a 120-second ceiling, 64 MiB memory limit, and 10% CPU quota. The timer waits 30 seconds after each completed invocation, with bounded jitter.

Each cycle also reports `startup_unit` from a bounded, read-only `systemctl show ai-gateway.service`. A failed startup is visible as `state=failed, ok=false, action=observe_only`. It never resets the unit's start limit, starts the stack, or starts an exited container. Unknown systemd state is reported as unknown, not success.

This does not prove that every upstream preparation/write stall also blocks the catalog. It does not identify CPA's memory-growth root cause. A successful systemd invocation means the guard ran; inspect individual `checks[].ok` and `action`, not only the unit result.

### Public APISIX probe and keyless catalog noise

The public APISIX has no `/healthz` upstream route: its native unmatched-route
404 is a bounded local liveness check. Keep both its Compose HEALTHCHECK and
recovery `CHECKS` on that path, expecting **404**, not 200 or 401. The inner
`apisix-models` has a separate explicit `/healthz` route returning 200.

The former public `/v1/models` probe was not local: it traversed keepalive,
keeper and bridge to Sub2API, which returned 401 without a key. Public APISIX
then rewrote that to 404. Read-only production correlation on 2026-10-25 matched
85/85 keepalive 401s in a 15-minute window to loopback APISIX probes (no User-Agent):
60 HTTP/1.0 requests from the 15-second Docker healthcheck and 25 HTTP/1.1
requests from the recovery timer (about 36 seconds including runtime/jitter).
The combined cadence explains the approximately 10.6-second noise; this was
internal probe traffic, not evidence of an external client misconfiguration.

Changing the repository does not update production. Deploy the recovery script
through its guarded `deploy-recovery` entry point, and separately review the
public APISIX HEALTHCHECK Compose update/recreate. The component image/env-only
transaction does not permit healthcheck changes; do not use full-stack `up` as
a workaround or activate the deferred Operator services. No keeper recreate or
cache wipe is needed for this probe fix. Keep catalog authentication intact.

## Deploy and inspect

Deploy the explicit `apisix-models` local health route and strict health status check first. Its old `/healthz` catch-all reached CPA; that would turn an upstream outage into an unsafe proxy-restart signal. The old shell check also accepted HTTP 502 and 503 because it ended with `true`.

From `ansible/`:

```bash
ansible-playbook ops.yml --tags deploy-recovery -e ai_ops_mode=plan
ansible-playbook ops.yml --tags deploy-recovery -e ai_ops_mode=apply
```

Plan compares the four candidate script/unit checksums with live files and reports which need upload; it does not copy files or start the timer. Apply copies the recovery script, its read-only `gateway_health.py` role probes, and units; checks application probes without restarting, verifies units, and enables the timer. It does not deploy application routes or recreate containers. Inspect the route and healthcheck change separately before deploying them.

```bash
systemctl status ai-gateway-recovery.timer --no-pager
systemctl show ai-gateway-recovery.service \
  --property=Result,ExecMainStatus,MemoryPeak,CPUUsageNSec
journalctl -u ai-gateway-recovery.service -n 12 --no-pager -o cat
```

Guard state is `/var/lib/ai-gateway-recovery/state.json`. Do not erase it to bypass recovery limits.

For a controlled diagnostic window, stop both the timer and any in-flight invocation:

```bash
systemctl stop ai-gateway-recovery.timer ai-gateway-recovery.service
```

Coordinate an explicit window-end receipt before resuming:

```bash
systemctl start ai-gateway-recovery.timer
```

Stopping does not disable boot enablement. To withdraw the recovery installation's automatic operation, disable the timer and stop the in-flight service:

```bash
systemctl disable --now ai-gateway-recovery.timer
systemctl stop ai-gateway-recovery.service
```

A restart request already accepted by Docker can still finish after the guard stops. Check state and container identity before taking further action.

## Keeper and bridge maintenance

Keeper and bridge have guarded single-service Ansible `deploy`/`rollback`
profiles; the old scoped standard-cache tags remain retired and fail in preflight.
See [Keeper and ws-sse bridge rollout](ansible-operations.md#guarded-component-rollout).
Coordinate one production writer and a maintenance window with other owners and
this recovery timer. A keeper restart interrupts in-flight streams and collectors;
the named volume survives, but keeper wipes its history buckets at startup, so
cached continuations are not restored.

The updated recovery source includes bridge `:8086/healthz` among its eight
application checks. This addition is not deployed merely by committing the source;
use the separate reviewed `deploy-recovery` operation. The timer restarts only
running containers that fail probes. It still skips stopped or exited containers
and cannot break Compose boot health gates. Boot start depends on
`ai-gateway.service` and Compose `depends_on`. On 2026-10-23 a VM reboot
deadlocked there: the bridge healthcheck probed its default `PORT` 8080
(keepalive in the shared namespace) instead of `LISTEN_ADDR` 8086. Keeper
waited for a healthy bridge, keepalive waited for keeper, and the unit hit its
start limit. The bridge now sets `PORT=8086`; a topology test keeps it equal to
the listen port.

## Component deployment readiness and interrupted controllers

Generic component deploy/rollback now shares the keeper/bridge transaction:
reviewed three-file/runtime CAS, immutable-image proof before recreate, bounded
readiness, and unchanged neighbouring identities. Docker HEALTHCHECK is used when
present. Services without one use the existing read-only `OBSERVE_HTTP` probes
below through `namespace_probe`; cpa-model-sync uses `worker_health(...).ready`,
not merely its live-worker `ok` flag. Unknown readiness refuses activation; no
new probe URL or restart authorization is inferred.

The remote `transaction-<id>.json` progress receipt and owned lock survive a
controller kill. A controller deadline should be at least 30 minutes, not 300
seconds. `recover-stale-lock` is separately planned and names the exact
transaction, requires matching owner/receipt, an age of at least 15 minutes,
the recorded controller PID gone on its original host, and no other deployment
writers locally/remotely. It releases only ownership; it never declares success,
rolls back, or changes live configuration. An empty lock left by the bounded
keeper experiment monitor has no Ansible owner and cannot pass this recovery.
See [component operations](ansible-operations.md#controller-deadline-logs-and-stale-lock-recovery)
for partial logs, progress stages and recovery commands.

The changed apply/rollback path was verified offline with fake Docker, including
failed/skipped pulls, wrong digests, failed recreation, controller kill, guarded
lock recovery, native HTTP readiness, CAS and neighbour drift. Read-only
production plans do not prove production apply. No production container was
recreated by this repair lane.

## Verification and limits

```bash
python3 scripts/test-gateway-recovery.py
```

The tests exercise strict production Compose commands against HTTP 200/502/503, authentication and JSON checks, a live health endpoint with a stuck catalog, startup grace, consecutive failures, cooldown/hourly limits across instances, namespace restoration and wall-clock timeout, and persistence before an unknown restart outcome. They also reject another process's listener in a shared namespace, a missing supervised relay port, default-route escape, a missing owned TUN handle, non-PONG Redis responses, stale native success, and confusing worker progress with dependency readiness. Restart API effects are mocked; this is not a production fault-injection result.

On 2026-10-20 the initial subprocess-heavy guard reached its 64 MiB limit. Replacing Docker CLI and repeated Python startup with the Unix-socket API and one interpreter reduced a measured systemd invocation to 22,298,624 bytes peak memory, 0.216 CPU seconds, and about two wall-clock seconds under the same limits. Seven probes passed and no recovery budget was consumed. These are short-window measurements, not a 24-hour stability result.

## Full-stack host observations

The deployed host guard observes every running container, including the standalone relay. Its historical automatic allowlist has seven applications; the updated source adds only the bridge as the eighth, effective after guard deployment. Additional roles only produce observations; they cannot enter the restart decision path.

| Role | Deployed observation | Meaning / limit |
| --- | --- | --- |
| 25 TCP relays | Exact configured IPv4 address/port, listening socket inode owned by the relay; the existing six-port shell supervisor checks its actual direct children and every port | Local listener, not upstream readiness. Another process in the shared namespace cannot make this green |
| 12 namespace owners | Interfaces and connected routes match attached IPv4 networks; no usable IPv4/IPv6 default route | Checks the current deny-default topology, not a sleeping PID |
| provider-sidecar-tunnel | Actual tun2proxy process, its owned `/dev/net/tun` handle, associated interface, and its active main-table default route | Current deployed routing was inspected first; do not reuse this route contract for a policy/split-routing deployment without adapting it |
| cloudflared | Local `:20241/ready`, strict HTTP 200 | Tunnel readiness; Cloudflare dependency failure does not authorize restart |
| aisix-status | Local `:8080/`, strict expected 404 | Local handler's rejection route, not AISIX readiness |
| AISIX | Its actual `172.30.68.2:9090/metrics`, strict HTTP 200 | Existing metrics listener, not an assumed loopback binding or an inference request |
| model-catalog-sidecar | `172.30.41.3:8080/models-table/refresh`, **GET**, strict expected 403 | Nginx's local method restriction rejects GET before proxying; never POST or trigger a refresh |
| Redis | Direct local RESP PING, exact `+PONG` | Error replies are not success; an authentication error is unknown, not proof the process died |
| Squid/Unbound | Squid's local rejection of `GET *`, expected HTTP 400, plus native resolver/Squid readiness reported separately | No destination lookup or provider request |
| PostgreSQL | Audited native `pg_isready` with fresh engine result | Accepting PostgreSQL connections, not proof of application credentials or query correctness |
| provider-sidecar | Audited native authenticated local HTTP health and virtual-DNS check, with fresh engine result | Includes DNS readiness; no auto-restart on upstream failure |
| cpa-model-sync | Most recent complete JSON cycle summary from at most 64 KiB of existing json-file logs; within twice its configured interval plus 120 seconds | Progress and dependencies are separate: `ok=true, ready=false` is a live worker with failed/unconfirmed channels, not fully healthy |

HTTP addresses above match the current static topology. Update them deliberately when that topology changes. Missing evidence, unsupported roles, inaccessible process files, and stale native health results are unknown, never fixed-success fallbacks. New containers with unknown roles do not silently become green.

The updated source emits `checks` for eight application recovery decisions, read-only `startup_unit`, `coverage.running`, `coverage.observed`, and per-container `coverage.checks`. Compare running/observed counts and inspect **both** `ok` and any `ready` field. A successful unit result is not an all-dependencies-green signal.

Production verification on 2026-10-20 at 14:56 UTC recorded 54 running / 54 observed, all 54 role/liveness checks passing, no unknown observations, unchanged running container IDs, and zero guard restart attempts. The model-sync worker still reported four failed dependencies (`ok=true, ready=false`); this is an open readiness issue, not suppressed as green. Counts are point-in-time, not a deployment contract.

A bounded read-only follow-up at 15:36 UTC inspected the existing 15:32 cycle: zero updates, 13 unchanged, four failed, zero unconfirmed, and one configured skip. Two OpenAI-compatible channels and one Codex channel reported `upstream request failed`. In the current source this means the management `api-call` JSON did not contain a successful `status_code`; the static log does not identify the exact status, so it does not prove expired credentials or a particular provider fault. One Claude channel reported `required string missing`. A management-only GET at 15:43 returned 200 using the worker's existing identity and actual hosts mapping: that entry had a key and auth index but no nonempty `base-url`. The current inventory code requires that field before making an upstream request. No refresh, upstream catalog call, model write, or restart was triggered by these follow-up inspections.

Follow-up on 2026-10-21: CPA's own `effectiveClaudeBaseURL` sends a Claude key without `base-url` to `https://api.anthropic.com`, so the model-sync requirement was a compatibility bug. `cpa-model-sync` v0.1.3 uses the same default for Claude keys only, and classifies catalog failures as `upstream rejected credentials` (401/403), `upstream rate limited` (429) or `upstream request failed` (other), never logging response bodies. Only the `cpa-model-sync` container was replaced; all other container IDs were unchanged. The first v0.1.3 cycle still reports 4 failed / 13 unchanged / 1 skipped, now with real causes: `anthropic-api`, `axis` and `kimi-cn-api` are rejected credentials, and `zakk` is rate limited. These are provider credential/quota states for the operator, not gateway or synchronizer faults; their existing model records are preserved.

## CPA profile capture before restart

CPA runs pprof on a loopback-only listener inside its own network namespace (`observability.pprof` with `addr: 127.0.0.1:8316`; nothing is published). Before the guard restarts CPA, it enters that namespace and streams `/debug/pprof/heap?debug=1` and `/debug/pprof/goroutine?debug=1` to `/var/lib/ai-gateway-recovery/pprof/` (directory `0700`, files `0600`), keeping the six most recent captures. Each profile has a 10-second deadline and a 16 MiB cap; `gc=1` and `seconds` are never sent. A capture failure is recorded in the cycle JSON and never delays or blocks the restart. Any tool that rewrites the CPA config must keep `observability.pprof.enable: true`, or the next CPA restart silently disables these captures.

Under the unchanged 64 MiB / 10% CPU limits, the full-coverage systemd invocation used 20,099,072 bytes peak memory and 0.295 CPU seconds over about three seconds. These are short-window measurements, not a 24-hour or whole-gateway CPU acceptance result.

## Remaining native Docker startup gap

Whole-stack host monitoring is deployed. It does **not** rewrite existing Docker health metadata or dependency gates. In particular, the tunnel's native `--version` check remains insufficient, even though the host now performs the real process/interface/route check. Do not treat its Docker `healthy` label alone as tunnel readiness.

The pinned tunnel image is scratch-based; runtime inspection found no shell, BusyBox, Python, or grep. Replacing its in-container probe requires an actual validated executable available inside that image, not a shell command that cannot run. Installing such a helper or changing the image/mounts is distinct from the approved host guard deployment. Recreating its namespace owner also requires a coupled, drift-protected operation with provider-sidecar and both adjacent relays; never rebuild the owner alone or blindly restart the full stack.

The operator explicitly chose to preserve availability and defer that native startup-gate repair to a separate maintenance window. This phase must not add a tunnel helper, image, or mount, or recreate the sidecar group. Host observations remain active; the native startup gap is not closed. No database, namespace owner, sidecar, application image, credentials, or limits were changed by the full-coverage host rollout.
