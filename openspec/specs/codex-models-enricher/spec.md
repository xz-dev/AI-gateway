# Codex Models Enricher Specification

## Purpose

A Go sidecar that answers Codex models-manifest requests (`GET /v1/models` with `client_version`) by aggregating every enabled non-OAuth CPA channel's live model list through CPA's authenticated api-call proxy, filling missing metadata from models.dev/modelparams.dev, and applying operator overrides and filters.

## Requirements

### Requirement: Codex manifest response

The sidecar SHALL answer `GET /v1/models` requests carrying a non-empty `client_version` query parameter with a ChatGPT Codex manifest. Requests without `client_version` or with an empty value SHALL be answered with the standard OpenAI models projection: a `{"object": "list", "data": [...]}` body whose entries carry `id` (the exact model ID, bytes preserved) and `object: "model"`, built from the same catalog snapshot and admission rules using the canonical build version `1`. The raw CPA `NativeManifest` fetch stays pinned to `client_version=1`, and the models-table output keeps its current inventory version. Existing Codex manifest content semantics are unchanged.

#### Scenario: Codex client gets enriched manifest

- **WHEN** a client sends `GET /v1/models?client_version=1.0.0`
- **THEN** the response is a Codex manifest whose `models[]` entries each carry `slug`, `display_name`, `context_window`, `max_context_window`, `input_modalities`, `supported_reasoning_levels` (with `effort`), and `default_reasoning_level`, plus `max_tokens` when known

#### Scenario: Standard OpenAI list without client_version

- **WHEN** a client sends `GET /v1/models` with an absent or empty `client_version`
- **THEN** the response is a valid OpenAI list object whose `data[]` entries carry the exact model IDs under the existing admission rules, built at canonical version `1`
- **AND** the raw CPA inventory fetch used for that build still uses `client_version=1`, and models-table keeps its current inventory version

### Requirement: Channel discovery

The sidecar SHALL discover all enabled non-OAuth key-type channels from the CPA management API (`openai-compatibility`, `claude-api-key`, `gemini-api-key`, `codex-api-key`, `xai-api-key`, `vertex-api-key`, `interactions-api-key`), excluding disabled channels and OAuth auth-file credentials.

#### Scenario: Disabled channel excluded

- **WHEN** a CPA channel entry has `disabled: true`
- **THEN** its models are not fetched and do not appear in the manifest

#### Scenario: OAuth credentials excluded

- **WHEN** CPA serves models from OAuth auth-files
- **THEN** the sidecar does not re-fetch them; CPA's native manifest content for those models is preserved as-is

### Requirement: Per-channel models fetch via CPA api-call

For each discovered channel, the sidecar SHALL fetch the live model list through CPA's `POST /v0/management/api-call` using the channel credential (`$TOKEN$` substitution), with a per-channel configurable request path (default `/v1/models?client_version=<incoming client_version value>`), and SHALL tolerate individual channel failures without failing the whole response.

#### Scenario: Channel path override

- **WHEN** the sidecar YAML sets a channel's models path to `/v1/models`
- **THEN** the api-call for that channel uses exactly that path

#### Scenario: Partial failure tolerated

- **WHEN** one channel's api-call fails or times out
- **THEN** the manifest still returns, containing the remaining channels' models, and the failure is logged with the channel identity

### Requirement: Metadata fallback with cached data sources

When a model's metadata fields are missing from the channel response, the sidecar SHALL look them up from models.dev (`/api.json`, `/models.json`, `/catalog.json`) and/or modelparams.dev, caching those data-source responses for 10 minutes. Channel model-list responses are not cached beyond the single request.

#### Scenario: Missing max_tokens filled

- **WHEN** a channel model lacks `max_tokens` and a data source knows the model's max output tokens
- **THEN** the emitted entry carries that value

#### Scenario: Source cache honored

- **WHEN** two enrichment runs happen within 10 minutes
- **THEN** models.dev/modelparams.dev are fetched at most once in that window

### Requirement: Overrides and model filtering

The sidecar SHALL apply per-channel, per-model field overrides from its YAML configuration (overrides win over both upstream and data-source values), and SHALL apply per-channel include/exclude regex lists to filter which models appear.

#### Scenario: Explicit override wins

- **WHEN** YAML declares channel A's model B has `max_tokens: 1000000`
- **THEN** the emitted entry for B reports 1000000 regardless of upstream or data-source values

#### Scenario: Exclude regex

- **WHEN** a channel has an exclude regex matching `^gemini-.*-image$`
- **THEN** matching models are omitted from that channel's contribution

### Requirement: Merge semantics with CPA native manifest

The sidecar SHALL merge discovered channel models with CPA's native Codex manifest: CPA-native fields are preserved, missing fields are filled from channel fetch and data sources, and only explicit YAML overrides may replace existing values. Discovered models SHALL be emitted with CPA's provider-prefix slug convention (`<prefix>/<model>`).

#### Scenario: Fill gaps only by default

- **WHEN** CPA's native manifest already provides `context_window` for a model
- **THEN** the sidecar keeps CPA's value unless a YAML override exists

#### Scenario: Undeclared channel models appear

- **WHEN** an enabled channel has no models array in CPA config but its live models endpoint returns models
- **THEN** those models appear in the manifest with the channel's prefix

### Requirement: Unprefixed model exclusion

CPA's model routing is abandoned: CPA is treated purely as a provider of prefix-qualified models. The emitted manifest SHALL exclude every entry whose slug has no channel prefix (e.g. bare `gpt-5.6-sol`), keeping only prefix-qualified slugs (e.g. `codex/gpt-5.6-sol`). This is a models-list filtering rule only; the sidecar and gateway SHALL NOT intercept or block inference requests for unprefixed model IDs.

#### Scenario: Bare slug dropped

- **WHEN** CPA's native manifest (or a channel fetch) yields both `gpt-5.6-sol` and `codex/gpt-5.6-sol`
- **THEN** the emitted manifest contains only `codex/gpt-5.6-sol`

#### Scenario: No request interception

- **WHEN** a client sends an inference request naming a bare model id
- **THEN** it passes through untouched; the exclusion applies to the models manifest only

### Requirement: Secret custody

The sidecar SHALL obtain the CPA management key only from its configured environment/file mount, and SHALL never expose the key or channel credentials in logs, responses, or error messages.

#### Scenario: No secret leakage

- **WHEN** any channel fetch fails or the sidecar errors
- **THEN** logs and responses contain channel names and status codes only, never credential material

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
