# CPA memory-pressure mitigation — 2026-11-10

The refusal recurred. The operator subsequently requested removing CPA's memory
admission gate; see [the 2026-11-12 removal record](cpa-memory-gate-removal-2026-11-12.md).
The observations below describe the earlier mitigation, not the current gate policy.

## Outcome

The CPA-only environment rollout completed. `GOMEMLIMIT` changed from
`512MiB` to `384MiB`; the byte-admission checks and 32 MiB margin were not
changed. Existing Pi traffic resumed successfully through the original gateway
path. This is an operational mitigation, not proof that the underlying retained
objects or concurrent reservations have been fully explained.

## Incident evidence

The client received CPA `503 insufficient_memory` while requesting a 9 MiB
charge. CPA requires `memory.max - memory.current - outstanding >= charge +
32 MiB`. The error's `limit_bytes=42991616` is that charge plus margin, **not**
the container memory limit. Negative availability can include outstanding
reservations; it does not by itself prove that `memory.current` exceeded the
limit.

Read-only production inspection established:

- RAM limit: 768 MiB; combined memory/swap limit: 1024 MiB, allowing 256 MiB swap.
- Original `GOMEMLIMIT`: 512 MiB; CPU quota remained 1.75.
- An early sample had `memory.current=668246016` and
  `memory.swap.current=268226560` bytes. A later process sample had about
  525 MiB anonymous RSS and 242 MiB swapped process memory. These are different
  samples, not one additive snapshot.
- No cgroup OOM or OOM-kill events were observed. The original cgroup had
  24,513 `max` events.
- The deployed image already included request-reference cleanup and reclamation:
  revision `19993a8b32de022295ac48a24511911770d8b796`, image
  `ghcr.io/xz-dev/cliproxyapi@sha256:4835aa6c189f2db5d4a2558e4533e9b7eb185ece17b20b4f3b10ea2134bf2b6a`.
  Reimplementing that fix would have duplicated deployed work.

The diagnostic and preflight children themselves encountered the same CPA
admission error. The audit timed out without a final report; the other child
was stopped. The parent recovered the partial evidence and performed the
rollout directly. Neither child deployed anything.

## Change and safeguards

The controller copied the actual live Compose inputs into protected,
disk-backed scratch. Its older private desired state was not treated as the
production baseline. The effective candidate changed only CPA's `GOMEMLIMIT`.
Image, resources, swap, account/configuration state, admission factors, routing,
other services and the safety margin were preserved.

The guarded preview passed with approval digest
`c9090c38c8ef02627387f2c6e3d54e7f81c9a06b43d1e441d3b57c11a5b9ecd7`.
Three ten-second connection observations found 6/5/5 established connections,
with one connection on a listening port in each sample. The CPA-only apply
completed with 152 successful tasks and zero failures; the controller verified
all 47 protected neighbours and namespace ownership. Protected rollback
snapshots and the operation receipt remain available.

The new container started at **13:16:48.417649728 UTC**. Actual interruption was
not measured; the recreation could interrupt the observed in-flight connection.
The private controller desired state was then adopted from the verified live
candidate, retaining its previous files in protected scratch. Final preview:
`activation_required=false`, `config_diff=false`, no environment delta, healthy,
writer lock unheld; 69 successful tasks, zero changes and zero failures.

## Verification

- Existing CPA regression seam passed:
  `go test ./internal/bodyspool -run 'Test.*(Charge|Fallback|Reclaim)' -count=1`.
- `openspec validate --all --strict`: 22 passed, zero failed.
- The existing local mock harness ran the deployed revision at 768 MiB/no swap
  with both 512 MiB and 384 MiB Go targets. Each media-heavy 256 MiB replay
  returned HTTP 200, delivered 268,435,643 upstream bytes with SHA-256
  `996cf1a91093d02104680959e293437821d496819ccb46711f84be5af67953a3`, and
  recorded zero `max`, OOM and OOM-kill events. Scratch was disk-backed.
- These local runs had only about 6.4 MiB live heap after completion. They prove
  request compatibility, **not** a measured GC improvement at either target.
  Their 2.0/1.8-second request times are not a controlled performance comparison.
- At 13:20:41 UTC the deployed CPA was healthy, restart count zero, OOM false,
  with the expected image, 768 MiB RAM/256 MiB swap and `GOMEMLIMIT=384MiB`.
  `memory.current=417488896`, swap `265076736` bytes; all `max`/OOM counters zero.
- By the session inspection around 13:24 UTC, four existing Pi sessions using
  the original API-extension path had recorded **10 successful assistant
  responses after activation and zero API errors**. This is actual resumed
  traffic, not a substitute mock or a catalog-only health check.
- At 13:25:51 UTC, with Pi traffic resumed, CPA remained healthy with zero
  restarts and zero `max`/OOM/OOM-kill events. `memory.current=497020928`,
  anonymous `281001984`, file `211779584`, kernel `5148672`, and swap
  `267784192` bytes. These observations are not a controlled before/after load
  comparison or a guarantee against future capacity refusals.

A separate minimal direct probe selected an available catalog model but received
provider `403 insufficient_quota`, not the incident's memory 503. It was not
counted as successful inference, and no quota, credential or provider settings
were changed. The existing-session successes above supplied the original-path
recovery evidence instead.

## Herdr follow-up

The operator requested checking running Pi sessions after gateway recovery and
using `/retry` only for those stopped by errors. Ten Pi agents were inventoried,
including this supervisor. The other nine were checked against their actual
session stop reasons and terminal detection output. None was confirmed stopped
on an error. One working session displayed historical CPA failures but had
subsequent successful responses; it was not interrupted. No `/retry` was sent
to a normally completed or active session.

## Remaining limits and rollback

Lowering the Go target does not replace admission accounting. The holder of the
original anonymous/swapped memory and the live outstanding-reservation amount
were not identified. A restart also releases process state, so its benefit must
not be attributed solely to the target change. Longer-term memory, CPU/latency
and representative concurrent-load behaviour remain unmeasured.

If the change causes unhealthy operation, new OOMs, inference failures or
material GC/latency regression, obtain a fresh guarded rollback preview and
apply its reviewed digest against the saved CPA snapshot. Do not restore the
stale historical 480 MiB private template or reduce the 32 MiB margin.

Private controller evidence is under `/var/tmp/cpa-fast-repair-20261110/`;
secret-bearing files/logs and the rollback snapshot are not tracked.
