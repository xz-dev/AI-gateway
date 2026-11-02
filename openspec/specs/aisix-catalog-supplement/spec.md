# AISIX Catalog Supplement Specification

## Purpose

Supplement the existing enriched CPA catalog with router-exclusive model IDs from the AISIX models API without changing catalog authority, metadata precedence, or client entitlements.

## Requirements

### Requirement: Existing enrichment and authority are retained

AISIX IDs SHALL be enriched through the existing mechanisms. A CPA-admitted ID SHALL keep the CPA native row as its base record. Static override precedence and the final Sub2API entitlement intersection SHALL remain authoritative. The AISIX source SHALL NOT require AISIX admin APIs or a new catalog service.

#### Scenario: Supplemental model is unauthorized

- **WHEN** an AISIX model is absent from a client's Sub2API entitlements
- **THEN** the client-visible catalog excludes it

#### Scenario: No supplemental IDs

- **WHEN** every AISIX ID is also admitted from CPA
- **THEN** no AISIX-only metadata reads occur and the CPA-admitted output is unchanged

### Requirement: Optional failures cannot consume successful CPA results

The optional source SHALL be disabled by default with bounded reads that cannot exhaust the mandatory CPA pipeline. Optional errors SHALL preserve successful CPA output under the existing validated cache/error policy.

#### Scenario: Source disabled or failing
- **WHEN** the source is disabled, times out, or returns malformed data
- **THEN** no request is made (disabled) or existing cache policy applies
- **AND** successful CPA results remain available

#### Scenario: Valid empty inventory
- **WHEN** a valid empty router inventory follows a nonempty one
- **THEN** the supplement becomes empty without stale router-only models

### Requirement: AISIX contributes its complete inventory

The catalog SHALL add every unique ID listed by the AISIX models API to catalog membership, whether or not CPA also lists it. Comparison SHALL be exact and case-sensitive. Accepted IDs SHALL keep their complete bytes. Output order follows the catalog output order in `codex-models-enricher`, not AISIX order.

#### Scenario: Router ID that CPA also lists bare

- **WHEN** AISIX lists `gpt-6.1-sol` and CPA's native manifest also lists a bare `gpt-6.1-sol`
- **THEN** `gpt-6.1-sol` is a catalog member

#### Scenario: Duplicates and case-distinct IDs

- **WHEN** the router lists repeated or case-distinct IDs
- **THEN** exact duplicates are removed and accepted IDs keep their original spelling
