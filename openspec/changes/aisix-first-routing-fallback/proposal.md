## Why

The operator wants ordered backend selection without model-name conventions, but clarified the governing principle: **the gateway is a streaming pipe, not a bigger bucket**. Decision A is revised: reject error-driven fallback and whole-request replay. Choose one backend from an incrementally located model and a background AISIX membership snapshot, then stream once with no cross-backend retry. A single disk-backed sliding-window spool bounds memory without turning late model position into incorrect routing.

AISIX 1.5.0 resolves the model only after Axum has extracted the complete JSON body. Its precise `404/error.type=model_not_found` signature therefore cannot support replay-free fallback. The local replay prototype also reproduced a pre-existing 64 MiB OOM in today's full-body cjson selector. The revised pipe must remove that decoder and pass 64 MiB under the existing 128 MiB container cap.

## What Changes

**Planning only. Do not run the new prototype or implement until the supervisor confirms this revised plan. Retention filtering and whole-body validation semantics require explicit user decisions.**

- **BREAKING:** replace literal slash routing with one ordered list: AISIX (caller credential, exact membership snapshot) → terminal CPA (`CPA_API_KEY`, no membership source). Pure priority applies to slash and bare IDs; authorized CPA-only bare IDs become callable without changing the catalog.
- Read classified bodies incrementally with `ngx.req.socket()` before any `read_body`. Use one sliding-window spool with `append(chunk)`, `drain(sink)`, `close()` and a bounded `feed(chunk) -> needMore | model | uncertain` scanner. `MAX_ROUTING_PREFIX_BYTES=65,536` bounds its memory window, not the search distance. Automatic lazy disk overflow retains older bytes until a complete model is found; the same drain sends file then window, followed by the remaining stream. No early/late routing implementation branch, complete-object decoder or cross-backend replay.
- Bound per-request stored bytes by existing 64/16 MiB body ceilings; use a dedicated disk-backed private spool directory, proposed 256 MiB aggregate quota/reservations pending prototype review, and cleanup on success/error/cancel/restart. Measure disk cache/I/O under the unchanged 128 MiB cgroup. An accepted valid late model gets the same backend as an early one; resource exhaustion fails explicitly, never by guessing AISIX.
- Require the separately approved keeper model-first upstream release/pin before routing rollout. Reuse its raw span/existing buffer/spool, leaving passthrough paths and other fields unchanged; no generic small-field sorter. Completed client/chain survey justifies the 64 KiB memory window but does not guarantee final model order.
- Refresh an atomic per-worker AISIX exact-ID set in a background timer, proposed every 3 seconds, with a dedicated full-visibility discovery service key never used for inference. No per-request discovery network call.
- Route uncertain input to AISIX with caller key, without fallback: no trustworthy top-level model at complete-body EOF, malformed/untrustworthy JSON before decision, duplicate model detected before commitment, or no usable snapshot. Model not yet found at 64 KiB is `needMore`, not uncertainty. Later duplicates/malformed suffixes remain an explicit validation decision. After refresh failure, use a previous snapshot only to a proposed 15-second maximum age; warn with rate limiting.
- Forbid wildcard AISIX model definitions through both local and Ansible resource validation. The list is otherwise wildcard-incomplete and permission-filtered; the dedicated key must see every concrete definition.
- Stream one request and its response, including incremental SSE; preserve cancellation, safe headers, credentials and HTTP framing. Selected-backend errors, including model-not-found, ACL denials and transport failures, are terminal.
- Keep ingress size ceilings at 64 MiB Responses / 16 MiB other classified JSON. With true streaming, a chunked over-limit upload may have sent bytes to its selected backend before the limit is reached; it must stop without retry, not claim pre-dispatch whole-body validation.
- Inventory current retention removal and full-body validation exactly. Propose a bounded streaming top-level key filter or backend-owned compatibility handling, but do not silently decide either or claim unchanged validation semantics. Keep these as blocking user decisions.
- Explicitly revise the documentation's cache prohibition: allow only this bounded, background-refreshed membership snapshot. Keep `/routing-index`, catalog-derived membership and unbounded last-known-good decisions prohibited.
- Record the Sub2API passthrough verdict: HTTP `extra.openai_passthrough` is distinct from forbidden WS mode `passthrough`; it bypasses ordinary Responses account mapping/eligibility whitelist, leaves common rewrites and Chat conversion active, and does not guarantee bytes/order. Leave raw flag disabled and WS off pending a separate explicit operator decision; no Sub2API patch or account/mapping change is included.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cpa-first-catalog-routing`: ordered snapshot-based selection independent of legal model position, explicit uncertainty/age policy, dedicated discovery credentials, no cross-backend retry, wildcard validation and unchanged authorization/catalog.
- `apisix-cpa-gateway`: one bounded sliding-window HTTP pipe with quota-limited disk overflow, cleanup/request-response framing, 64 MiB resource gate and explicit compatibility decisions.

## Impact

- Runtime work after approval: `services/routing/apisix-models/` selector, config, template and tests; real APISIX 3.19 streaming harness at the existing `ops/scripts/validate.sh` seam. Add reviewed disk-backed spool mount/quota to Compose/operational lifecycle; no new dependency/service is selected. Separate keeper changes are delivered in its upstream repo and gateway gitlink/image pins, not duplicated here.
- Discovery provisioning: dedicated AISIX key, private caller-key schema and key-generation/verification paths, Compose/Ansible secret delivery and matching examples. No secret values enter Git. This is a coupled configuration/source activation, not merely selector-file deployment.
- Validation: extend `ops/scripts/check-aisix-resource-policy.py` and the Ansible AISIX deployment seam to reject wildcard definitions without changing the seven intentionally retained Axis entries or other routing/cooldown policy.
- Documentation: short component README; technical decisions, new snapshot exception, limits, byte-filter semantics and rollback in `docs/model-routing.md`; reconcile affected topology/spec text.
- Old replay prototype and its memory ablation remain rejected-alternative evidence, not acceptance of the new pipe. Task 0.1 must prove the same code at model offsets 1 and 4 MiB, a fixed 64 KiB window plus bounded overhead across 1/16/64 MiB, early forwarding, correct late CPA-only selection, byte fidelity, SSE, chunked uploads, disk quota/resource errors and file cleanup on success/error/cancel/restart. Uncertainty still goes to AISIX, but prefix overflow does not.
- No runtime implementation, production writes, new production probes or pipe prototype execution in this planning update. All implementation tasks remain unchecked.
