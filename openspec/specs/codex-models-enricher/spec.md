# Codex Models Enricher Specification

## Purpose

The `model-catalog` service (formerly `models-enricher`; this capability ID is kept as a history key), a TypeScript service that answers Codex models-manifest requests (`GET /v1/models` with `client_version`) from CPA-admitted catalog members, selecting each model's metadata through the shared openai-api-pi-extension source chain (bundled models.dev/modelparams.dev snapshots, the CPA native row `@self` and the channel upstream row `@cpa-upstream` read through CPA's authenticated api-call proxy), and applying operator overrides, `include_models` filtering and gateway-only post-processing.

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

The sidecar SHALL read a channel's upstream model rows only to serve the `@cpa-upstream` record source. It SHALL perform that read only when at least one model of the current compile set from that channel has an effective chain containing `@cpa-upstream`. The read SHALL go through CPA's `POST /v0/management/api-call` with the channel credential (`$TOKEN$` substitution). Each channel MAY configure its request path; the default is `/v1/models?client_version=<incoming client_version value>`.

For channels configured with an Ollama native base, the `@cpa-upstream` row of a model SHALL come from Ollama `/api/show` through the same api-call. Requests are deduplicated per channel and lookup ID.

Individual channel or model read failures SHALL NOT fail the whole response. A failed read only removes that record source's contribution, subject to the cached-response fallback.

#### Scenario: Channel path override

- **WHEN** the sidecar YAML sets a channel's models path to `/v1/models` and a kept model of that channel has `@cpa-upstream` in its effective chain
- **THEN** the api-call for that channel uses exactly that path

#### Scenario: Partial failure tolerated

- **WHEN** one channel's api-call fails or times out
- **THEN** the catalog still returns, containing every admitted model, and the failure is logged with the channel identity
- **AND** models of the failed channel receive their remaining chain sources' fields

#### Scenario: No channel read without @cpa-upstream

- **WHEN** no kept or referenced model of a channel has `@cpa-upstream` in its effective chain
- **THEN** the sidecar sends no api-call for that channel's upstream models

#### Scenario: Ollama show folded into @cpa-upstream

- **WHEN** an Ollama channel's model chain contains `@cpa-upstream`
- **THEN** that model's `@cpa-upstream` row is built from `/api/show` read through the CPA api-call, and two models sharing one lookup ID cause one read

### Requirement: Secret custody

The sidecar SHALL obtain the CPA management key only from its configured environment/file mount, and SHALL never expose the key or channel credentials in logs, responses, or error messages.

#### Scenario: No secret leakage

- **WHEN** any channel fetch fails or the sidecar errors
- **THEN** logs and responses contain channel names and status codes only, never credential material

### Requirement: Enrichment inputs survive sidecar restart on a dedicated volume

The sidecar SHALL retain its validated successful `@cpa-upstream` responses on a dedicated Docker data volume mounted as its cache path. Normal process restart and container recreation with the same volume SHALL preserve cached payloads, last-successful-refresh times, capacity accounting, and FIFO order. Cache age alone SHALL NOT remove persisted inputs, and startup SHALL NOT pretend recovered stale data was newly fetched.

Persistent cache contents SHALL consist of `@cpa-upstream` responses and their cache metadata only. They SHALL NOT include:
- assembled model catalogs or rendered tables;
- authority reads: native inventory, management discovery and identity, AISIX membership;
- authorization snapshots.

Authority reads SHALL remain process-local.

Cache request identities SHALL isolate method, destination, relevant authentication and headers, and any inner read request body. Stored keys, logs, and diagnostics SHALL NOT expose credentials. Cache files from an earlier, incompatible storage format SHALL be ignored without failing startup.

#### Scenario: Restart during an external metadata outage

- **GIVEN** the sidecar has successfully cached `@cpa-upstream` responses and its dedicated data volume is retained
- **WHEN** the sidecar restarts or is recreated and the channel upstream remains unavailable
- **THEN** a catalog build with usable current CPA authority reuses the recovered response for `@cpa-upstream` fields
- **AND** recovery preserves the input's original freshness age rather than resetting it at startup

#### Scenario: Separate requests cannot borrow another request's cache

- **WHEN** otherwise similar `@cpa-upstream` requests differ in authenticated identity, target, method, relevant headers, or inner request body
- **THEN** their successful responses are isolated
- **AND** one request's failure cannot return another request identity's cached response

