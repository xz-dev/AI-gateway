## MODIFIED Requirements

### Requirement: Codex manifest response

The sidecar SHALL answer `GET /v1/models` requests carrying a non-empty `client_version` query parameter with a ChatGPT Codex manifest. Requests without `client_version` or with an empty value SHALL be answered with the standard OpenAI models projection: a `{"object": "list", "data": [...]}` body whose entries carry `id` (the exact model ID, bytes preserved) and `object: "model"`, built from the same catalog snapshot and admission rules using the canonical build version `1`. The raw CPA `NativeManifest` fetch stays pinned to `client_version=1`, and the models-table output keeps its current inventory version. Existing Codex manifest content semantics are unchanged.

#### Scenario: Codex client gets enriched manifest

- **WHEN** a client sends `GET /v1/models?client_version=1.0.0`
- **THEN** the response is a Codex manifest whose `models[]` entries each carry `slug`, `display_name`, `context_window`, `max_context_window`, `input_modalities`, `supported_reasoning_levels` (with `effort`), and `default_reasoning_level`, plus `max_tokens` when known

#### Scenario: Standard OpenAI list without client_version

- **WHEN** a client sends `GET /v1/models` with an absent or empty `client_version`
- **THEN** the response is a valid OpenAI list object whose `data[]` entries carry the exact model IDs under the existing admission rules, built at canonical version `1`
- **AND** the raw CPA inventory fetch used for that build still uses `client_version=1`, and models-table keeps its current inventory version

## ADDED Requirements

### Requirement: Routing snapshot retains distinct raw memberships

The sidecar SHALL retain, alongside the enriched manifest, the distinct raw memberships captured for routing: the complete CPA raw inventory (manifest inputs captured before CPA-local identity filtering) and the complete AISIX raw inventory. Raw memberships SHALL NOT be derived from the enriched output union. The sidecar SHALL expose a bounded internal routing-index view (`GET /routing-index?model=<exact ID>`) returning exactly one of `cpa`, `aisix`, `not_found`, or `unavailable` plus generation, where `not_found` requires both memberships authoritative and the ID absent from both. The routing-index view SHALL be internal-only: unauthenticated external callers cannot read it, and it never returns full catalog contents.

#### Scenario: Raw memberships are independent of the enriched union
- **WHEN** an AISIX-only or alias ID appears in the enriched manifest through supplementation
- **THEN** the CPA raw membership does not contain it
- **AND** the routing index treats it as AISIX-only

#### Scenario: Routing index stays bounded
- **WHEN** the entrance performs a per-request classification lookup
- **THEN** the index view answers one of the four outcomes for the requested ID without returning full catalog contents

#### Scenario: Authoritative absence versus unavailable source
- **WHEN** the index is queried for an ID absent from both memberships under an authoritative generation
- **THEN** it returns `not_found`
- **WHEN** the same ID is queried while a membership required to decide it is failed
- **THEN** it returns `unavailable`, not `not_found`

### Requirement: Snapshot lifecycle is initialized and refreshed independently of catalog traffic

The sidecar SHALL initialize the routing snapshot at startup and refresh it on a bounded periodic cadence aligned with the existing cache TTL, reusing the existing collector and singleflight discipline, independent of any catalog-client or lookup traffic. Failed refreshes SHALL retry on the same bounded cadence without tight-looping and without a new persistent store. Collection SHALL keep the CPA mandatory phase and optional AISIX phase isolated, with the existing eligible last-good policy, so a failed optional AISIX collection preserves successful CPA output and known CPA routing decisions. Publication SHALL be an immutable raw-membership/source-availability generation: known CPA decisions are preserved when only AISIX collection fails, and no hot-path lookup triggers upstream collection. Catalog projection caches SHALL key each cached body to the raw generation plus client_version/format: a completed projection of a stale generation cannot be served as current, and a late old-generation build cannot overwrite the current entry; in-flight readers may finish serving their pinned generation, with no global simultaneous multi-request guarantee. Cold readiness is truthful: before the first successful snapshot the index returns `unavailable`, and recovery occurs via the bounded retry cadence even when only inference traffic exists.

#### Scenario: Startup initialization without catalog traffic
- **WHEN** the sidecar starts and no client requests any catalog
- **THEN** the routing snapshot is initialized and refreshed on the bounded cadence
- **AND** inference classification does not depend on a catalog request having occurred

#### Scenario: Optional AISIX failure preserves CPA decisions
- **WHEN** an AISIX collection attempt fails while CPA collection succeeds
- **THEN** the published generation preserves known CPA membership decisions
- **AND** IDs decidable only with AISIX membership return `unavailable`

#### Scenario: Generation-pinned projection caching
- **WHEN** a new raw generation is published while a catalog projection build is in flight
- **THEN** the in-flight build may complete for readers pinned to its generation
- **AND** its result does not overwrite the current entry, and stale completed projections are not served as current

#### Scenario: Inference-only cold start and recovery
- **WHEN** the sidecar restarts and only inference (routing-index) traffic arrives
- **THEN** lookups return `unavailable` until the first successful bounded-cadence collection completes, then answer per membership
- **AND** refresh retries observe the bounded cadence without a tight loop

#### Scenario: Hot-path lookup never collects
- **WHEN** a routing-index lookup arrives between refreshes
- **THEN** it is answered from the published generation without triggering upstream collection
