## Purpose

Supplement the existing enriched CPA catalog with router-exclusive model IDs from the AISIX models API without changing catalog authority, metadata precedence, or client entitlements.

## ADDED Requirements

### Requirement: Supplement is an exact original-inventory difference

The catalog SHALL compute unique AISIX models API IDs minus complete original CPA models API IDs captured before CPA-local identity filtering. Comparison SHALL be case-sensitive; accepted IDs SHALL preserve their complete bytes.

#### Scenario: Overlapping ID filtered by CPA identity rules
- **WHEN** an ID exists in both inventories but CPA-local filtering rejects it
- **THEN** the supplement does not resurrect it

#### Scenario: Duplicates and case-distinct IDs
- **WHEN** the router lists repeated or case-distinct IDs
- **THEN** exact duplicates are removed and accepted IDs keep their original spelling

### Requirement: Existing enrichment and authority are retained

Only router-exclusive IDs SHALL be supplemented via existing enrichment mechanisms. CPA metadata, static override precedence, and final Sub2API entitlement intersection SHALL remain authoritative. The supplement SHALL NOT require AISIX admin APIs or a new catalog service.

#### Scenario: Supplemental model is unauthorized
- **WHEN** a router-exclusive model is absent from a client's Sub2API entitlements
- **THEN** the client-visible catalog excludes it

#### Scenario: No supplemental IDs
- **WHEN** every router ID already exists in the original CPA inventory
- **THEN** no supplement-only metadata reads occur and existing output is unchanged

### Requirement: Optional failures cannot consume successful CPA results

The optional source SHALL be disabled by default with bounded reads that cannot exhaust the mandatory CPA pipeline. Optional errors SHALL preserve successful CPA output under the existing validated cache/error policy.

#### Scenario: Source disabled or failing
- **WHEN** the source is disabled, times out, or returns malformed data
- **THEN** no request is made (disabled) or existing cache policy applies
- **AND** successful CPA results remain available

#### Scenario: Valid empty inventory
- **WHEN** a valid empty router inventory follows a nonempty one
- **THEN** the supplement becomes empty without stale router-only models