#### Scenario: Old cache format is ignored

- **WHEN** the volume still holds a cache file written by the previous implementation
- **THEN** the sidecar starts with an empty cache in its own format and reports no cache-recovery error as fatal

### Requirement: Persistent enrichment cache has a 100 MB FIFO payload budget

The sidecar SHALL limit the sum of retained enrichment-response payload bytes to **100,000,000 bytes (100 MB)**. Before admitting a response that would exceed this limit, it SHALL evict complete cached entries in first-in-first-out admission order until the new retained payload fits; when replacing an existing key, eviction SHALL consider other entries after subtracting that key's previous payload size. Reads and successful replacement of an existing entry SHALL NOT make that entry newer in FIFO order; a removed entry admitted again SHALL receive a new position. The budget and admission order SHALL survive restart. Freshness expiry SHALL NOT cause eviction, and the sidecar SHALL NOT impose an additional entry-count limit. Database indexes, reusable free pages, and temporary maintenance files SHALL be outside this effective-payload limit. A successful response too large to fit by itself SHALL NOT flush the cache or replace an existing retained response; the existing per-endpoint response limits SHALL remain enforced.

#### Scenario: Admission at the exact capacity boundary

- **GIVEN** retained response payloads plus a new valid response total exactly 100,000,000 bytes
- **WHEN** the new response is admitted
- **THEN** the sidecar retains it without capacity eviction

#### Scenario: FIFO differs from LRU

- **GIVEN** entries A and B were admitted in that order, and A was later read or successfully refreshed
- **WHEN** admitting C requires one older entry to be evicted
- **THEN** A is evicted before B
- **AND** retained payload bytes do not exceed 100,000,000

#### Scenario: FIFO survives restart

- **GIVEN** A was admitted before B and both remain persisted
- **WHEN** the sidecar restarts and admitting C requires eviction
- **THEN** eviction preserves A-before-B order rather than ordering entries by startup, last access, or last refresh

#### Scenario: Oversized response does not destroy retained successes

- **WHEN** a successful response cannot fit within the entire effective-payload budget by itself
- **THEN** it is not retained and the sidecar does not evict other entries merely to attempt its admission
- **AND** any previous retained successful response for the same request remains intact

### Requirement: Cached metadata does not freeze catalog membership or grant authority

For every newly built catalog, the sidecar SHALL compose output from the pinned current authority generation and available enrichment inputs rather than reuse an old final catalog to conceal an enrichment failure. Cached enrichment data SHALL only contribute metadata or model candidates that pass the existing current admission rules. Current CPA additions SHALL remain visible when eligible; current removals, disabled channels, authorization denials, and absent routing eligibility SHALL NOT be reversed by old enrichment inputs. Native CPA membership, management discovery and identity reads, AISIX routing membership, and per-user authorization SHALL retain their existing authority-specific failure policy, including denial and invalid-response handling. The relaxed metadata fallback policy SHALL NOT be applied to those authority decisions. A valid input without metadata for a new model SHALL remain an ordinary metadata miss, not grounds to replay a prior catalog.

#### Scenario: New CPA model appears while old metadata is reused

- **GIVEN** CPA's current eligible membership contains existing model A and newly added model B, and the retained source response describes only A
- **WHEN** that source refresh fails and a catalog is rebuilt
- **THEN** A retains its matching cached metadata and B appears under current admission rules
- **AND** fields unknown for B remain unknown or at their CPA baseline rather than suppressing B

#### Scenario: Removed model is not resurrected

- **GIVEN** a persisted channel-enrichment response mentions model A but current admission no longer permits A
- **WHEN** a source failure causes that response to be reused for enrichment
- **THEN** the output does not regain A from cached metadata

#### Scenario: Authority denial stays strict despite metadata cache

- **GIVEN** retained metadata exists for a model
- **WHEN** an authority read returns an authentication denial or invalid authority response, or a user is not entitled to the model
- **THEN** the existing authority failure or entitlement rejection behavior remains in force
- **AND** cached metadata does not grant catalog visibility or routing readiness

### Requirement: Chain-based metadata selection

The shared keys `chain`, `models` and `include_models` SHALL live under the gateway YAML's `metadata` block. Scenarios in this capability write them without that prefix. Configuration errors SHALL report paths with the prefix, for example `metadata.models[0].chain`.

