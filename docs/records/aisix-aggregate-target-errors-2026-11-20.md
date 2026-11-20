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

On 2026-11-20 the owner authorized unsigned commits for this change in the operator pane. The final source and [publisher commit](https://github.com/xz-dev/aisix/commit/c6cc9e489181909cd498b5618f7fa5d4f47c8289) used per-command `--no-gpg-sign`; signing configuration was not changed. The publisher tree differs from the final source only in `.github/workflows/ghcr-ci.yml`. [Final image CI](https://github.com/xz-dev/aisix/actions/runs/44712584914) was triggered. The first image's digest and acceptance results are not evidence for this final candidate.
