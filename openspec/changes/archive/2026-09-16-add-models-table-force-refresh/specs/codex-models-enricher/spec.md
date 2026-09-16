## MODIFIED Requirements

### Requirement: Snapshot lifecycle is initialized and refreshed independently of catalog traffic
The sidecar SHALL initialize the routing snapshot at startup and refresh it on a bounded periodic cadence aligned with the existing cache TTL, reusing the existing collector and singleflight discipline, independent of any catalog-client or lookup traffic. Failed periodic refreshes SHALL retry on the same bounded cadence without tight-looping and without a new persistent store. Collection SHALL keep the CPA mandatory phase and optional AISIX phase isolated, with the existing eligible last-good policy, so a failed optional AISIX collection preserves successful CPA output and known CPA routing decisions. Publication SHALL be an immutable raw-membership/source-availability generation: known CPA decisions are preserved when only AISIX collection fails, and no hot-path lookup triggers upstream collection. Catalog projection caches SHALL key each cached body to the raw generation plus client_version/format: a completed projection of a stale generation cannot be served as current, and a late old-generation build cannot overwrite the current entry; in-flight readers may finish serving their pinned generation, with no global simultaneous multi-request guarantee. Cold readiness is truthful: before the first successful snapshot the index returns `unavailable`, and recovery occurs via the bounded retry cadence even when only inference traffic exists.

The diagnostic force-refresh action SHALL be the sole exception to request-independent collection: it SHALL explicitly initiate a bounded, singleflight refresh of raw memberships and catalog inputs, bypass eligible enricher read caches for that attempt, and publish a new generation only according to the same source-authority and atomic-publication rules. Normal `GET /models-table`, catalog requests, and routing-index lookups SHALL retain their existing non-forcing behavior.

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

#### Scenario: Normal catalog and table reads do not force collection
- **WHEN** a client requests `GET /v1/models` or `GET /models-table`
- **THEN** the request uses the current generation and eligible caches without bypassing the periodic refresh cadence

#### Scenario: Diagnostic force refresh uses existing publication rules
- **WHEN** an operator submits the diagnostic force-refresh action
- **THEN** the sidecar performs one bounded refresh attempt that bypasses eligible enricher read caches
- **AND** concurrent force-refresh submissions share the active attempt
- **AND** any newly published generation follows the existing atomic source-authority rules