The sidecar SHALL select model metadata with the shared chain semantics of openai-api-pi-extension:
- `metadata.chain`;
- ordered `models[].match` rules with standard glob matching;
- `@default`, `@creator` and literal fourth segments;
- first-valid-value-per-field selection in chain order.

The sidecar SHALL register exactly two record-source keywords:
- `@self`: the model's CPA native manifest row.
- `@cpa-upstream`: the model's rich row from its channel upstream.

External `models.dev/...` and `modelparams.dev/...` entries SHALL read the data versions bundled into the running image, without network requests. An absent `metadata.chain` SHALL use the shared default chain. Any other `@`-prefixed entry, a wildcard entry, or an empty chain SHALL be a configuration error.

Rule `overrides` SHALL apply as RFC 7396 merge patches, in rule order, to the emitted catalog entry using catalog field names. Overrides SHALL NOT change `slug` or `id`. Gateway-only post-processing SHALL run after chain selection and overrides.

#### Scenario: CPA value kept when @self is first

- **WHEN** the chain is `["@self", "models.dev/@creator/api_key"]` and CPA's native row has `context_window` 200000 while models.dev has 400000
- **THEN** the emitted entry reports 200000

#### Scenario: Explicit override wins

- **WHEN** a rule `match: "codexA/modelB"` declares `overrides: {max_tokens: 1000000}`
- **THEN** the emitted entry for `codexA/modelB` reports 1000000 regardless of chain values

#### Scenario: Missing max_tokens filled

- **WHEN** CPA's native row lacks `max_tokens` and a later chain entry knows the model's max output tokens
- **THEN** the emitted entry carries that value

#### Scenario: Unknown record keyword rejected

- **WHEN** the configuration contains the chain entry `@ollama_cloud`
- **THEN** configuration validation fails, names the entry path and lists `@self` and `@cpa-upstream`

#### Scenario: Image output by rule

- **WHEN** a rule `match: "**/gpt-image-*"` declares `overrides: {output_modalities: ["image"]}`
- **THEN** matching entries report image output, and no built-in special case applies to other models

### Requirement: Cached @cpa-upstream responses keep last-good fallback

Validated successful `@cpa-upstream` responses SHALL be cached as API responses, not as final catalogs:
- Ordinary reads SHALL reuse fresh responses for five minutes.
- Expiry SHALL make a response eligible for refresh without deleting it and without a maximum fallback age.
- After a failed refresh, the sidecar SHALL reuse that request's last successful response when present, and SHALL NOT replace or invalidate it. Failures include timeouts, connection failures, non-success HTTP statuses including 401/403, and invalid bodies.
- A successful valid empty response SHALL replace an older populated one.

A client disconnecting SHALL stop only that client's wait. The shared build SHALL continue within its deadline and store successful responses. Cancellation of an individual read SHALL return cancellation rather than success and SHALL keep the stored response. Without a usable cached response, the `@cpa-upstream` node contributes no fields. None of these rules SHALL relax membership, authorization or authority policy.

#### Scenario: Source cache honored

- **WHEN** two ordinary builds request the same `@cpa-upstream` input within five minutes
- **THEN** that input is fetched at most once in the freshness window

#### Scenario: Expired input survives a source outage

- **GIVEN** a successful `@cpa-upstream` response is older than five minutes
- **WHEN** its refresh times out, loses its connection or returns a non-success status
- **THEN** the build reuses the last successful response without an age cutoff
- **AND** the failed attempt does not advance its last-successful-refresh time

#### Scenario: Rejection or malformed response retains last-good data

- **WHEN** a refresh returns 401, 403, malformed JSON or a body failing validation
- **THEN** the previous validated response is kept and reused

#### Scenario: Successful empty input replaces old data

- **WHEN** a previously populated input successfully returns a valid empty response
- **THEN** the empty response replaces the old one and old rows are not resurrected

#### Scenario: Client disconnect does not abort the shared build

- **WHEN** a client disconnects while a shared build reads `@cpa-upstream`
- **THEN** the build continues within its deadline and stores successful responses for later requests

### Requirement: Model selection with include_models

