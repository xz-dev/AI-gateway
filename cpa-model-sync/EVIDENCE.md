# Delta scenario evidence

Paths below are relative to `AI-gateway`. This original mapping is local evidence,
not acceptance of either rolled-back production attempt. The 32 MiB candidate was
withdrawn after correcting NIM/ShuaiAPI policy; the subsequent production retry
encountered Sub2API OOM before candidate public-load acceptance completed.
See [CATALOG-LIMIT.md](CATALOG-LIMIT.md) and [PRODUCTION.md](PRODUCTION.md).

PASS means the referenced local automated
check was executed; source-only / not re-run is explicitly weaker evidence. No
production or live Pi/UI result is inferred from a server-side fixture.

## cpa-model-sync

| Scenario | Evidence |
| --- | --- |
| Enabled and disabled providers both refresh | PASS: final-image `tests/real_cpa.go`; enabled/disabled inventories updated without activation |
| OAuth is left alone rather than excluded | PASS: `tests/discovery.rs` rejects unmanaged/auth-files requests; Go OAuth preservation tests |
| Missing or ambiguous channel identity is not guessed | PASS: `tests/discovery.rs`, no fetch/write on ambiguous identities |
| Later pages are not silently deleted | PASS: `tests/inventory.rs`, complete two pages and failed later page |
| Empty original inventory preserves configured models | PASS: inventory table and retry-exhaustion tests |
| Invalid success body is not an empty inventory | PASS: inventory envelope/record/size/cursor cases |
| Existing include and exclude semantics remain recognizable | PASS: inventory include/exclude precedence table |
| Client version is preserved for parameterized catalogs | PASS: inventory request assertions and synchronization path override |
| Invalid filter never causes destructive refresh | PASS: inventory invalid-regex case |
| All models intentionally filtered out | PASS: inventory table and final-image CPA filtered-empty round |
| CPA default fallback is not counteracted | PASS: real-CPA zero configured models and unchanged non-model fields; code writes only models, not fallback controls |
| Unchanged current models cause no mutation | PASS: synchronization HTTP counts and final-image immediate second round / restart |
| Reordered equivalent model records do not cause churn | PASS: library canonical-comparison test |
| Manual model edits are not protected | PASS: real-CPA manual alias/extra-model overwrite |
| Non-model fields remain untouched | PASS: real-CPA full snapshots excluding models, including unmanaged providers |
| Current-state read failure is not mistaken for equality | PASS: retries current-unreadable case, zero writes |
| 并发抓取不产生并发 CPA 写 | PASS: scheduling test red at write peak 2, green at fetch peak 2 / write peak 1; final-image CPA probe |
| Retry succeeds on fourth attempt | PASS: retries fetch-fourth and write-fourth |
| Exhausted upstream retries do not block healthy providers | PASS: retries counts and CLI isolation A-failed/B-updated |
| Write response lost after CPA accepted the update | PASS: retries response-lost and unconfirmed cases |
| Slow rounds do not accumulate scheduled work | PASS: controlled elapsed-time unit cases and slow-channel daemon test |
| Warm idle rather than startup alone is measured | PASS: fixed five-round final-image resource run; numerical evidence in VALIDATION.md |
| Standard client exceeds the optimization target | Measured and disclosed: ~22 MiB post-round RSS, no custom infrastructure; long-term no-growth remains unproved |
| Synchronization reaches CPA without APISIX | PASS: network-none real-CPA probe; Compose direct internal pair |
| Disabled provider uses transient CPA-supplied credentials | PASS: real-CPA fixture with absent live auth index |
| Disabled provider cannot supply a forwarding credential | Source guard: missing required credential fails before api-call; no separate final-image missing-credential scenario was run |
| Failure evidence does not expose secrets | PASS: synthetic-key assertions in discovery/isolation; static public error strings; no raw production response used |

Rust evidence paths in this section are under `cpa-model-sync/`.

## enricher-source-priority

The existing Go suite was re-run with `go test -race ./...` and `go vet ./...`.
Metadata fidelity implementation was not rewritten. Three old fixtures were only
updated to explicitly contain their expected managed member in the CPA baseline.

| Scenario | Evidence |
| --- | --- |
| exact channel configuration required | PASS: existing config/runtime-validation and handler tests |
| provider-qualified exact lookup | PASS: source lookup tests in `models-enricher/enricher_test.go` |
| model-level whole-list replacement | PASS: existing channel/model source-chain tests |
| source-specific lookup ID | PASS: lookup tests and final Go HTTP fixture |
| tag remains literal without lookup override | PASS: exact-ID lookup tests; no fuzzy resolution added |
| overrides always win | PASS: precedence and new failed-fetch enrichment test |
| source chain outranks channel capability templates | PASS: `TestMergeProviderChainPrecedence` |
| explicit input limit only | PASS: existing metadata/source limit fixtures |
| source miss preserves channel metadata | PASS: existing null/missing metadata tests |
| source order is independent of fetch order | PASS: existing deterministic merge/source tests |
| Extra upstream member contributes no public identity | PASS: new ownership test across all five managed kinds |
| Missing upstream metadata does not delete a CPA member | PASS: ownership A-only/B-only/nil cases and actual HTTP fetch-failure test |
| Filtered model is not resurrected by metadata fetching | PASS: managed ownership gate; upstream-only B absent |
| Explicit static alias remains a supported exception | PASS: existing static tests and new mixed boundary test |
| Disabled refresh is not activation through enrichment | PASS: mixed disabled/static/OAuth test plus CPA disabled-state preservation |
| Unmanaged Gemini and Interactions retain existing behavior | PASS: both kinds retain upstream membership and Go exclusion test |
| Stale filter configuration is not silently misleading | PASS: runtime validation rejects managed include/exclude; committed config migration inventory documented |

## codex-models-catalog

| Scenario | Evidence |
| --- | --- |
| 网关完整而 Pi 仅列对话模型 | Server completeness verified by existing metadata/HTTP fixtures; live Pi filtering not re-run, Pi code unchanged |
| 合法对话模型缺 limits | Existing client behavior unchanged; no new live Pi regression executed |
| 仅缺输出上限沿用客户端既有行为 | Existing client behavior unchanged; no new live Pi regression executed |
| 未知模型元数据穿过授权交集 | PASS: seven front Lua checks and real layered HTTP fixture, including large integer/decimal/unknown-field fidelity |
| 上游独有模型不能绕过 CPA 集合 | PASS: Go five-kind ownership tests and unchanged front entitlement boundary |
| OAuth 未管理但仍可见 | PASS: Go mixed/native tests and final real CPA unmanaged-kind checks |
| Zero configured models may coexist with CPA defaults | PASS: real-CPA configured-empty read-back; no fallback-disabling field is written |
| Newly synchronized model does not grant caller entitlement | PASS: existing entitlement-first/unauthorized-cold front HTTP cases and Lua intersection checks |
| Successful refresh respects the cache window | PASS: existing actual HTTP proxy-cache HIT and final-entity ETag tests; no claim of instantaneous propagation |

## Remaining acceptance limits

- Five maximum-catalog rounds are not proof of long-term zero memory growth, and
  warm idle exceeded the approximate 5 MiB aspiration.
- Complete gateway validation retains pre-existing egress-baseline and image-digest
  policy failures; those checks were not relabeled as passing.
- Live Pi/UI regressions and a dedicated final-image missing-forwarding-credential
  test were not newly executed; source-only evidence must not be upgraded to a run.
- Independent review was explicitly waived by the user.
