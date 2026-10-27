## Why

The Go models-enricher re-implements metadata selection with its own concepts: `source_priority`, `provider_prefix_map`, `lookup_ids`, `global_fallback`, per-channel regex filters and `exclude_models`. These have drifted from the chain semantics of openai-api-pi-extension. Two codebases now give different metadata for the same model and need double maintenance.

The extension change `add-chain-source-keyword-registry` exposes a host-neutral compile core with registered record sources and standard glob selection. The gateway can therefore reuse that core and rewrite the sidecar in TypeScript.

## What Changes

- Replace the Go sidecar with a TypeScript service that reuses openai-api-pi-extension as a git submodule through its host-neutral entry. The service runs on a Node LTS slim image as an esbuild-bundled server plus worker, with a baked compile cache. Each catalog generation compiles in a one-shot worker. Go code, Go CI and the Go image are deleted after cutover.
- **BREAKING** (operator config): the configuration moves to the extension schema: root `chain`, ordered `models[].match` rules with `overrides`, and `include_models`.
  - Removed fields: `source_priority`, `provider_prefix_map`, `lookup_ids`, `global_fallback`, `channels.*.include`, `channels.*.exclude`, `channels.*.overrides`, `channels.*.models`, `exclude_models`, `fetch_models` and `bare_models_takeover`.
  - Fields that stay gateway-only: `static_models` (with `inherit`), `metadata_from`, `custom_channels` pools, `channels.<prefix>.path`, `sync_max_context_window`, AISIX settings and refresh durations.
- Two record sources:
  - `@self`: the CPA native manifest row.
  - `@cpa-upstream`: the rich model row that CPA fetches from the channel's upstream through the management api-call. Ollama `/api/show` is folded into it.
  
  Channel upstream reads happen only when an effective chain of a kept model contains `@cpa-upstream`.
- **BREAKING**: models.dev and modelparams.dev data come from exactly pinned npm packages. They are converted at image build time instead of fetched over HTTP at runtime, so the sidecar needs no internet egress.
- **BREAKING**: `GET /routing-index` is removed. It has no consumer since APISIX moved to catalog-based routing.
- The main spec is aligned with current Go behavior: channel upstream lists only supply data and never add catalog membership. Go already intersects channel results with CPA-admitted members. Membership comes from CPA native manifest admission, the AISIX supplement and gateway static models. This is not a behavior change.
- **BREAKING**: unprefixed CPA models admitted by CPA identity are always listed; this was the former `bare_models_takeover` behavior. Operators hide them with `include_models` (for example `*/**`).
- The `gpt-image-*` output-modality special case becomes an ordinary override rule in configuration.
- `/models-table` lists models hidden by `include_models` with the deciding rule, without processing them. The compile set is the kept models plus the closure of models they reference through `inherit`/`metadata_from`; referenced-only models are shown as hidden and referenced.
- The persistent cache keeps only `@cpa-upstream` responses, with the same 100 MB FIFO budget in a new on-disk format. Management and authority reads stay in memory only.
- Dependabot covers `gitsubmodule`, `npm`, `docker`, `cargo`, `gomod` and `github-actions`. The data packages are grouped weekly.
- Cutover: the TS service runs in shadow next to Go, and catalog outputs are diffed. Production switches only after the diff is accepted.

## Capabilities

### New Capabilities

- `models-enricher-runtime`: Resource and isolation contract of the TS sidecar:
  - memory and CPU bounds against the measured Go baseline;
  - worker isolation and release of metadata tables;
  - build-time data provenance;
  - no runtime internet egress;
  - CLI compatibility of `healthcheck` and `validate-config`.

### Modified Capabilities

- `codex-models-enricher`:
  - chain/record-source metadata selection replaces source priority and the HTTP metadata fetch;
  - `include_models` replaces regex and exact-ID filters;
  - on-demand `@cpa-upstream` channel reads;
  - the outdated "undeclared channel models appear" scenario is replaced by the current rule that membership never grows from channel fetches;
  - bare model admission;
  - routing-index removal;
  - persistent cache contents limited to `@cpa-upstream` responses.
- `models-table-force-refresh`:
  - the force-refresh chain covers `@cpa-upstream` responses instead of runtime metadata HTTP sources;
  - the table shows hidden and referenced-only models.

## Impact

- **Code**:
  - New `models-enricher-ts/` (renamed to `models-enricher/` after the Go code is deleted).
  - New submodule `models-enricher-ts/vendor/openai-api-pi-extension`.
  - `models-enricher/*.go` deleted after cutover.
- **Deployment**:
  - `compose.yaml` gets a shadow service and then the switched `models-enricher` service.
  - The `models-enricher-cache` volume content format changes; the old bbolt file is ignored and then removed.
  - The egress relay dependency of the enricher can be dropped after cutover.
- **CI**: `.github/workflows/validate.yml` and `publish-models-enricher.yml` move from Go to Node, with submodule checkout. New `.github/dependabot.yml`.
- **Ops**: `ansible` keeps invoking `/models-enricher validate-config` and `/models-enricher healthcheck`. The private models-enricher YAML must be migrated to the new schema before cutover; this stays in gitignored private config.
- **Dependency**: requires openai-api-pi-extension with `add-chain-source-keyword-registry` applied.
- **Risk**: the default `@creator` resolution changes metadata values for some models, so cutover is gated on the shadow diff.