The sidecar SHALL apply the shared `include_models` selection to member IDs, using exact-byte, case-sensitive glob matching:
- The last matching entry decides.
- A `!` prefix hides.
- Unmatched models are hidden.
- Absence of the field keeps all models.
- An empty list is a configuration error.

Both the Codex manifest and the standard list SHALL contain only kept models.

The compile set SHALL be the kept models plus every model they transitively reference through `static_models[].inherit` or the gateway `metadata_from` map. Hidden models outside the compile set SHALL NOT be compiled and SHALL NOT trigger `@cpa-upstream` reads. Selection SHALL NOT intercept or block inference requests for hidden IDs.

#### Scenario: Hide unprefixed models

- **WHEN** `include_models: ["*/**"]`, AISIX lists `gpt-5.6-sol` and CPA admits `codex/gpt-5.6-sol`
- **THEN** the catalogs list only `codex/gpt-5.6-sol`

#### Scenario: Referenced hidden model still provides metadata

- **WHEN** kept model `pool/x` has `metadata_from: {"pool/x": "vendor/y"}` and `vendor/y` is hidden
- **THEN** `vendor/y` is compiled for `pool/x`'s metadata but is absent from the catalogs

#### Scenario: Hidden model is not processed

- **WHEN** a hidden, unreferenced model's chain contains `@cpa-upstream`
- **THEN** no api-call is made on its behalf

#### Scenario: No request interception

- **WHEN** a client sends an inference request naming a hidden model ID
- **THEN** it passes through untouched; selection applies to model lists only

### Requirement: Catalog membership comes from authority sources only

Membership candidates SHALL be the union of these sources, deduplicated by exact, case-sensitive ID:
- every CPA native-manifest ID;
- every ID listed by the AISIX models API;
- configured gateway static models.

The profile's membership script (`catalog.lua`) SHALL decide, for every candidate, whether it is a member and from which of its sources. Membership SHALL be decided before `include_models` selection, chain metadata and post-processing. The script SHALL NOT be able to add IDs that are not candidates.

The image's default script SHALL keep this membership:
- a CPA ID that starts with a CPA prefix followed by `/` is a CPA member;
- a CPA ID listed in the script's extra CPA list is a CPA member;
- any other candidate listed by AISIX is an AISIX member;
- any other candidate that is a static model is a static member;
- every other candidate is rejected.

So, with the default script, a CPA ID without `/` and a slash ID whose first segment is not a CPA prefix, such as an upstream original `deepseek/deepseek-v4.1-flash`, SHALL NOT appear unless AISIX or a static model lists it or the extra CPA list matches it.

When a member's source is CPA, its CPA native row SHALL be its base record, even if AISIX or a static model also lists it. A CPA row SHALL NOT be used as the `@self` record of a member whose source is not CPA.

When the script keeps a candidate without choosing a source, the source SHALL be the first of CPA, AISIX and static that lists it. Member output order SHALL follow "Catalog output order and Codex priority" and SHALL NOT depend on source order or on the order in which the script is called.

`@cpa-upstream` rows and bundled metadata SHALL only contribute fields. A model present only in a channel's upstream list SHALL NOT be a candidate.

#### Scenario: Channel-only model does not appear

- **WHEN** a channel's upstream list contains `m-new` but CPA's native manifest does not
- **THEN** no catalog lists `<prefix>/m-new`, and the script is not called for it

#### Scenario: Prefixed CPA ID is a member

- **WHEN** the default script runs, CPA lists `codex/gpt-6.1-sol` and `codex` is a CPA prefix
- **THEN** the member set contains `codex/gpt-6.1-sol` with the CPA native row as its base record

#### Scenario: Bare CPA ID is not a member

- **WHEN** the default script runs, CPA lists `gpt-6.1-sol`, and AISIX and the static models do not
- **THEN** no catalog lists `gpt-6.1-sol`

#### Scenario: Slash original without a CPA prefix is not a member

- **WHEN** the default script runs, CPA lists `deepseek/deepseek-v4.1-flash`, `deepseek` is not a CPA prefix, and the extra CPA list does not match it
- **THEN** no catalog lists `deepseek/deepseek-v4.1-flash`

#### Scenario: Exception list admits unprefixed CPA IDs

- **WHEN** the script's extra CPA list is `{"devin/*"}` and CPA lists `devin/claude-opus-5-5` from an account without a prefix
- **THEN** the member set contains `devin/claude-opus-5-5` as a CPA member

