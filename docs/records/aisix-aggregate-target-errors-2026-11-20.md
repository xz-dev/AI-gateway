# AISIX aggregate target errors — 2026-11-20

## Source acceptance

The [source fix](https://github.com/xz-dev/aisix/commit/adf73210b1fcfcef4d4c1fe90b2ee87e903242f0) is based on deployed `6734303611311efb336470cd63f91da75dfa141a`. It changes the terminal pre-output error for an explicit aggregate, after retry/fallback selection. A structured upstream 400/404 with `error.code=model_not_found` becomes a safe 502. The deployed CPA fork (`3d1f7004`, `sdk/api/handlers/handlers_routing.go`) uses that same code for an unknown provider. No match on human-readable text is needed. The existing no-candidate path returns 503 as before. The common routing predicate used by retries, quotas and telemetry is unchanged.

Loopback upstream mocks and the real proxy handlers exercised these cases without provider inference:

| Input | Expected result | Observation |
|---|---|---|
| Valid aggregate, primary returns 503, backup returns structured missing-model 400 | Call each target once; terminal 502 | Deployed source returned 400. Fixed Responses and Chat handlers returned 502 with no internal target in the client message. |
| Valid aggregate, confirmed primary cooldown, backup reports unknown provider | Skip primary, call backup once; terminal 502 | Deployed source returned 400. Fixed handlers returned 502. |
| No eligible candidates under fail-fast policy | Existing 503, `all_candidates_unavailable` and retry hint | Preserved; no upstream calls. |
| Policy 400/403 or unrelated 400 | Original status and error envelope | Negative controls passed on fixed source. |
| Unknown public model, direct target rejection, healthy fallback | Preserve caller error, direct error, and success respectively | Controls passed; a separate nonaggregate semantic-router test confirms no reclassification at the terminal error boundary. |

With the new classification disabled, seven positive tests failed with actual 400 versus expected 502, including both Responses paths. An overbroad mutant also failed the direct-model negative control. On the signed final source, `cargo test --locked -p aisix-proxy --lib aggregate_target_errors` passed all 14 focused tests. `cargo test --locked -p aisix-proxy` passed 1,723 library tests and 25 integration tests. These are local source checks, not published-image or production acceptance.

## Publication

The signed [publisher revision](https://github.com/xz-dev/aisix/commit/cd330d3704a8ddfdbecbd2156f2da486cf2616c1) uses the existing `ci/capacity-v1.10.0` workflow. Its tree differs from the approved source only in `.github/workflows/ghcr-ci.yml`. [Fork image CI](https://github.com/xz-dev/aisix/actions/runs/44705128706) was triggered; a workflow submission alone is not evidence of an admitted image or deployment.
