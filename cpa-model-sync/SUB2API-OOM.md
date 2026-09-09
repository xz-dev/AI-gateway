# Sub2API OOM: bounded read-only findings

Date: 2026-09-06. Scope authorized: existing logs, resource settings and call-path
evidence only. No production replay, profiling endpoint, instrumentation,
configuration change, restart or deployment was performed during this investigation.
Production remains rolled back. This report does not establish rollout acceptance.

## Established facts

- Deployed image: `ghcr.io/wei-shaw/sub2api:0.1.185`, revision
  `2ac784c51a5d0925b324efef2ba6b3446c364781`.
- Sub2API memory/swap limit: **268,435,456 bytes each**, 1.5 CPU, 128 PIDs.
  `GOMEMLIMIT`, `GOGC`, and `GOMAXPROCS` were not explicitly present in its container
  environment. That is not proof of the effective runtime's internal settings.
- Kernel logs confirm **container-cgroup OOM**, not a demonstrated host-global OOM:
  kills at **12:40:20 and 12:41:14 UTC**, with anonymous RSS of 260,072 and
  259,252 KiB. Docker restart count moved from 1 to 3.
- No panic/fatal/out-of-memory marker appeared in retained application logs.
  Application log silence and `State.OOMKilled=false` after restart do not refute
  the kernel evidence. The retained Docker event query was empty and inconclusive.

## Direct catalog-load explanation lacks supporting evidence

During 12:35–12:42 UTC, the 27 completed Sub2API `/v1/models` requests were all
**401**, with `ingress_reject_reason=api_key_required`. Most completed in 0 ms.
These are not successful large-catalog processing requests.

The unchanged catalog APISIX container's retained logs had:

| Window UTC | `/v1/models` path mentions |
| --- | ---: |
| 12:31–12:34, before cutover | 3 |
| 12:36:38–12:52:09, candidate interval | 0 |
| 12:52:09–12:55, rollback interval | 3 |

Its candidate-window logs did contain inference traffic, including 90
`/v1/responses` mentions. This supports the absence of an observed catalog fetch
through that leg, not an exhaustive proof that no alternate/unlogged path exists.
Candidate Go/sidecar historical logs could not be recovered from their replacement
containers; an empty time-window query against a replacement is not negative
evidence. Candidate public-load acceptance was never completed.

## Stronger lead: large Responses inference bodies

The security-audit entry logs record `body_bytes` for incoming inference bodies:

| Audit-log window UTC | Records with body size | Maximum bytes | Records ≥5 MB |
| --- | ---: | ---: | ---: |
| 12:15–12:36:38, before | 227 | 1,495,751 | 0 |
| 12:36:38–12:52:09, candidate | 123 | 6,311,904 | 6 |
| 12:52:09–13:05, restored | 83 | 829,400 | 0 |

These are audit-start records, not a controlled comparison of equal request loads.
Six large records corresponded to six client request IDs under one API-key ID;
identifiers and payload contents are deliberately omitted.

Large `/v1/responses` bodies entered at 12:40:03.847 (WebSocket first turn),
12:40:17.701 (HTTP), 12:40:23.805 (HTTP), 12:40:35.605 (WebSocket first turn),
12:40:55.116 (HTTP), and 12:41:19.326 (HTTP). Each audit entry had a matching
`allow` completion within roughly 0.5 ms. Therefore the audit log location is
not evidence that the audit check itself hung or caused the allocation spike.

At the exact image revision, `backend/internal/handler/security_audit_helper.go`
passes `len(body)` to `logSecurityAuditStart`, records it as `body_bytes`, and
passes the same body into a `securityaudit.Request`. This confirms that those
6.31 MB measurements are inference request bodies, not catalog responses.

Source:
https://github.com/Wei-Shaw/sub2api/blob/2ac784c51a5d0925b324efef2ba6b3446c364781/backend/internal/handler/security_audit_helper.go

## Conclusion and limits

**Confirmed:** Sub2API exceeded its own 256 MiB cgroup budget during the candidate
interval. **Leading investigation direction:** Responses request handling, copies,
concurrency and WebSocket/HTTP retry behavior with approximately 6.31 MB bodies.
The available evidence does not support labeling this a direct catalog-body OOM.

**Not established:** the allocation site, the exact triggering request, or whether
model synchronization indirectly altered routing/reconnect/replay behavior. The
large-body workload also disappeared after rollback, so an OOM-free restored
window cannot prove that rollback itself removed the cause. No heap profile or
controlled reproduction exists; no request content was inspected or replayed.

Greater certainty requires separately authorized isolated reproduction with
representative/sanitized Responses bodies and the exact image/resource budget,
or separately approved instrumentation. Do not treat an increased memory limit,
`GOMEMLIMIT` change, or another deployment as an established fix.

Kernel and rollback receipts are preserved under
`/root/AI-gateway/.model-sync-filtered-cb312e5afa90/` and sanitized copies under
`/tmp/cpa-sync-deploy.YnAHzK/filtered-deploy/receipts/`. Exact-revision source was
read locally under the same temporary evidence directory. No credentials or
inference bodies are included in this report.