#### Scenario: Admitted unprefixed model listed

- **WHEN** the script's extra CPA list is `{"my-alias"}`, CPA lists the operator-configured unprefixed model `my-alias`, and `include_models` is absent
- **THEN** the catalogs list `my-alias` with chain-selected metadata

#### Scenario: Every AISIX ID is a member

- **WHEN** the default script runs, AISIX lists the bare ID `gpt-6.1-sol` and CPA also lists a bare `gpt-6.1-sol`
- **THEN** the member set contains `gpt-6.1-sol` once, as an AISIX member without a CPA `@self` record

#### Scenario: ID listed by both CPA and AISIX

- **WHEN** the default script runs, CPA admits `codex/gpt-5.3-codex-spark` and AISIX also lists it
- **THEN** it appears once, with the CPA native row as its base record

#### Scenario: Edited script admits every candidate

- **WHEN** `catalog.lua` is `function select(m) return true end`
- **THEN** every CPA, AISIX and static candidate is a member, and a candidate listed by CPA uses its CPA row

#### Scenario: Edited script prefers AISIX on shared IDs

- **WHEN** the script returns `true, {source = "aisix"}` for every candidate listed by AISIX, and CPA and AISIX both list `codex/gpt-5.3-codex-spark`
- **THEN** `codex/gpt-5.3-codex-spark` is an AISIX member without a CPA `@self` record, at its position in the catalog output order

### Requirement: Gateway-only catalog post-processing is preserved

After chain selection and overrides, the sidecar SHALL apply the following gateway-only post-processing:
- static models with an ordered `inherit` list;
- the root `metadata_from` map, from a full output slug to the full output slug whose metadata it copies;
- `custom_channels` pools;
- `sync_max_context_window`, selected per model (see below);
- removal of a `default_reasoning_level` that is not among the model's supported reasoning levels, unless an override sets it explicitly.

These features SHALL be configured only in the gateway YAML and SHALL NOT change shared chain semantics.

`sync_max_context_window` SHALL accept either a boolean or an ordered glob list:
- `true` selects every non-native entry.
- `false`, or an absent field, selects none.
- A list uses the `include_models` matching rules: `!` excludes, the last matching entry decides, and unmatched models are not selected. An empty list is a configuration error.

For a selected model, `max_context_window` SHALL equal `context_window`. For an unselected model, `max_context_window` SHALL only be raised to `context_window` when it is missing or smaller. In both cases an override that sets `max_context_window` explicitly SHALL win. Rows emitted unchanged as CPA-native SHALL NOT be touched.

#### Scenario: Static model inherits compiled metadata

- **WHEN** static model `alias/a` declares `inherit: ["codex/gpt-5.6-sol"]` with an override of `display_name`
- **THEN** `alias/a` carries `codex/gpt-5.6-sol`'s compiled fields plus the override

#### Scenario: Conflicting reasoning default removed

- **WHEN** a model's selected `default_reasoning_level` is `xhigh`, its supported levels are `low` and `high`, and no override sets the default
- **THEN** the emitted entry omits the conflicting default

#### Scenario: Context window synchronized

- **WHEN** `sync_max_context_window` is enabled and a model has `context_window` 200000 and `max_context_window` 1000000
- **THEN** the emitted `max_context_window` is 200000

#### Scenario: Context window synchronized per model

- **WHEN** `sync_max_context_window: ["xl/**", "!xl/keep-*"]`, and models `xl/a`, `xl/keep-b` and `aihub/c` each have `context_window` 200000 and `max_context_window` 872000
- **THEN** `xl/a` reports 200000, while `xl/keep-b` and `aihub/c` keep 872000

#### Scenario: Synchronization off leaves larger upper bounds

- **WHEN** `sync_max_context_window` is absent and a model has `context_window` 500000 and `max_context_window` 872000
- **THEN** the emitted `max_context_window` is 872000
- **AND** a model whose `max_context_window` is missing or 128000 is raised to 500000

### Requirement: Legacy configuration fields are rejected

The following legacy fields SHALL be configuration errors that name the field path and a migration hint:
- `source_priority`, `provider_prefix_map`, `lookup_ids`, `global_fallback`, `exclude_models`, `fetch_models` and `bare_models_takeover`;
- the root `membership` block, including `membership.cpa_include`;
- channel-level `include`, `exclude`, `overrides` and `models`.

