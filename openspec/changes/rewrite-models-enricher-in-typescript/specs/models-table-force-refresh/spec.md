## MODIFIED Requirements

### Requirement: Force refresh covers the full catalog collection chain
A force-refresh request SHALL bypass the enricher's eligible read caches for:
- the CPA and AISIX raw snapshots;
- management discovery and identity reads;
- `@cpa-upstream` responses needed by the compile set;
- the catalog projection.

Metadata bundled into the running image SHALL be reused, because it involves no network read. A force refresh SHALL perform at most one active refresh collection at a time and SHALL NOT trigger the independent `cpa-model-sync` service.

#### Scenario: Newly loaded CPA provider becomes visible
- **WHEN** CPA has loaded a newly configured provider and an operator submits a force refresh
- **THEN** the refresh re-reads the current CPA inventory and the needed `@cpa-upstream` data instead of waiting for the normal cache cadence
- **AND** the resulting table includes the provider's eligible prefixed models when collection succeeds

#### Scenario: Concurrent refresh requests share work
- **WHEN** multiple operators submit force refresh while one refresh is active
- **THEN** the system coalesces them onto one bounded upstream collection
- **AND** each request receives the result associated with that collection

#### Scenario: CPA model sync remains independent
- **WHEN** an operator submits a force refresh
- **THEN** the system does not invoke, restart, or add a control request to `cpa-model-sync`

## ADDED Requirements

### Requirement: Models table shows hidden and referenced-only models
The models table SHALL list every admitted model, including models that `include_models` hides.

Each hidden model SHALL be marked hidden and SHALL show what decided it:
- the deciding rule path and pattern, for example `include_models[1]` and `!**/gpt-image-*`; or
- "no include rule matched".

A hidden model that the compile set needs through `inherit` or `metadata_from` SHALL additionally be marked "referenced by" with the referencing slugs, and SHALL show its compiled metadata. Other hidden models SHALL show no compiled metadata, chain or source fields, because they were not processed.

The table SHALL summarize kept, hidden and referenced-only counts. These markings SHALL NOT affect JSON catalog contents.

#### Scenario: Excluded model shown with its rule
- **WHEN** `include_models: ["**", "!**/gpt-image-*"]` and CPA admits `codex/gpt-image-1`
- **THEN** the table row for `codex/gpt-image-1` is marked hidden by `include_models[1]` and shows no compiled metadata

#### Scenario: Referenced hidden model shows metadata
- **WHEN** hidden model `vendor/y` is the `metadata_from` target of kept model `pool/x`
- **THEN** its row is marked hidden, referenced by `pool/x`, and shows its compiled metadata

#### Scenario: Unmatched model shown as hidden
- **WHEN** `include_models: ["codex/**"]` and CPA admits `other/m`
- **THEN** the row for `other/m` is marked hidden with "no include rule matched"
