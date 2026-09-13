## Purpose

A Go sidecar that answers Codex models-manifest requests (`GET /v1/models` with `client_version`) by aggregating every enabled non-OAuth CPA channel's live model list through CPA's authenticated api-call proxy, filling missing metadata from models.dev/modelparams.dev, and applying operator overrides and filters.

## ADDED Requirements

### Requirement: Codex manifest response

The sidecar SHALL answer `GET /v1/models` requests carrying a non-empty `client_version` query parameter with a ChatGPT Codex manifest. Requests without `client_version` never reach the sidecar (handled by the gateway route split).

#### Scenario: Codex client gets enriched manifest

- **WHEN** a client sends `GET /v1/models?client_version=1.0.0`
- **THEN** the response is a Codex manifest whose `models[]` entries each carry `slug`, `display_name`, `context_window`, `max_context_window`, `input_modalities`, `supported_reasoning_levels` (with `effort`), and `default_reasoning_level`, plus `max_tokens` when known

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
