## MODIFIED Requirements

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

## ADDED Requirements

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

## REMOVED Requirements

### Requirement: Membership exception list

**Reason**: `membership.cpa_include` is replaced by the extra CPA list in the profile's `catalog.lua`, which can also express any other membership rule.

**Migration**: move each `membership.cpa_include` entry into the `EXTRA_CPA` table of the private `catalog.lua`, with the same order and `!` entries, and delete the `membership` block from `config.yaml`.