The hint for channel `models.<name>.metadata_from` SHALL point to the root `metadata_from` map. The hints for channel overrides and model overrides SHALL point to `models[].match` rules. The hints for `membership` and `bare_models_takeover` SHALL point to the extra CPA list in `catalog.lua`.

#### Scenario: Old private config detected before cutover

- **WHEN** `validate-config` runs on a YAML containing `source_priority`
- **THEN** it exits non-zero, names `source_priority` and suggests `chain`

#### Scenario: Startup with legacy config fails closed

- **WHEN** the sidecar starts with a YAML containing `exclude_models`
- **THEN** it exits non-zero before listening and does not serve a catalog built from partially understood config

#### Scenario: Exception list moved to the script

- **WHEN** `validate-config` runs on a YAML containing `membership.cpa_include: ["devin/*"]`
- **THEN** it exits non-zero, names `membership` and points to the extra CPA list in `catalog.lua`

### Requirement: Authority snapshot lifecycle and generation-pinned projections

The sidecar SHALL collect the CPA authority snapshot at startup and refresh it on a bounded periodic cadence, independent of catalog traffic. The CPA authority snapshot covers:
- the native inventory;
- management discovery of CPA prefixes and of the CPA prefix mode;
- the optional AISIX membership.

Collection SHALL keep the mandatory CPA phase and optional AISIX phase isolated. A failed AISIX collection SHALL preserve successful CPA output. Each publication SHALL be an immutable generation.

Catalog projection caches SHALL key each body to generation plus client_version and format:
- A stale generation's projection SHALL NOT be served as current.
- A late old-generation build SHALL NOT overwrite the current entry.
- Concurrent builds of one generation SHALL be coalesced.

`/readyz` SHALL report not ready until the first successful CPA authority generation is published. Failed refreshes SHALL retry on the bounded cadence without tight-looping.

The diagnostic force refresh SHALL be the only traffic-triggered collection. It SHALL bypass freshness reuse for one bounded, coalesced attempt, SHALL publish only under the same authority rules, and SHALL NOT disable last-good fallback for `@cpa-upstream` responses.

#### Scenario: Startup initialization without catalog traffic

- **WHEN** the sidecar starts and no client requests a catalog
- **THEN** the authority snapshot is collected and refreshed on the bounded cadence

#### Scenario: Optional AISIX failure preserves CPA output

- **WHEN** AISIX collection fails while CPA collection succeeds
- **THEN** the published generation keeps CPA membership and omits only the AISIX members

#### Scenario: Generation-pinned projection caching

- **WHEN** a new generation is published while a projection build for the previous one is in flight
- **THEN** the in-flight result is not stored as the current entry

#### Scenario: Readiness before first authority

- **WHEN** the sidecar has not yet published a CPA authority generation
- **THEN** `/readyz` reports not ready, and becomes ready after the first successful collection

#### Scenario: Normal catalog and table reads do not force collection

- **WHEN** a client requests `GET /v1/models` or `GET /models-table`
- **THEN** the request uses the current generation and eligible caches without triggering authority collection

#### Scenario: Missing mandatory authority is not hidden by a saved table

- **WHEN** a forced refresh cannot obtain usable CPA authority
- **THEN** the diagnostic request reports the unavailable/error outcome and does not substitute a previously rendered table

#### Scenario: Routing index no longer served

- **WHEN** a client requests `GET /routing-index?model=x`
- **THEN** the sidecar returns 404

### Requirement: Catalog output order and Codex priority

Every catalog list that model-catalog emits SHALL use one fixed order of the kept members:
1. IDs without `/`, sorted by Unicode code point;
2. then every other ID, sorted by Unicode code point.

Comparison SHALL be exact and case-sensitive, with no locale or case folding. The order SHALL apply to the standard list, every Codex manifest and the `/models-table` rows, and SHALL NOT depend on source order, CPA native order or CPA `priority`.

In every Codex manifest the sidecar SHALL set each row's `priority` to its 1-based position in that order. This SHALL apply to every row, including rows emitted as CPA returned them, and SHALL replace any `priority` that CPA or a metadata source supplied. `priority` SHALL be the only field changed on such rows, and the existing field order of a CPA row SHALL be kept.

