## MODIFIED Requirements

### Requirement: Models table shows hidden and referenced-only models
The models table SHALL list every membership candidate, including candidates that the membership script rejects and members that `include_models` hides.

Each hidden model SHALL be marked hidden and SHALL show what decided it:
- for a candidate the script rejected: `catalog.lua` and the script's reason;
- for a member that `include_models` hides: the deciding rule path and pattern, for example `include_models[1]` and `!**/gpt-image-*`, or "no include rule matched".

A hidden model that the compile set needs through `inherit` or `metadata_from` SHALL additionally be marked "referenced by" with the referencing slugs, and SHALL show its compiled metadata. Only members hidden by `include_models` can be referenced this way. Other hidden models SHALL show no compiled metadata, chain or source fields, because they were not processed.

The table SHALL summarize kept, hidden and referenced-only counts, and SHALL count script-rejected candidates separately from `include_models` hides. These markings SHALL NOT affect JSON catalog contents.

#### Scenario: Excluded model shown with its rule
- **WHEN** `include_models: ["**", "!**/gpt-image-*"]` and CPA admits `codex/gpt-image-1`
- **THEN** the table row for `codex/gpt-image-1` is marked hidden by `include_models[1]` and shows no compiled metadata

#### Scenario: Referenced hidden model shows metadata
- **WHEN** hidden model `vendor/y` is the `metadata_from` target of kept model `pool/x`
- **THEN** its row is marked hidden, referenced by `pool/x`, and shows its compiled metadata

#### Scenario: Unmatched model shown as hidden
- **WHEN** `include_models: ["codex/**"]` and CPA admits `other/m`
- **THEN** the row for `other/m` is marked hidden with "no include rule matched"

#### Scenario: Script-rejected candidate shown with its reason
- **WHEN** the default script rejects the bare CPA ID `gpt-6.1-sol` with the reason "CPA ID without a CPA prefix"
- **THEN** the row for `gpt-6.1-sol` is marked hidden by `catalog.lua` with that reason, shows no compiled metadata, and is counted as script-rejected

#### Scenario: Failed script attempt shown
- **WHEN** the latest collection failed with a membership script error
- **THEN** the table shows the failed attempt and its error class, and its rows are those of the generation still served
