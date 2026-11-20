# AISIX aggregate target errors — 2026-11-20

## First candidate (not admitted)

The signed [first candidate](https://github.com/xz-dev/aisix/commit/adf73210b1fcfcef4d4c1fe90b2ee87e903242f0) is based on deployed `6734303611311efb336470cd63f91da75dfa141a`. It changes the terminal pre-output error for an explicit aggregate, after retry/fallback selection. A structured upstream 400/404 with `error.code=model_not_found` becomes a safe 502. The deployed CPA fork (`3d1f7004`, `sdk/api/handlers/handlers_routing.go`) uses that same code for an unknown provider. No match on human-readable text is needed. The existing no-candidate path returns 503 as before. The common routing predicate used by retries, quotas and telemetry is unchanged.

Loopback upstream mocks and the real proxy handlers exercised these cases without provider inference:

| Input | Expected result | Observation |
|---|---|---|
| Valid aggregate, primary returns 503, backup returns structured missing-model 400 | Call each target once; terminal 502 | Deployed source returned 400. Fixed Responses and Chat handlers returned 502 with no internal target in the client message. |
| Valid aggregate, confirmed primary cooldown, backup reports unknown provider | Skip primary, call backup once; terminal 502 | Deployed source returned 400. Fixed handlers returned 502. |
| No eligible candidates under fail-fast policy | Existing 503, `all_candidates_unavailable` and retry hint | Preserved; no upstream calls. |
| Policy 400/403 or unrelated 400 | Original status and error envelope | Negative controls passed on fixed source. |
| Unknown public model, direct target rejection, healthy fallback | Preserve caller error, direct error, and success respectively | Controls passed; a separate nonaggregate semantic-router test confirms no reclassification at the terminal error boundary. |

With the new classification disabled, seven positive tests failed with actual 400 versus expected 502, including both Responses paths. An overbroad mutant also failed the direct-model negative control. On the signed first candidate, `cargo test --locked -p aisix-proxy --lib aggregate_target_errors` passed all 14 focused tests. `cargo test --locked -p aisix-proxy` passed 1,723 library tests and 25 integration tests. These are local source checks, not published-image or production acceptance.

## Published image and shared-dispatch correction

The [first publish run](https://github.com/xz-dev/aisix/actions/runs/44705128706) passed on publisher `cd330d3704a8ddfdbecbd2156f2da486cf2616c1`. The published linux/amd64 manifest digest `8e3d9b23008ab78412c8cb34b7bc8f1260d9b0586681c98f271bd814c382d2cd` matched the registry; the source label named `adf73210`. On that image, the synthetic Responses paths changed from 400 to 502, with one call to each target after a primary 503, and only one call to the backup after confirmed primary cooldown. The deployed and candidate images agreed on public unknown-model, wildcard/direct, policy, unrelated 400 and healthy-fallback cases. Error comparisons covered the status and parsed message, type, code and param, not byte-for-byte serialization.

**Not admitted:** on the same first candidate source, a `/v1/completions` HTTP test with the same aggregate still returned 400 and named the internal model. Its shared failover helper returned the upstream error both on a nonretryable failure and on budget exhaustion. No production rollout used the first image.

[Final source `b05be5f0`](https://github.com/xz-dev/aisix/commit/b05be5f044fffccacf36226c328d824a695da436) applies the same aggregate-only classifier at both exits, after attempt accounting. The completions tests assert 502 after exhaustion and 502 with **no** backup call when failover is disallowed. All 16 focused tests passed; the full proxy suite passed 1,725 library and 25 integration tests. These are source-level checks.

On 2026-11-20 the owner authorized unsigned commits for this change in the operator pane. The final source and [publisher commit](https://github.com/xz-dev/aisix/commit/c6cc9e489181909cd498b5618f7fa5d4f47c8289) used per-command `--no-gpg-sign`; signing configuration was not changed. The publisher tree differs from the final source only in `.github/workflows/ghcr-ci.yml`. [Final image CI](https://github.com/xz-dev/aisix/actions/runs/44712584914) passed. The first image's digest and acceptance results are not evidence for this final candidate.

## Final image acceptance and compatibility lock

`ghcr.io/xz-dev/aisix@sha256:f064f7a8b7bfd756c0379765cda3f6a88c7e6f9f86b0b0d098498b1634e309ce` is the single linux/amd64 OCI manifest reported by the publisher, the registry tag `ci-b05be5f044` and the manifest byte hash. Its OCI revision is `b05be5f044fffccacf36226c328d824a695da436`; the binary reports `dev+sha-b05be5f`.

Local mock requests addressed this image by digest. The two Responses fallback cases returned 502; the cooling primary was not called. Both old images returned 400 on the `/v1/completions` exhaustion case; the final image returned 502 without naming the internal model. All three made exactly two calls in order. Public lookup, wildcard, direct-model, policy 400/403, ordinary 400 and healthy fallback controls retained their status and error fields or success. Native all-unavailable 503 and its retry hint were tested at source level, not re-created in the image harness. No production inference or end-to-end Sub2API failure was exercised.

The unchanged local compatibility ladder at `5599333` passed all ten default rungs in run `20261120T100145Z-2594413`. All eight fixture containers had zero restarts; every rung had `offlist=0`. The seven bounded services had zero cgroup swap before tests, using the previously verified project-scoped wrapper. The generated lock changes only AISIX among eight image pins and preserves the existing caps, resource limits and allocation factors. Its manifest records the harness-time Compose hash, not a hash of the later pin promotion. The fixture was removed; the nightly lock was not changed.

| Artifact | SHA-256 |
|---|---|
| Ladder manifest | `f6bf2877334441309d249a2e341c13f8239fc3ce5d77564d2dc12557d142b562` |
| Ladder summary | `3019bd78301e6ab6fe21179373694f7a640733c0454b5b557583190a5efa552e` |
| Generated compatibility lock | `a5c9fd242262109a5e1afd850256caf97b6d6967f1490f9b8dca6f3217d560ee` |

## Review and guarded deployment

Independent review approved gateway `c2ded795cedd9c9379d69edb285ca577085127aa` with the exact final source and digest above. The reviewer traced the nine shared-dispatch call sites, reran the focused source tests, and checked four additional published-image controls, including message-only errors staying 400. [Gateway Validate 44719035965](https://github.com/xz-dev/AI-gateway/actions/runs/44719035965) passed all four jobs at that revision. It was fast-forwarded to `main` without modifying the reviewed tree.

The fresh guarded AISIX preview reported 66 OK, zero changed/failed, 47 protected neighbours, no held writer lock, and no effective environment changes. Only the selected image changed in the Compose render; only the `AISIX_IMAGE` line changed in the three private source files. The old image was present by digest. Three ten-second-spaced inbound samples counted 5, 4 and 4 established connections. These are not counts of active generations.

- **Apply:** 11:14:54–11:16:30 UTC, 149 OK, 10 changed, zero failures. The replacement AISIX container started at 11:16:15 UTC. The 96-second controller duration is not measured downtime; the client interruption interval was not measured.
- **Identity:** live AISIX runs the reviewed digest and full `b05be5f0…` revision, running with zero restarts and no OOM. Only AISIX changed identity: all 47 other services retained their container and image IDs, start times, state and health. Both Compose files and unmanaged AISIX configuration were byte-identical before and after.
- **Zero-token acceptance:** `GET /status` returned 26 aggregates and 45 direct targets, with unchanged membership and target order. Live `resources.yaml` and its managed baseline both remained `671fc1e692350addbb1d865a32e5145f8a364be66f9825531c2708f98466947d`. No production generation request was made. The status-code regression was demonstrated against published images with mocks, not by breaking a live target.
- **Final preview:** after adopting only `AISIX_IMAGE` into the controller's private `.env`, the 11:19:51 UTC preview reported 66 OK, zero changes/failures, ready, `config_diff=false`, `activation_required=false`, and the writer lock released.
- **Rollback:** the previous `ghcr.io/xz-dev/aisix@sha256:9721ff105e2e568bcaa31c5e5ae4231ecce1c74df822619fd51f4e087e3c6108` and the guarded grouped snapshot remain available. Rollback requires a fresh AISIX rollback preview and its own approval digest. No rollback was needed.

**Controller follow-up:** the normal private controller copy of `resources.yaml` still hashes to `acff6bf6…`, the exact pre-November-20 route set. The live file and its managed baseline were already correct. This image-only transaction did not read that stale copy as desired state or upload it. Reconcile the controller copy with approved live resources before any later managed **resources** rollout; the clean **component** preview covers the three Compose/environment files, not this separate managed file.
