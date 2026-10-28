## Why

The operator wants ordered backend selection without model-name conventions, but clarified the governing principle: **the gateway is a streaming pipe, not a bigger bucket**. Decision A is revised: reject error-driven fallback and whole-request replay. Choose one backend from a bounded request prefix and a background AISIX membership snapshot, then stream once with no cross-backend retry.

AISIX 1.5.0 resolves the model only after Axum has extracted the complete JSON body. Its precise `404/error.type=model_not_found` signature therefore cannot support replay-free fallback. The local replay prototype also reproduced a pre-existing 64 MiB OOM in today's full-body cjson selector. The revised pipe must remove that decoder and pass 64 MiB under the existing 128 MiB container cap.

## What Changes

**Planning only. Do not run the new prototype or implement until the supervisor confirms this revised plan. Retention filtering and whole-body validation semantics require explicit user decisions.**

- **BREAKING:** replace literal slash routing with one ordered list: AISIX (caller credential, exact membership snapshot) → terminal CPA (`CPA_API_KEY`, no membership source). Pure priority applies to slash and bare IDs; authorized CPA-only bare IDs become callable without changing the catalog.
- Read classified request bodies incrementally with `ngx.req.socket()` before any `read_body`. Retain at most `MAX_ROUTING_PREFIX_BYTES` (**value pending client survey; see design**), use bounded JSON scanner depth/model length, and forward prefix plus remaining chunks through a backpressured `resty.http` iterator. Never retain/spool a complete request for replay or decode it into a complete JSON object.
- Refresh an atomic per-worker AISIX exact-ID set in a background timer, proposed every 3 seconds, with a dedicated full-visibility discovery service key never used for inference. No per-request discovery network call.
- Route uncertain input to AISIX with the caller key, without fallback: model missing beyond the prefix cap, malformed prefix, duplicate top-level model in the retained prefix, cold or expired snapshot. After refresh failure, use a previous snapshot only to a proposed 15-second maximum age; warn with rate limiting.
- Forbid wildcard AISIX model definitions through both local and Ansible resource validation. The list is otherwise wildcard-incomplete and permission-filtered; the dedicated key must see every concrete definition.
- Stream one request and its response, including incremental SSE; preserve cancellation, safe headers, credentials and HTTP framing. Selected-backend errors, including model-not-found, ACL denials and transport failures, are terminal.
- Keep ingress size ceilings at 64 MiB Responses / 16 MiB other classified JSON. With true streaming, a chunked over-limit upload may have sent bytes to its selected backend before the limit is reached; it must stop without retry, not claim pre-dispatch whole-body validation.
- Inventory current retention removal and full-body validation exactly. Propose a bounded streaming top-level key filter or backend-owned compatibility handling, but do not silently decide either or claim unchanged validation semantics. Keep these as blocking user decisions.
- Explicitly revise the documentation's cache prohibition: allow only this bounded, background-refreshed membership snapshot. Keep `/routing-index`, catalog-derived membership and unbounded last-known-good decisions prohibited.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cpa-first-catalog-routing`: ordered snapshot-based selection, bounded prefix uncertainty and age policy, dedicated discovery credentials, no cross-backend retry, wildcard validation and unchanged authorization/catalog.
- `apisix-cpa-gateway`: bounded streaming HTTP pipe, request/response framing, 64 MiB resource gate and explicit compatibility decision gates.

## Impact

- Runtime work after approval: `services/routing/apisix-models/` selector, config, template and tests; real APISIX 3.19 streaming harness at the existing `ops/scripts/validate.sh` seam. No new dependency/service is selected.
- Discovery provisioning: dedicated AISIX key, private caller-key schema and key-generation/verification paths, Compose/Ansible secret delivery and matching examples. No secret values enter Git. This is a coupled configuration/source activation, not merely selector-file deployment.
- Validation: extend `ops/scripts/check-aisix-resource-policy.py` and the Ansible AISIX deployment seam to reject wildcard definitions without changing the seven intentionally retained Axis entries or other routing/cooldown policy.
- Documentation: short component README; technical decisions, new snapshot exception, limits, byte-filter semantics and rollback in `docs/model-routing.md`; reconcile affected topology/spec text.
- Old replay prototype and its memory ablation remain rejected-alternative evidence, not acceptance of the new pipe. Task 0.1 must prove bounded memory across 1/16/64 MiB, size-independent first upstream byte, byte identity, SSE, cancellation, chunked uploads and uncertain-to-AISIX behavior.
- No runtime implementation, production writes, new production probes or pipe prototype execution in this planning update. All implementation tasks remain unchecked.
