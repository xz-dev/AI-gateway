## Why

`apisix-models` chooses CPA for IDs containing `/`, and AISIX for other IDs. The operator wants priority-based routing: prefer AISIX, use CPA only when AISIX does not define the model, and allow a later backend as another ordered entry rather than a naming convention. The operator also proposed checking AISIX's model list on every request and asked for measured latency versus error-based fallback.

## What Changes

**Planning only. Implementation is blocked on user decisions A–D in `design.md`. No mechanism, slash-ID exception, bare-ID policy or relaxation of the existing no-lookup/cache rule has been approved by these artifacts.**

- **BREAKING (proposed routing rule):** replace the literal slash selector with one ordered backend list, initially AISIX then terminal CPA. Advance only on established model absence, never on auth, quota, target, transport or stream failure.
- Compare three mechanisms, not three runtime switches:
  - **L, recommended subject to approval:** direct AISIX `GET /v1/models` for every classified request, then native APISIX inference proxying. Saved measurements: 1.122/63.357/86.513 ms p50/p95/p99 for the reused-connection list round trip, plus separate 0.116/0.645/1.422 ms decode/set measurements. The list is 6,567 bytes with 76 IDs.
  - **T:** a bounded, per-caller/per-worker 1–5 s membership cache. It trades freshness and cache complexity for fewer lookups; cache performance has not been measured.
  - **E:** actual AISIX request first, then replay to CPA only for pre-commit 404 JSON with `error.type = "model_not_found"`. Measured AISIX miss round trip is 1.409/100.123/115.721 ms p50/p95/p99; full replay/streaming-relay overhead is unmeasured.
- AISIX list membership is **key-filtered** and excludes wildcard definitions. Current managed keys have full access and current definitions are concrete; list rollout must verify those live invariants or obtain a separately approved safe policy. A missing ACL entry is not evidence of nonexistence. Failure/timeout must not fail open to CPA.
- Required routing decisions:
  - **B:** pure priority changes 53 AISIX-defined slash IDs from direct CPA to AISIX. Alternative: retain an explicit slash→CPA compatibility exception. Inventory and credential/accounting/session effects are in the design.
  - **C:** permit or reject CPA-only bare IDs, which CPA's default prefix behavior registers but the catalog omits. Any permitted invocation must retain Sub2API authorization; visibility is not authority.
  - Unknown-everywhere IDs can return CPA's final 400 instead of AISIX's 404 if eligible for terminal fallback; preserve current downstream safe-error handling.
  - **D:** explicitly revise the current no-lookup/index/cache and no-cross-backend-retry rules to match the selected mechanism. Do not silently choose an exception.
- Preserve classified paths (`POST /v1/responses`, `/v1/chat/completions`, `/v1/images/generations`), body limits/validation and `prompt_cache_retention` stripping, multipart edits→AISIX, catch-all/direct WS→CPA, and catalog routing/membership/order/priority. No Sub2API or AISIX retry-policy change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cpa-first-catalog-routing`: decision-gated priority selection and absence evidence, while preserving credential and retry boundaries, default CPA prefix registration, and the existing WS chain.
- `apisix-cpa-gateway`: replace slash-rule passthrough with priority passthrough at the same HTTP seam.

The deltas describe common guarantees and explicitly conditional alternatives. Task 0 must replace those alternatives with the operator-selected contract before apply or archive.

## Impact

- Selector and unit tests: `services/routing/apisix-models/routing_selector.lua` and `routing_selector_test.lua`.
- A local APISIX 3.19 stub-backend harness and integration into `ops/scripts/validate.sh`; exact source/config/startup changes depend on A. L can retain native upstream proxying and current credential injection; E needs a separately verified streaming relay lifecycle.
- Documentation: keep the service README short; put policy, latency limits, authorization and activation details in `docs/model-routing.md`.
- Runtime: L adds one local list request per classified inference (observed peak 5 requests/s, about 32.1 KiB/s list body traffic); T adds bounded cache state; E adds replay and streaming-relay responsibility. None of those costs is approved yet.
- Deployment: existing `deploy.yml -e component=apisix-models` handles guarded image/env transactions, not tracked Lua files. Plan one reviewed manual source-file activation/recreate with saved-file rollback; do not overlap other production windows.
- No implementation, secret/config mutation, production deployment, git staging or commit is part of this planning task.
