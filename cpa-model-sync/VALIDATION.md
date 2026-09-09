# Final local validation

> Both production attempts were rolled back; see [PRODUCTION.md](PRODUCTION.md).
> The initial unfiltered attempt exceeded 16 MiB. The 32 MiB experiment was
> subsequently withdrawn after recovering the required channel policies.
> Corrected-policy local Rust and four-component HTTP budget checks passed,
> but the production retry encountered Sub2API OOM kills before candidate
> public-load acceptance completed. These local results do not establish
> production convergence or safety; see [CATALOG-LIMIT.md](CATALOG-LIMIT.md).

## Final candidate and CPA workaround

Local image: `localhost/cpa-model-sync:v0.1.0`

Image ID: `cb312e5afa90b4c28d355cc38ee852ce521a4156fea1f8ea1ed0f23f202faf13`.

CPA target: `ghcr.io/xz-dev/cli-proxy-api@sha256:aaab5960f930abbfd9f1e69b23126c97fb33da93597bd0ab97a561e71ba257ef`
(`v7.2.146-xz.1`, revision `0f10a3635c39984d9eae139a4d7b3759f51ec6d0`).

The user confirmed CPA's known concurrent-write limitation and directed the client
to avoid it. A single process-local mutex now spans the fresh read, comparison,
PATCH and uncertain-result confirmation. Up to two channels still fetch in
parallel. No Core change, fixed waiting period, whole-config restore, persistent
queue or cross-channel transaction was introduced. Run **one active synchronizer
per CPA**; the mutex does not coordinate external management writers or other
processes.

Evidence:

- The scheduling test first failed against overlapping writes (peak 2), then
  passed with fetch peak 2 and write peak 1.
- Three consecutive real-CPA runs passed with the revised local build.
- The rebuilt final image's extracted release executable passed the existing
  real-CPA probe, including immediate GET read-back:

```text
first update:       updated=6 unchanged=0 failed=0
second round:       updated=0 unchanged=6 failed=0
manual model edit:  updated=1 unchanged=5 failed=0
following round:    updated=0 unchanged=6 failed=0
after CPA restart:  updated=0 unchanged=6 failed=0
filtered empty:     updated=6 unchanged=0 failed=0
following round:    updated=0 unchanged=6 failed=0
```

Other fields, disabled state and unmanaged Gemini/Interactions were preserved.
All configuration and credentials were synthetic, with external networking disabled
and no APISIX dependency. Final-image output: `/tmp/cpa-final-image-sjhzgyw8/result.txt`.

For historical context, the previous uncoordinated candidate failed read-back.
`tests/compatibility/concurrent.go` reproduced the limitation independently of Rust
using two standard Go HTTP clients: 10 rounds included two cases where both PATCH
requests returned 200 but GET returned an old model. That evidence motivates the
client workaround; it is **not a remaining prerequisite to fix Core or change CPA
images**. Historical outputs are in `/tmp/cpa-release-probe-skb5a48y/`.

## Final checks

| Check | Result |
| --- | --- |
| Rust fmt --check, locked tests, all-target Clippy -D warnings | PASS |
| Go test -race ./... and go vet ./... | PASS |
| Front Lua JSON/authorization/ETag contract | PASS, seven checks |
| Actual layered Go → APISIX → sidecar → front HTTP fixture | PASS |
| Docker Compose v2.39.4 and installed Podman Compose rendering | PASS |
| New direct CPA two-member network, non-root/read-only/no ports/no external network | PASS |
| git diff --check | PASS |
| OpenSpec strict validation | PASS |
| Full gateway validation | Existing failures remain; NOT PASS |
| Independent model-based review | Waived by user; NOT performed |

The HTTP fixture verifies numeric/unknown-field fidelity, entitlement-first
handling, native failure, the 16 MiB cap, cache HIT and path restrictions. Outputs:
`/tmp/cpa-final-checks-serialized/`. The separate front Lua and HTTP checks were
re-run after the final write-coordination change.

Existing gateway failures, intentionally not repaired:

1. `default egress policy differs from the exact control-plane baseline`.
2. Its independently executed Compose-boundary section forbids image digests,
   while HEAD already pins `model-catalog-sidecar` to a digest.

The second check is not a substitute for passing the complete gateway script.
UI/Pi-specific live/read-only client regressions were not newly executed; no Pi
plugin, UI code, chat filtering or limit-fallback behavior was changed. See
[EVIDENCE.md](EVIDENCE.md) for the delta-scenario mapping and uncovered claims.

The final image uses Rust 1.96.0 and Cargo.lock. Online crate-index access stalled;
the successful local build mounted the existing registry cache read-only and used
`CARGO_NET_OFFLINE=true` with `--network=none`. Docker Compose was downloaded and
checksum-verified in a task-local `/tmp` directory. No system package was installed.

## Fixed five-round final-image measurement

Command: `python3 tests/resources/measure.py`.

The release executable runs alone in its non-root/read-only/capability-dropped
container and cgroup. A separate mock CPA supplies two inventories, each with
10,000 records and a 30,420,041-byte encoded response. CPU limit: 0.5. Memory limit:
256 MiB. The interval is shortened to 10 seconds. Exactly five rounds were measured;
no open-ended allocator tuning followed.

| Round | Summary | Completion from start | Post-round Rust RSS | Container memory |
| --- | --- | --- | --- | --- |
| 1 | updated=2 | 6.074 s | 41,872 KiB | 39.87 MB |
| 2 | unchanged=2 | 15.324 s | 22,096 KiB | 19.90 MB |
| 3 | unchanged=2 | 25.074 s | 22,104 KiB | 19.91 MB |
| 4 | unchanged=2 | 35.075 s | 22,608 KiB | 20.53 MB |
| 5 | unchanged=2 | 45.074 s | 22,640 KiB | 20.53 MB |

First observed RSS: 3,712 KiB. Peak process RSS: **179,544 KiB (~175.3 MiB)**.
Final post-round RSS: **22,640 KiB (~22.1 MiB)**. Cold-to-first-summary includes
container startup and full maximum-catalog work, not just initialization.

The last two container samples were equal, while process RSS changed slightly.
Five rounds do **not** prove long-term zero growth. The roughly 5 MiB warm-idle
aspiration was not achieved for this workload. Standard HTTP/TLS/JSON and the
platform allocator were retained; no custom allocator, forced trim loop or hidden
helper was added. The separate mock container used 192.1 MB, disclosed separately
rather than attributed to or hidden from the Rust process.

Raw result: `/tmp/cpa-sync-memory-ks_l032d/result.json`.

The **previous** candidate was OOM-killed at 192 MiB on one run. Its older 256 MiB
numbers are not substituted for this final image. The Compose limit remains 256
MiB to provide peak-memory headroom. Resource task 5.3 retains the unproven long-term
no-growth/idle-target limitation rather than being marked fully accepted.

## Delivery boundaries

Functionality is frozen at the validated client workaround. The former Core
prerequisite is withdrawn. Required gateway checks that still fail and resource
claims that remain unproved stay visibly open in OpenSpec; review is recorded as a
waiver, not a pass. All changes remain uncommitted. The subsequently authorized production attempt was
rolled back after a catalog-size regression; see PRODUCTION.md. No push, Core
modification or unrelated CI repair occurred.