The unparameterized front-door `/v1/models` answered by Sub2API is outside this requirement.

#### Scenario: Bare IDs first, code-point order

- **WHEN** the kept members are `z/a`, `b`, `A/b`, `B`, `a` and `a/b`
- **THEN** both the standard list and the Codex manifest list `B`, `a`, `b`, `A/b`, `a/b`, `z/a`

#### Scenario: Priority follows the output order

- **WHEN** CPA returns `codex/m` with `priority: 7` and `devin/x` with `priority: 343`, and the Codex manifest lists them at positions 6 and 7
- **THEN** their `priority` values are 6 and 7

#### Scenario: Passthrough row changes only priority

- **WHEN** a CPA row is emitted as CPA returned it
- **THEN** every field except `priority` equals the CPA row, in CPA's field order, and the generation's copy of the CPA row is not modified

#### Scenario: Order does not change membership

- **WHEN** the same authority inputs are collected
- **THEN** the member set and passthrough set equal those produced without the ordering rule

### Requirement: CPA prefixes come from CPA management data

The sidecar SHALL take the CPA prefix set from the CPA Management API only:
- the non-empty `prefix` of every enabled key channel;
- for every enabled OAuth account, each first segment `P` for which the account's registered models contain both `P/x` and `x`.

The sidecar SHALL NOT download authentication files, read OAuth model definitions or OAuth model aliases for membership, or accept an operator-written prefix list.

The host SHALL compute a native flag for every CPA candidate. The flag is set when the ID is an OAuth registration under an inferred prefix, and no static model has that slug. A CPA member whose native flag is set SHALL be emitted as CPA returned it, without compile, resolution or post-processing and with only `priority` replaced (see "Catalog output order and Codex priority"), unless the script asks for a normal compile. Other CPA members SHALL be compiled normally.

If prefix discovery is incomplete because a management read failed with no eligible cached response, the sidecar SHALL log a warning, report the prefix mode `fallback` to the script, and set the native flag on every CPA candidate that is not a static slug. The default script SHALL then admit every CPA ID containing `/` plus its extra CPA list, and no other CPA ID without `/`.

#### Scenario: OAuth prefix inferred from paired registrations

- **WHEN** an enabled OAuth account registers `supergrok/grok-4.7` and `grok-4.7`
- **THEN** `supergrok` is a CPA prefix, the script sees `supergrok/grok-4.7` with a matched prefix `supergrok` and the native flag set, and the default script's member is emitted as CPA returned it

#### Scenario: Account without a prefix contributes no prefix

- **WHEN** an enabled OAuth account registers only `devin/claude-opus-5-5` and no `claude-opus-5-5`
- **THEN** `devin` is not a CPA prefix, and the script sees `devin/claude-opus-5-5` without a CPA prefix

#### Scenario: Management read failure keeps slash IDs

- **WHEN** an OAuth account's registered-models read fails with no eligible cached response
- **THEN** a warning is logged, the script sees prefix mode `fallback`, and the default script admits every CPA slash ID as CPA returned it and no bare CPA ID outside its extra CPA list

### Requirement: Compatibility with forced model prefixes

On every authority collection the sidecar SHALL read CPA's `force-model-prefix` setting from the Management API. While CPA reports `true`, CPA lists no bare copies, so OAuth prefixes cannot be inferred. In that state the sidecar SHALL report the prefix mode `forced` to the script and SHALL set the native flag on every OAuth registration that is not a static slug.

The default script SHALL, in `forced` mode:
- admit every CPA ID containing `/`, plus its extra CPA list;
- ask for a normal compile of members matched by its extra CPA list, so those are compiled and every other OAuth registration is emitted as CPA returned it, with only `priority` replaced.

A failed read of the setting SHALL count as incomplete prefix discovery.

#### Scenario: Forced prefixes keep today's membership

- **WHEN** the default script runs with the extra CPA list `{"devin/*"}`, CPA reports `force-model-prefix: true` and lists `codex/gpt-6.1-sol` and `devin/claude-opus-5-5`
- **THEN** both are members, `codex/gpt-6.1-sol` is emitted as CPA returned it, and `devin/claude-opus-5-5` is compiled normally

#### Scenario: Default prefixes use the prefix rule

- **WHEN** CPA reports `force-model-prefix: false`
- **THEN** the script sees prefix mode `prefix`, and the default script follows the CPA prefix rule and its extra CPA list

