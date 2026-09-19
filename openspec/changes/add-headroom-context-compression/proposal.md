## Why

Long agent histories offer an opportunity to reduce upstream input work, but compression can also invalidate an already valuable provider cache or remove information needed later. Introduce Headroom as an explicitly selected, recoverable context-compression path without requiring every gateway client to support retrieval or changing the existing Responses streaming contract.

## What Changes

- Add an optional `Sub2API -> Headroom -> AISIX -> CPA` inference path. Ordinary clients retain the current direct Sub2API-to-AISIX path; no automatic client-capability detection or mandatory client plugin is introduced.
- Provide full Context Compression with Retrieval (CCR), including local CPU ML compression, for explicitly configured MCP-capable clients. Reuse Headroom's official MCP server and `headroom_retrieve` rather than implementing a plugin for each client. A tool declaration is not authorization to retrieve originals.
- Keep retrieval client-managed: disable gateway-owned CCR generation loops, stream buffering/reconstruction, extra inference retries, response/semantic caching, and unrelated routing or memory features. Sub2API keeps client authority and `http_bridge`; AISIX keeps routing/fallback/cooldown; CPA keeps credentials and its single upstream pool.
- Require authenticated, isolated original storage and retrieval, stable compressed prefixes, and a verified recovery lifecycle. An unqualified client or unproven identity boundary cannot enter the lossy CCR path.
- Compare unchanged traffic, format-only lossless compression, and ML plus client-managed CCR using synthetic fixtures first. Lossless compression is an evaluation baseline, not a silently enabled new default. Provider-cache hits, compression-cache hits, credential affinity, and net token cost remain separate measurements.
- Plan a bounded single-worker CPU deployment against the possible 4-core/4-GB host upgrade, with controlled image/model publication, an isolated candidate, and separate production approval. No hardware upgrade is presumed complete.

## Capabilities

### New Capabilities

- `headroom-context-compression`: Explicit opt-in routing, protocol-preserving compression, local ML resource gates, and safe bypass behavior.
- `headroom-ccr-retrieval`: Official MCP reuse, authenticated original retrieval, isolation, expiry, and client-owned recovery turns.
- `headroom-cache-continuity`: Stable prefix reuse, bounded session policy, cache-aware comparisons, and honest evidence boundaries.

### Modified Capabilities

- `safe-aisix-cutover`: Permit an approved optional Headroom frontend and its explicit private network edges while preserving the direct path, sole AISIX routing ownership, unpublished Admin boundary, and resource/approval gates.

## Impact

Implementation would affect Compose networking and service/image settings, a small Headroom integration area, selected Sub2API account/group configuration, official MCP configuration examples, image publication, and focused offline acceptance checks. It does not replace the model catalog path, alter logical-model priorities/weights, rewrite concrete CPA names, or expose Headroom storage or AISIX Admin publicly.

Research is pinned to Headroom v0.37.0 (`32d7ca4577d599b8a5f811ada74cf31504302c9d`), not an approved deployment pin. Its default automatic CCR is incompatible with the selected streaming contract. The first implementation gate must establish a maintainable client-managed configuration and trusted inference-to-retrieval identity binding; if that requires bespoke client plugins or a broad upstream fork, stop for a revised decision.

The completed cache investigation reports no demonstrated systemic concurrency-related cache degradation. This change therefore does not include a speculative cache-affinity or load-balancing repair. The design records its evidence and limitations. Windows-hosted remote Kompress, automatic CCR, arbitrary multi-tenant retrieval exposure, and additional compression modalities are outside this first integration.

This proposal authorizes no installation, provider inference, provisioning, production change, commit, or deployment. All implementation tasks remain pending until a subsequent apply request.
