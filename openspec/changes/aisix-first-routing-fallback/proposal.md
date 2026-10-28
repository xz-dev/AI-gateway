## Why

`apisix-models` currently selects CPA for IDs containing `/` and AISIX otherwise. The operator approved priority-based routing: try AISIX first for every classified inference request, then CPA only when AISIX itself proves the model is undefined. This removes backend-specific naming conventions and permits later components through another ordered backend entry.

The operator also requested comparison with per-request AISIX model-list lookup. Research found that listing is permission-filtered and omits wildcard definitions, so list omission is not an authoritative existence signal. The operator selected error-based fallback, pure priority for slash IDs, and authorized fallback for CPA-only bare IDs.

## What Changes

**Planning only. User policy decisions are resolved; runtime implementation remains gated on a local APISIX 3.19 prototype.**

- **BREAKING:** replace literal slash selection with one Lua backend list: AISIX (caller credential, exact local-miss predicate) → terminal CPA (`CPA_API_KEY`, never advances).
- Send every classified JSON inference request to AISIX first, regardless of slash or bare ID. Fall back at most once, only before response commitment, for HTTP 404 with a complete JSON body whose top-level `error.type` is exactly `model_not_found`.
- Pass all other AISIX responses through unchanged at the inner seam, including `upstream_error` with `error.code=model_not_found`, other 404s, auth/permission errors, 429 and 5xx. Transport errors/timeouts retain existing client-visible failure semantics and do not retry. CPA's response, including unknown-model errors, is terminal and unchanged.
- No model-list lookup, routing index, membership cache or last-known-good state. Retain the documentation prohibition on indexes/caches and document this one narrow cross-backend retry exception.
- Buffer a replayable validated request within existing bounds: 64 MiB for Responses, 16 MiB for other classified JSON. Preserve bytes across both attempts after the existing `prompt_cache_retention` removal, without decode/re-encode between attempts. Preserve file-backed handling; reject over-limit requests before any attempt.
- Log each actual AISIX→CPA fallback once at INFO with bounded, escaped model ID and request ID; no credentials or bodies in logs.
- The 53 AISIX-defined slash direct models now enter AISIX, adding its accounting and CPA-pool credential hop. Preserve `session_id` and verify error/credential boundaries. CPA-only bare IDs can fall back when authorized; catalog membership remains unchanged and Sub2API authorization still applies.
- Keep the classified seam: `POST /v1/responses`, `/v1/chat/completions`, `/v1/images/generations`. Preserve multipart edits→AISIX, messages/direct WS/catch-all→CPA, catalog routing/order/priority, validation and downstream safe-error handling.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cpa-first-catalog-routing`: ordered AISIX-first routing, exact pre-stream miss predicate, terminal CPA fallback, credential and retry boundaries; preserve default CPA prefix registration and existing WS chain.
- `apisix-cpa-gateway`: priority passthrough at the existing classified HTTP seam, bounded replay and incremental SSE.

## Impact

- Selector/tests: `services/routing/apisix-models/routing_selector.lua`, `routing_selector_test.lua`, and a local APISIX 3.19 stub harness wired into `ops/scripts/validate.sh`.
- Prototype must prove byte-identical replay, incremental SSE/timing, credential swap, non-miss error boundaries, and large/over-limit bodies before the relay mechanism is locked. A Lua `resty.http` streaming relay is the candidate; native status-only interception is insufficient.
- Documentation: short service README; policy, rejected list/cache alternatives, measured latency, deployment and rollback details in `docs/model-routing.md` during implementation.
- Runtime: no discovery round trip on AISIX hits; misses add a measured 1.409/100.123/115.721 ms p50/p95/p99 AISIX miss round trip, plus unmeasured replay overhead. All successful streams acquire relay responsibility; no zero-overhead claim is made.
- Deployment: existing `deploy.yml -e component=apisix-models` handles guarded image/env transactions, not tracked source-file installation. Use one reviewed manual source-file activation and component-only recreate, with saved-file rollback; do not overlap catalog Lua or public-list production windows.
- This planning update changes no code, production state, secrets, staging or commits. All implementation tasks remain unchecked.
