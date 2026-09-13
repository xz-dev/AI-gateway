## Why

CPA's model-router plugin coupled logical aliases and fallback policy to the credential/pool execution layer, while the rejected New API and evaluated LiteLLM alternatives did not meet the desired routing, Responses, memory, and operational constraints. AISIX provides a smaller dedicated routing layer with ordered failover and per-target cooldown while preserving CPA as the single provider channel, but its API-oriented OSS management surface does not give the operator a safe direct view of live route exclusions.

## What Changes

- Deploy a pinned AISIX 1.2.0-based image with the upstream cooldown exclusion-log deadlock fix, between Sub2API and CPA: Sub2API -> AISIX -> CPA.
- Make AISIX the exclusive owner of client-visible bare-ID logical models, ordered fallback, bounded retry, and per-concrete-target cooldown. Keep Sub2API responsible for client authentication, entitlement, quota, WebSocket ingress, and `http_bridge`; keep CPA responsible for provider credentials and pools.
- Define one CPA provider key, 25 concrete CPA models, 12 logical aliases, and two scoped caller keys, for 40 AISIX resources total.
- Use direct internal `sub2api-aisix`, `aisix-cpa`, and `enricher-aisix` pairwise inference/catalog networks. Give AISIX no external/default route, keep its read-only Admin API on a dedicated unpublished internal management network, and expose only a server-rendered `/status` page through the existing loopback/Tailscale management address. Keep the status renderer outside the AISIX source tree so AISIX upgrades do not accumulate a long-lived UI patch.
- Supplement the rich catalog with the exact case-sensitive set difference `AISIX IDs - complete original CPA IDs`, computed before CPA-local filtering and then processed by existing enrichment and final Sub2API entitlement intersection.
- Bound models-enricher's completed-catalog memory with a five-minute cache, 24-entry and 16 MiB aggregate limits, and serialized large builds across client versions; deploy it with a 384 MiB cgroup limit and `GOMEMLIMIT=288MiB`.
- Migrate all CPA-backed Sub2API accounts to `http://aisix:3000/v1`, remove isolated candidate records and infrastructure after verification, and permanently delete the zero-use CPA model-router plugin and its executable/restorable artifacts.
- Preserve unrelated APISIX public entry, SSE, catalog, WebSocket compatibility, relay components, and native `/v1/responses` HTTP/SSE passthrough.

## Capabilities

### New Capabilities

- `aisix-model-routing`: AISIX-owned logical-model scheduling over one CPA provider channel, including ordered fallback, per-target 429 cooldown isolation, Retry-After handling, bounded retries, and Responses passthrough.
- `aisix-catalog-supplement`: Optional catalog supplementation from AISIX-exclusive IDs without changing CPA metadata precedence or Sub2API entitlement authority.
- `safe-aisix-cutover`: Additive candidate validation followed by in-place production integration, bounded verification, candidate cleanup, permanent retirement of CPA model routing, and a page-only private operator status surface over an unpublished AISIX Admin API.

### Modified Capabilities

None. Public ingress and default egress-policy capabilities retain their existing authority and boundaries.

## Impact

- Tracked deployment surface: `compose.yaml`, `.env.example`, `.gitignore`, `aisix/`, the isolated AISIX status renderer, `models-enricher/`, `scripts/init.sh`, `scripts/validate.sh`, and operational documentation.
- Private production surface: `/root/AI-gateway/aisix/`, Sub2API account configuration, retained recovery snapshots, and redacted migration provenance under `/root/AI-gateway/.migration-evidence/`.
- CPA model-router is not retained as a candidate, standby, or rollback mechanism. New API remains permanently rejected.
- Upstream-native WebSocket is not required. Explicit full-history WebSocket replay remains supported through Sub2API `http_bridge`; implicit `previous_response_id` continuation is a separate change.
