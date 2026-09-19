## Purpose

Make AISIX route edits and approved cleanup repeatable from private desired configuration while preserving exact upstream identities, bounded routing behavior, and intentionally retained direct models.

## ADDED Requirements

### Requirement: Route edits happen in the local resources file

Route changes SHALL be made in the local private AISIX resources YAML (by hand or a local helper script) and reviewed locally before deploy. The deploy SHALL upload the reviewed file and activate it via the tested recreation path. The plan SHALL show added or removed targets and changes to order, priority, weight, strategy, retry, fallback, and cooldown fields against the last-deployed baseline. It SHALL reject duplicate identities, missing references, an empty target set for a retained route, and unsupported route shapes rather than silently translate them. An ordered failover route SHALL NOT become a balancing route as a side effect of editing its membership.

#### Scenario: Remove one backup from an ordered route

- **GIVEN** an ordered route contains three exact CPA targets
- **WHEN** the operator removes the last backup in the local file and deploys
- **THEN** the two remaining targets retain their names, order, priorities, and existing policy

#### Scenario: Route edit retains custom fields in the same object

- **GIVEN** the local resources file carries private fields on a route and its targets adopted from production
- **WHEN** one identified backup target is removed locally and deployed
- **THEN** the remaining targets keep their relative order and custom fields, and the route keeps its unedited fields

#### Scenario: A retained route loses its last target

- **WHEN** a local edit leaves a retained logical route with no target
- **THEN** validation rejects the candidate before deploy rather than deploying a broken route or deleting the route implicitly

### Requirement: Retry and fallback changes have verified bounded semantics

Each changed retry or fallback field SHALL have a verified meaning for the selected AISIX version, and the plan SHALL show the resulting configured limits separately. A literal `retries: -1` SHALL NOT be inferred from a request for `target_count - 1` fallbacks. Unknown, unbounded, or incompatible values SHALL block that parameter change, not authorize an arbitrary substitute. The operation SHALL retain the existing prohibition on retrying or switching targets after generated output reaches the client.

#### Scenario: Target-count fallback policy is explicitly selected

- **GIVEN** the operator has explicitly selected and verified a maximum fallback count of target count minus one
- **WHEN** a route changes from three targets to two
- **THEN** the plan changes `max_fallbacks` from two to one
- **AND** it does not change `retries` or claim that this alone establishes the total upstream attempt count

#### Scenario: Negative retry value is unverified

- **WHEN** a requested `retries: -1` has no verified supported bounded meaning for the deployed version
- **THEN** the parameter change is blocked with the unresolved field and version identified
- **AND** a separate route edit preserving verified current retry settings remains possible

### Requirement: Orphan reports distinguish managed, shared, and standalone models

The operation SHALL inspect references from all retained AISIX logical routes, including unowned routes, before proposing deletion of a managed direct model. It SHALL report unreferenced managed direct models that are not explicitly retained for standalone use as cleanup candidates, not automatically unused resources. Shared targets, standalone-retained models, and unowned resources SHALL remain untouched. Incomplete inventory or uninterpretable references SHALL block cleanup instead of treating missing evidence as zero references.

#### Scenario: One removed target is still shared

- **GIVEN** both `coding` and `review` reference the same direct model
- **WHEN** that model is removed only from `coding`
- **THEN** it is retained for `review` and is not proposed for deletion

#### Scenario: Standalone access is intentional

- **GIVEN** a managed direct model has no logical-route references but is explicitly retained for standalone use
- **WHEN** an orphan report is generated
- **THEN** the model is reported as retained rather than a deletion candidate

### Requirement: Cleanup is an explicit AISIX-only deletion set

Route edits and related cleanup SHALL be reviewed together, including the exact direct model declarations to delete and the loss of their AISIX catalog/direct-call exposure. An unapproved cleanup candidate SHALL remain present. Deletion SHALL be refused while any retained AISIX reference points to it. Cleanup SHALL NOT delete CPA channels/models/credentials, Sub2API accounts/mappings/entitlements, provider keys, caller keys, or Squid rules. Potential effects outside AISIX SHALL be reported for separate review, not resolved by cascading mutation.

#### Scenario: Approve removal of an orphan declaration

- **GIVEN** an adopted direct model becomes unreferenced, is not retained for standalone use, and its deletion is explicitly approved
- **WHEN** the route update and cleanup are applied
- **THEN** that declaration is absent from the effective AISIX resources and AISIX's model catalog
- **AND** CPA inventory and Sub2API configuration remain unchanged; no gateway-wide disappearance is claimed

#### Scenario: Route edit is approved but cleanup is not

- **WHEN** an operator approves the route edit without approving the listed orphan deletions
- **THEN** the orphan declarations remain in the desired and effective AISIX resources
- **AND** the receipt states that they can remain directly exposed by AISIX

### Requirement: Application verifies effective routing rather than file presence

The operation SHALL validate the complete candidate and reference set before activation, preserve provider and caller credentials, and verify the effective runtime route fields and approved direct-model removals afterward. It SHALL NOT treat a successful file copy or an empty API response as proof of activation. If the deployed loader does not remove omitted resources, the operation SHALL use a verified deletion method or stop without claiming cleanup. Recovery SHALL upload the protected pre-deploy copy of the resources file and reactivate it under the common operations contract, without changing account placement or reviving a retired router.

#### Scenario: Omitted model is still loaded

- **WHEN** a removed direct model remains in the effective AISIX inventory after activation
- **THEN** verification fails and the receipt reports incomplete cleanup rather than success

#### Scenario: Route validation fails before activation

- **WHEN** the candidate includes an unresolved target reference
- **THEN** the running routes and credentials remain unchanged
