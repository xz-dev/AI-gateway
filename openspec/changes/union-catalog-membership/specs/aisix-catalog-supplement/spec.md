## ADDED Requirements

### Requirement: AISIX contributes its complete inventory

The catalog SHALL add every unique ID listed by the AISIX models API to catalog membership, whether or not CPA also lists it. Comparison SHALL be exact and case-sensitive. Accepted IDs SHALL keep their complete bytes and first-seen order.

#### Scenario: Router ID that CPA also lists bare

- **WHEN** AISIX lists `gpt-6.1-sol` and CPA's native manifest also lists a bare `gpt-6.1-sol`
- **THEN** `gpt-6.1-sol` is a catalog member

#### Scenario: Duplicates and case-distinct IDs

- **WHEN** the router lists repeated or case-distinct IDs
- **THEN** exact duplicates are removed and accepted IDs keep their original spelling

## MODIFIED Requirements

### Requirement: Existing enrichment and authority are retained

AISIX IDs SHALL be enriched through the existing mechanisms. A CPA-admitted ID SHALL keep the CPA native row as its base record. Static override precedence and the final Sub2API entitlement intersection SHALL remain authoritative. The AISIX source SHALL NOT require AISIX admin APIs or a new catalog service.

#### Scenario: Supplemental model is unauthorized

- **WHEN** an AISIX model is absent from a client's Sub2API entitlements
- **THEN** the client-visible catalog excludes it

#### Scenario: No supplemental IDs

- **WHEN** every AISIX ID is also admitted from CPA
- **THEN** no AISIX-only metadata reads occur and the CPA-admitted output is unchanged

## REMOVED Requirements

### Requirement: Supplement is an exact original-inventory difference

**Reason**: CPA's default configuration also lists bare IDs. Subtracting CPA's original inventory would then drop AISIX logical IDs such as `gpt-6.1-sol`, so the difference no longer identifies router-exclusive IDs.

**Migration**: AISIX contributes every ID; see "AISIX contributes its complete inventory". CPA-side filtering is now the CPA prefix rule in `codex-models-enricher`.
