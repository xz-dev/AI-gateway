## MODIFIED Requirements

### Requirement: Catalog membership comes from authority sources only

Before `include_models` selection, catalog membership SHALL be the union of the following sources, deduplicated by exact, case-sensitive ID:
- every ID listed by the AISIX models API;
- every CPA native-manifest ID that starts with a CPA prefix followed by `/`;
- every other CPA native-manifest ID matched by `membership.cpa_include`;
- configured gateway static models.

A CPA native-manifest ID that matches none of these SHALL NOT appear, including:
- IDs without `/`;
- slash IDs whose first segment is not a CPA prefix, such as an upstream original `deepseek/deepseek-v4.1-flash`.

When an ID is admitted from CPA, its CPA native row SHALL be its base record, even if AISIX or a static model also lists it. A CPA row for an ID that is not admitted from CPA SHALL NOT be used as that ID's `@self` record.

Source precedence on a duplicate SHALL be CPA-admitted, then AISIX, then static. Member output order SHALL follow "Catalog output order and Codex priority" and SHALL NOT depend on source order.

`@cpa-upstream` rows and bundled metadata SHALL only contribute fields. A model present only in a channel's upstream list SHALL NOT appear.

#### Scenario: Channel-only model does not appear

- **WHEN** a channel's upstream list contains `m-new` but CPA's native manifest does not
- **THEN** no catalog lists `<prefix>/m-new`

#### Scenario: Prefixed CPA ID is a member

- **WHEN** CPA lists `codex/gpt-6.1-sol` and `codex` is a CPA prefix
- **THEN** the member set contains `codex/gpt-6.1-sol` with the CPA native row as its base record

#### Scenario: Bare CPA ID is not a member

- **WHEN** CPA lists `gpt-6.1-sol` and AISIX and the static models do not
- **THEN** no catalog lists `gpt-6.1-sol`

#### Scenario: Slash original without a CPA prefix is not a member

- **WHEN** CPA lists `deepseek/deepseek-v4.1-flash`, `deepseek` is not a CPA prefix, and `membership.cpa_include` does not match it
- **THEN** no catalog lists `deepseek/deepseek-v4.1-flash`

#### Scenario: Exception list admits unprefixed CPA IDs

- **WHEN** `membership.cpa_include: ["devin/*"]` and CPA lists `devin/claude-opus-5-5` from an account without a prefix
- **THEN** the member set contains `devin/claude-opus-5-5`

#### Scenario: Admitted unprefixed model listed

- **WHEN** `membership.cpa_include: ["my-alias"]`, CPA lists the operator-configured unprefixed model `my-alias`, and `include_models` is absent
- **THEN** the catalogs list `my-alias` with chain-selected metadata

#### Scenario: Every AISIX ID is a member

- **WHEN** AISIX lists the bare logical ID `gpt-6.1-sol` and CPA also lists a bare `gpt-6.1-sol`
- **THEN** the member set contains `gpt-6.1-sol` once, as an AISIX member without a CPA `@self` record

#### Scenario: ID listed by both CPA and AISIX

- **WHEN** CPA admits `codex/gpt-5.3-codex-spark` and AISIX also lists it
- **THEN** it appears once, with the CPA native row as its base record

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

## ADDED Requirements

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

A CPA-admitted ID SHALL be emitted as CPA returned it, without compile, resolution or post-processing and with only `priority` replaced (see "Catalog output order and Codex priority"), when an OAuth account registers it under an inferred prefix and no static model has that slug. IDs admitted through a key-channel prefix or through `membership.cpa_include` SHALL be compiled normally.

If prefix discovery is incomplete because a management read failed with no eligible cached response, the sidecar SHALL log a warning. It SHALL admit every CPA native-manifest ID containing `/`, emitting it as CPA returned it with only `priority` replaced, plus the IDs matched by `membership.cpa_include`. It SHALL NOT admit any other CPA ID without `/` on this path.

#### Scenario: OAuth prefix inferred from paired registrations

- **WHEN** an enabled OAuth account registers `supergrok/grok-4.7` and `grok-4.7`
- **THEN** `supergrok` is a CPA prefix and `supergrok/grok-4.7` is admitted and emitted as CPA returned it

#### Scenario: Account without a prefix contributes no prefix

- **WHEN** an enabled OAuth account registers only `devin/claude-opus-5-5` and no `claude-opus-5-5`
- **THEN** `devin` is not a CPA prefix

#### Scenario: Management read failure keeps slash IDs

- **WHEN** an OAuth account's registered-models read fails with no eligible cached response
- **THEN** a warning is logged, every CPA slash ID is admitted as CPA returned it, and no bare CPA ID outside `membership.cpa_include` is admitted

### Requirement: Membership exception list

The optional `membership.cpa_include` field SHALL be an ordered list of standard glob patterns, using the same matcher and semantics as `metadata.include_models`:
- exact-byte, case-sensitive matching;
- the last matching entry decides;
- a leading `!` excludes.

It SHALL only admit CPA native-manifest IDs that do not start with a CPA prefix. It SHALL NOT remove prefixed CPA IDs, AISIX IDs or static models; `include_models` remains the only way to hide members.

When the field is absent, no exception applies. These SHALL be configuration errors:
- an empty list;
- a non-string entry;
- an invalid pattern;
- an unknown key under `membership`.

#### Scenario: Last match decides

- **WHEN** `membership.cpa_include: ["devin/*", "!devin/model_private_*"]` and CPA lists `devin/model_private_2` and `devin/glm-5-3`
- **THEN** `devin/glm-5-3` is a member and `devin/model_private_2` is not

#### Scenario: Exception list cannot hide prefixed IDs

- **WHEN** `membership.cpa_include: ["!codex/*"]` and CPA lists `codex/gpt-6.1-sol` with `codex` a CPA prefix
- **THEN** `codex/gpt-6.1-sol` is still a member

#### Scenario: Empty list rejected

- **WHEN** the configuration sets `membership.cpa_include: []`
- **THEN** configuration validation fails and names the field

### Requirement: Compatibility with forced model prefixes

On every authority collection the sidecar SHALL read CPA's `force-model-prefix` setting from the Management API. While CPA reports `true`, CPA lists no bare copies, so OAuth prefixes cannot be inferred. In that state the sidecar SHALL:
- admit every CPA native-manifest ID containing `/`, plus the IDs matched by `membership.cpa_include`;
- emit an ID as CPA returned it, with only `priority` replaced, when an OAuth account registers it and `membership.cpa_include` does not match it.

A failed read of the setting SHALL count as incomplete prefix discovery.

#### Scenario: Forced prefixes keep today's membership

- **WHEN** CPA reports `force-model-prefix: true` and lists `codex/gpt-6.1-sol` and `devin/claude-opus-5-5`
- **THEN** both are members, `codex/gpt-6.1-sol` is emitted as CPA returned it, and `devin/claude-opus-5-5` is compiled normally

#### Scenario: Default prefixes use the prefix rule

- **WHEN** CPA reports `force-model-prefix: false`
- **THEN** CPA membership follows the CPA prefix rule and `membership.cpa_include`