### Requirement: Membership script interface

The membership script SHALL define a global function `select(m)`. The host SHALL call it exactly once per candidate, in candidate order (CPA native-manifest order, then AISIX order, then static configuration order), with a fresh table `m` holding:
- `id`: the exact candidate ID;
- `sources`: a table with `cpa`, `aisix` and `static` set to `true` for each source that lists the ID;
- `mode`: the CPA prefix mode of this collection, `"prefix"`, `"forced"` or `"fallback"`;
- `has_prefix`: whether the ID starts with a known CPA prefix followed by `/` at any `/` boundary;
- `prefix`: the shortest such matched prefix, or `nil`;
- `native`: the host's native flag, `false` for candidates not listed by CPA.

The script SHALL return one of:
- `true`: keep the ID; its source is the first of CPA, AISIX and static that lists it, and the native flag decides passthrough;
- `true, opts`: keep the ID with the optional `opts.source` (`"cpa"`, `"aisix"` or `"static"`, which MUST be one of the ID's sources) and the optional boolean `opts.compile` (`true` compiles a CPA member normally even when the native flag is set);
- `false, reason`: reject the ID; `reason` is a string of at most 200 bytes, longer reasons are cut, and a missing reason is shown as `rejected by catalog.lua`.

Any other return value, an unknown `opts` key or a source the ID does not have SHALL be a script error.

The host SHALL provide `glob(pattern, id)`, which returns whether `id` matches the standard glob `pattern` using the same matcher as `include_models` (exact bytes, case-sensitive, `*` within one segment, `**` across `/`). An invalid pattern SHALL be a script error. `glob` SHALL NOT accept a leading `!`; list negation is written in Lua.

Rejected candidates SHALL NOT appear in any catalog, SHALL NOT be compiled and SHALL NOT trigger `@cpa-upstream` reads.

#### Scenario: Reject with a reason

- **WHEN** the script returns `false, "excluded image model"` for `codex/gpt-image-2`
- **THEN** no catalog lists `codex/gpt-image-2`, and its models-table row shows that reason

#### Scenario: Script cannot invent an ID

- **WHEN** the script sets a global table of extra IDs or returns a different ID string
- **THEN** no new member appears, and returning a string is a script error

#### Scenario: Source not listed for the ID

- **WHEN** the script returns `true, {source = "aisix"}` for an ID that only CPA lists
- **THEN** the collection fails as a script error

#### Scenario: Glob helper uses the include_models matcher

- **WHEN** the script calls `glob("devin/*", "devin/claude-opus-5-5")` and `glob("devin/*", "devin/a/b")`
- **THEN** the first returns `true` and the second returns `false`

### Requirement: Membership script failure keeps the last good generation

A script load error, runtime error, invalid return value, exceeded limit or worker failure SHALL fail that authority collection only:
- no new generation SHALL be published;
- the previous published generation SHALL keep serving, including its catalogs;
- the sidecar SHALL log a WARN with the error class and the candidate ID when one is known, without the script text;
- the models table SHALL show the failed attempt and its error.

The sidecar SHALL NOT publish a generation with zero members because of a script failure. Before the first generation, a failing script SHALL keep `/readyz` not ready, and collection SHALL retry on the bounded cadence.

A successful run that rejects every candidate is not a failure. The sidecar SHALL publish the resulting empty membership as is, SHALL log it at INFO with the candidate and rejected counts, and SHALL NOT block it.

At startup, the sidecar SHALL load and dry-run the script as `validate-config --script` does, and SHALL exit non-zero before listening when that fails.

#### Scenario: Runtime error keeps the previous catalog

- **WHEN** an edited script raises an error for one candidate during a scheduled collection
- **THEN** `/v1/models` keeps serving the previous generation, a WARN names the candidate, and the models table shows the failed attempt

#### Scenario: Broken script at startup

- **WHEN** the sidecar starts with a `catalog.lua` that does not define `select`
- **THEN** it exits non-zero before listening

#### Scenario: Script rejects everything

- **WHEN** an edited script returns a valid `false, reason` for every candidate
- **THEN** the collection succeeds and publishes a generation with no members, an INFO line reports the candidate and rejected counts, and the models table shows every candidate as hidden by `catalog.lua` with its reason
