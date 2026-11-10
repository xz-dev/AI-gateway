# Maintenance and capacity deployment closeout — 2026-11-09

## Disposition and chronology

[PR21](https://github.com/xz-dev/AI-gateway/pull/21) and
[PR22](https://github.com/xz-dev/AI-gateway/pull/22) are merged and deployed.
Operational acceptance is **PASS**, not full capacity acceptance, issue #46
completion or authorization to archive `large-request-capacity`.

The November 2 PR1387 AISIX publisher and Sub2API v0.2.19 migration rehearsal
remain source history, not current desired releases. The November 9
[rollout receipt](chat-errorfix-rollout-2026-11-09.md) and
[17:46 stopped attempt](chat-errorfix-rollout-stop-2026-11-09.md) retain their
original failures and admission refusal. A later separately authorized retry
activated the final error-only patch. Do not reinterpret the stopped attempt
as a successful deployment or restore its previous resource configuration.
Historical 77 error envelopes were not retained; their exact codes remain
unknown. They were not erased or reclassified as post-readiness failures.

## Final deployed releases

Full content digests are in `ci/large-request/ci/compatibility-lock.json`.

| Component | Digest prefix | Reviewed source / release |
| --- | --- | --- |
| Sub2API | `56557012dc75` | `02918ea957f0`, v0.2.19-based capacity fork |
| keeper | `ef4dc093e218` | `d7cd0bc5d07b` |
| WS/SSE bridge | `dede337bbe10` | `3a9c76b223fe` |
| AISIX | `b9d01af4a8d9` | capacity fork, supersedes PR1387 publisher |
| CPA | `4835aa6c189f` | capacity fork, v8.0.51 + PR7134 lineage |
| front APISIX | `9a7e45dc943f` | 3.19.0 |

Final observation at **18:39:17Z**, mount readback at **18:40:32Z**, and final
cleanup observation at **18:49:52Z**: 48 services running/healthy where configured;
47 unrelated container identities, 48 namespace inodes/topology/bindings and
204 security fingerprints unchanged. Sub2API had restart count 0 and no OOM.
Its production limits remained **512 MiB memory, 832 MiB combined memory+swap
(actual swap allowance 320 MiB), CPU 1.5, GOMEMLIMIT 288 MiB**. The isolated
PR ladder's Sub2API remains 320 MiB/no swap; that is not production parity.
No restarts occurred during cleanup. Interruption duration for the final
activation is not established by these observational receipts.

Disk-backed mode-0700 anonymous spools and `nocopy` mounts, keeper-writable /
inner-hop-read-only ledger, and the loaded front `1024m` transport backstop
with **256 MiB declared-length guard** remained in place. Production cap is
256 MiB; **1 GiB is a throwaway nightly-only target**, never a production cap.
The guard's local regression result and loaded configuration are not a
successful production overcap test.

At 18:39, existing traffic since 18:22:54 included 51 Chat 200, 36 embedding 200,
1 rerank 200 and 1 Responses 200 at Sub2API; front had 2 Responses upgrades (101).
At 18:49, cumulative existing post-readiness Sub2API completions were
**76 Chat 200 + 59 embedding 200 + 2 rerank 200 + 24 Responses 200**, with
front/Sub2API 5xx 0. No paid synthetic calls were added. An HTTP status or WS
upgrade alone does not prove a provider-terminal event. Earlier transaction
three Chat 502s and the historical 77 failures remain historical evidence.

## Accounting and evidence limits

Reviewed [Sub2API source](https://github.com/xz-dev/sub2api/tree/02918ea957f0639db932b23e7e5d00a6f2298308/backend/internal)
sets `DefaultBodySpoolKSkel=8` in `config/body_spool.go`. Its
`handler/body_spool_conversion.go` charges per class (worst total allocation
plus 15%, not a universal multiplier): Responses 114, Responses fallback 107,
other-platform Responses 143, Chat 260, Messages 159, count_tokens 156,
Gemini 147, web_search 65, **embeddings 65, rerank 65**, unmeasured 260.
The lock's `footprint.basis=declared` and historical Sub2API `k_skel=17`
remain provenance, not a new measurement or the current source default.
The final five-line Chat fix changes error handling, not those factors.

Two accounting layers must not be conflated. The host keeper gate uses real
MemAvailable (which counts dirty pages as available) and anonymous+kernel
charges; it does not add dirty/writeback again or credit swap/GC. The local CI
working-set criterion in `ci/large-request/hlib.py` and `ci/verdict.py` is
conservatively `anon + file_dirty + file_writeback + kernel` for keeper/AISIX,
excluding reclaimable clean file cache; raw `memory.peak` is retained separately.
The lock is not proof that all declared factors were measured on production.

The PR ladder's CPU quotas are recorded in the
[PR22 release record](chat-capacity-errors-pin-2026-11-09.md). Full production
CPU-quota parity and Squid TLS-bump-path parity are not established by the
five-hop PR ladder; the extended nightly path is separate evidence. Retain
these deviations instead of calling the local mock chain production-equivalent.
AISIX `stream_timeout_ms` stays 120000; no new WAN throughput guarantee is made.

Three capacity checks remain open:
1. **245,000,000-byte admission is best effort**, not always granted: final
   three ten-second samples admitted/refused/refused, with memory slack
   +27,454,560 / -40,629,152 / -13,427,616 B. No such model request was sent.
2. Realistic **8 MiB production success remains unobserved**.
3. The single header-only production overcap attempt timed out before the
   front correction. The corrected guard is loaded and locally tested but
   **production-unretested**. Preserve the one-attempt marker; do not replay
   it or report a production 413 PASS from local tests.

A +24h read-only observation is a follow-up, not a sleeping tool or permission
to reopen the completed rollout. Pending examples/specifications remain goals.

## Authorized cleanup

After independent operational acceptance, exact-manifest cleanup removed
obsolete unreferenced rollout images and the unmounted old bridge spool,
then the obsolete `340273…` Sub2API image, three original controller backups
(268,158,912 logical bytes), and two existing-key **copy files** (67 B each).
The API key resource/source was unchanged; no key revocation was implied.
No broad prune, volume wipe, database or active spool deletion was authorized.

Measured filesystem availability, not summed image sizes:
- Earlier six-entry stage1 production window: **+617,938,944 B**.
- Final production window: **3,870,560,256 → 4,047,486,976 B**, gain
  **176,926,720 B** (separate from stage1).
- Controller after one bounded sync: **293,525,258,240 → 293,791,858,688 B**,
  gain **266,600,448 B**, not production bytes.

Concurrent writes/accounting delay prevent exclusive attribution of these
filesystem deltas. Final disk available 4,047,433,728 B and MemAvailable
1,083,236,352 B preserved the 1 GiB disk / 200 MiB memory floors plus 32 MiB
margin, with 0 tickets and only `ledger.lock` at idle. Active images, current
rollback/configuration snapshots, databases, auth/config, keeper cache including
its hidden legacy owner, active spools/ledger, unrelated old rollback aliases
and small sanitized receipts were retained. Private evidence can still contain
credential copies; this receipt authorizes no wider search/deletion.

## Repository integration versus deployment configuration

The public defaults now use the exact final five application lock digests;
keeper consumes the published image rather than rebuilding its stale submodule.
For development only, explicitly build the preserved source into a separate
local tag; do not tag a local build with a published digest or use it for rollout.
The historical AISIX publisher keeps immutable-tag refusal and is not rerun.

**Public template capacity enablement is pending.** The tracked template alone
has no shared ledger, inner-hop spool mounts, same-device/mount-mode validator,
or complete production flags/resource overrides. Existing cache/spool ownership
migration and `nocopy` adoption are not automatically guaranteed on an existing
volume. Image defaults alone do not reproduce the private production deployment
or promise large-body startup/request compatibility. No private mount/network
configuration was copied and no new infrastructure was introduced to tick 9.1.
Apply only a separately reviewed deployment configuration; never deploy these
templates over an existing host's private desired state. Capacity tasks 9.1/9.2
and unverified maintenance rollout tasks remain open/superseded.

Evidence basis: final independent verification and parent-authorized cleanup
receipts dated 2026-11-09, plus the published PR21/PR22 lock/source records.
This reconciliation performs no production operation or new capacity test.

### Integration-local startup evidence (2026-11-10 UTC)

Fresh throwaway volumes, final published images, `--network none`, no published
ports/provider calls: keeper passed its healthcheck. Bridge exited before
serving: default named-volume `/spool` was mode **0755**, while the final image
requires **0700**. The public template therefore has a confirmed **fresh-start
blocker**, not just incomplete large-body enablement. No template mount migration
was invented to hide this. Separate approval is required for ownership/nocopy
initialization and the shared ledger before a fresh deployment; existing
production mounts were already verified and are unaffected. Throwaway smoke
containers/volumes were removed. This is not full-stack startup acceptance.

Integration checks: `openspec validate --all --strict` passed 22/22;
`ops/scripts/validate.sh .env.example` reached
`runtime=docker compose=valid firewall=valid one_way_edges=valid private_paths=untracked`
within its 170-second bound, with no private override present. This scaffold
validator does not start the final bridge with its default volume: the runtime
blocker above remains. All 14 topology/pin tests passed; stale-pin and removed
digest-support negative controls failed as intended and were restored. AISIX
publisher actionlint passed; `git diff --check` and added-diff/new-artifact
privacy pattern review passed (not a guarantee against every possible secret).
No image publishing, fork write or production operation occurred.
