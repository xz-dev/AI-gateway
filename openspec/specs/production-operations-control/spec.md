# Production Operations Control Specification

## Purpose

Make repeatable production changes from operator-owned private desired configuration, with explicit ownership, truthful drift plans, bounded disruption, and recoverable outcomes.

## Requirements
### Requirement: Adopted private configuration owns only declared resources

The operations workflow SHALL distinguish desired configuration, observed live state, and the last successfully applied configuration. Adoption SHALL require operator review of the current baseline and an explicit ownership list for image selectors, Compose fields, Squid ACL entries/fields, and AISIX route fields/target entries. Owning one field or entry SHALL NOT imply ownership of its enclosing service, model, policy, list, or file. Private desired values SHALL be authoritative only within that declared scope. Observation alone SHALL NOT authorize adoption or overwrite desired values. Credentials, OAuth state, databases, usage data, and panel/API-managed settings SHALL remain outside reconciliation unless separately and explicitly adopted. Repository upgrades SHALL NOT replace private desired configuration with examples.

#### Scenario: Existing production differs from the checkout

- **GIVEN** production has private overrides and legitimate changes not present in Git
- **WHEN** the operator requests an adoption plan
- **THEN** the workflow reports the differences and proposed ownership without changing serving state
- **AND** unmatched production state remains unowned until reviewed

#### Scenario: An application updates its own data

- **GIVEN** the workflow owns AISIX routing but not CPA's synchronized inventory or Sub2API accounts
- **WHEN** those application-managed values change
- **THEN** a later deployment preserves them rather than restoring an old captured copy

### Requirement: Changes preserve unselected configuration within managed objects

Each operation SHALL change only explicitly selected fields or entries, preserving custom values and unselected fields even inside the same object. Missing desired fields or list entries SHALL NOT imply deletion, reset to defaults, or replacement of an enclosing object. Deletions SHALL be explicit. Before activation, the workflow SHALL verify that the effective configuration difference contains only approved changes, including when a complete file must be written. It SHALL preserve native override precedence and list identity/order semantics rather than apply a generic whole-object template or merge. Ambiguous identity, unsupported configuration that cannot be safely preserved, or unexpected effective changes SHALL stop the operation without rewriting serving configuration. These constraints SHALL also apply to recovery.

#### Scenario: Image update preserves a customized Compose service

- **GIVEN** CPA has custom mounts, environment, command, network settings, and private overrides
- **WHEN** an approved operation changes only its effective image selector
- **THEN** those custom settings and the established override precedence remain unchanged
- **AND** an override that prevents the requested image from taking effect is reported rather than bypassed by rewriting the service

#### Scenario: Unspecified fields are not deletions

- **GIVEN** an operator supplies one desired field while sibling fields and list entries have private values
- **WHEN** the candidate configuration is prepared
- **THEN** the unspecified values and entries remain present with unchanged semantics
- **AND** no repository template defaults replace them

#### Scenario: A custom field cannot be preserved safely

- **WHEN** the operation cannot construct or validate a candidate without dropping or changing an unselected custom field
- **THEN** it stops with the unsupported configuration identified
- **AND** it does not fall back to a template-generated replacement

### Requirement: Planning observes without applying

A plan SHALL identify the target host, canonical deployment root, Compose project, selected artifact set, expected configuration differences, affected services, deletions, verification, and recovery scope. Planning SHALL NOT pull images, build, start test containers, regenerate production files, reload services, modify APIs, or acquire a persistent production deployment lock. Unknown readiness or compatibility SHALL be reported as unknown, not success. Human-facing output SHALL be a sanitized projection rather than a raw environment or credential dump.

#### Scenario: An operator plans a CPA image update

- **WHEN** a plan selects a different CPA image
- **THEN** it shows the old and desired image identity, required preparation, expected recreation set, and checks
- **AND** the running CPA container and production configuration remain unchanged

#### Scenario: A validator has write side effects

- **WHEN** a proposed validation command builds images or starts containers
- **THEN** the read-only plan excludes that execution and reports the separate validation requirement

### Requirement: Apply is bound to approval and fresh state

Apply SHALL require explicit approval of the target, effective diff, deletion set, service interruption, verification traffic, and recovery action. Before mutation it SHALL verify the desired-input identity and fresh preconditions, serialize cooperating operations, and reject overlapping unexplained drift. An expired plan SHALL NOT silently absorb new changes. Authentication failure SHALL stop the operation without trying an unapproved alternative access method.

#### Scenario: Routing changes after approval

- **GIVEN** an approved plan selects particular AISIX routing fields
- **WHEN** those fields or their deployment preconditions differ before application
- **THEN** apply stops with a drift result and requests a fresh plan instead of overwriting them

#### Scenario: Unrelated private state changes

- **GIVEN** a plan owns only the CPA image selector
- **WHEN** an unrelated panel setting changes without affecting deployment preconditions
- **THEN** the workflow preserves that setting and does not expand its mutation scope

### Requirement: Artifact and capacity gates precede disruption

The workflow SHALL identify the exact deployable artifacts and retain prior artifact identities for recovery. It SHALL reject implicit latest-version selection and unreviewed dirty-source deployment. Required images SHALL be prepared and verified before stopping serving components; application SHALL NOT build on the production host. Fresh disk, memory, existing swap use, temporary candidate overhead, and active-traffic conditions SHALL be evaluated against the selected operation's requirements. Insufficient capacity or an unmet interruption gate SHALL stop deployment without automatic system tuning.

#### Scenario: A candidate does not fit

- **WHEN** the proposed candidate and serving stack cannot coexist within the approved capacity margin
- **THEN** deployment stops before starting the candidate or stopping serving components
- **AND** it does not increase swap or change resource limits to force the deployment through

#### Scenario: An artifact is unavailable or changed

- **WHEN** the selected artifact cannot be obtained or no longer matches its approved identity
- **THEN** serving state is preserved and the workflow reports the preparation failure

### Requirement: Recreation respects shared runtime dependencies and is idempotent

The workflow SHALL distinguish a proven application-only replacement from a network, namespace-owner, relay, or other coupled change. It SHALL present the required service set and ordering before approval and SHALL NOT independently restart a shared namespace owner or use unconditional full-stack recreation for a single-service update. Repeating an already satisfied operation SHALL NOT recreate containers or reload configuration unnecessarily. Changed mounted configuration SHALL be activated even when the image and Compose declaration are unchanged.

#### Scenario: Application-only CPA upgrade

- **GIVEN** only the CPA image changes and its existing namespace and dependency contracts remain valid
- **WHEN** the approved update is applied
- **THEN** only CPA is recreated and unrelated container identities are checked for unexpected changes

#### Scenario: Shared tunnel owner changes

- **WHEN** a proposed change requires replacing the provider-sidecar tunnel namespace
- **THEN** the plan includes the dependent sidecar's ordered rebuild
- **AND** an application-only approval cannot authorize that larger operation

#### Scenario: Desired state already runs

- **WHEN** the same verified desired state is applied again
- **THEN** the workflow reports no effective change without stopping containers or reloading Squid

### Requirement: Recovery material is scoped and protected

Before mutation the workflow SHALL capture affected configuration with ownership and modes, prior image identities, and any consistent application backup required by the selected upgrade. Database backups SHALL use an application-supported consistent method rather than an unqualified copy of live database files. Configuration recovery SHALL upload the protected pre-deploy copy of the same files and reactivate; the drift gate guarantees those files are exactly what this operation replaced. Recovery SHALL NOT apply a pre-deploy copy when the drift check shows the remote changed independently after this deploy. Image rollback SHALL NOT be represented as data rollback when schema compatibility is unknown. Recovery assets SHALL remain protected and SHALL NOT be deleted by routine deployment.

#### Scenario: A config-only deployment fails verification

- **WHEN** the approved config change fails its stop threshold
- **THEN** recovery uploads the pre-deploy files and reactivates the prior configuration using verified backups
- **AND** it does not restore databases or overwrite unrelated settings

#### Scenario: Remote changed after the failed deploy

- **WHEN** the drift check shows the remote changed independently after this deploy
- **THEN** recovery stops for operator review instead of overwriting the newer state

#### Scenario: The new application migrated its database

- **WHEN** image downgrade compatibility has not been established
- **THEN** the workflow stops with an explicit recovery requirement rather than blindly starting the old image against the changed data

### Requirement: Verification and receipts distinguish success from incomplete evidence

The workflow SHALL verify runtime artifact identity, effective configuration, explicit readiness endpoints, affected behavior, and unrelated-service invariants. Container running state alone SHALL NOT prove service or inference health. Required live inference or interruption SHALL be separately covered by the approved plan; unapproved billable probes SHALL NOT run. Receipts SHALL record desired and observed identities, checks performed and skipped, actual mutation scope, recovery location, and success, stopped, or recovery-required outcome without credentials or raw private responses. An incomplete recovery SHALL NOT be reported as successful deployment.

#### Scenario: Health passes but inference was not tested

- **WHEN** a scoped upgrade passes readiness and provenance checks without an authorized provider request
- **THEN** the receipt states those checks passed and explicitly does not claim end-to-end inference success

#### Scenario: Control is lost during application

- **WHEN** a connection fails after some approved changes have taken effect
- **THEN** a subsequent run re-observes live state and consults the protected receipt before proceeding
- **AND** it does not assume the whole operation either succeeded or rolled back

### Requirement: Model-sync policy deployment preserves drift, approval, and recovery guarantees
The model-sync operation SHALL apply the existing per-file drift gate, protected pre-deploy copy, baseline update, rollback guard, and sanitized receipt guarantees to the remote model-sync policy. It SHALL additionally bind apply to an explicitly approved preview digest covering the candidate policy, fresh source inventory, and complete desired per-channel model sets. Independent remote edits or a changed preview digest SHALL stop deployment before upload unless the operator reviews and approves a fresh plan.

#### Scenario: Remote policy changed independently
- **WHEN** the remote model-sync policy differs from both its recorded baseline and the local desired policy
- **THEN** deployment stops before upload or container recreation and reports that reconciliation is required

#### Scenario: Preview state changed after approval
- **WHEN** apply's fresh preview digest differs from the explicitly approved digest
- **THEN** deployment stops before upload and reports the changed channel/model diff for new approval

#### Scenario: Policy activation fails
- **WHEN** an approved changed model-sync policy is uploaded but activation cannot recreate the sidecar successfully
- **THEN** the operation restores the protected pre-deploy policy, attempts to recreate the sidecar with it, and reports the recovery outcome without claiming success

### Requirement: Changed policy activates only the model-sync sidecar
A changed model-sync policy SHALL be activated by recreating only `cpa-model-sync`, without rebuilding images, restarting CPA, or recreating unrelated services. Because the sidecar reads configuration only at startup, a content change SHALL force recreation even when Compose service configuration and image identity are unchanged. An unchanged desired policy with a current baseline SHALL remain a no-op.

#### Scenario: Include or exclude policy changes
- **WHEN** the fresh preview digest is approved and the validated local policy differs from the drift-free remote policy
- **THEN** the file is uploaded and only `cpa-model-sync` is force-recreated without dependencies or image builds

#### Scenario: Desired policy already runs
- **WHEN** local, remote, and baseline policy checksums match
- **THEN** the operation does not recreate the sidecar or perform a synchronization probe

### Requirement: Apply verifies both execution and resulting CPA inventories
Before mutation, the operation SHALL capture the current per-channel model sets needed for recovery. After recreating `cpa-model-sync`, it SHALL wait within a bounded interval for the new container to complete its immediate real synchronization round, then re-read CPA and compare every managed channel's actual model set with the approved desired set. Success SHALL require the recreated container to remain running, the round summary to contain zero `failed` and zero `unconfirmed` channels, and every read-back set to equal its approved desired set. `updated`, `unchanged`, and configured `skipped` channels SHALL be acceptable only when read-back also matches the approved preview. The operation SHALL NOT issue a separate duplicate synchronization run.

#### Scenario: Real synchronization and read-back succeed
- **WHEN** the recreated sidecar reports zero failed and unconfirmed channels, remains running, and CPA read-back equals every approved desired set
- **THEN** deployment records successful live verification with sanitized aggregate counts and set digests

#### Scenario: Process succeeds but inventory differs
- **WHEN** the synchronization summary reports success but any CPA channel's actual model set differs from the approved desired set
- **THEN** deployment fails, restores the previous policy, recreates the sidecar, and verifies recovery against the captured pre-apply model sets

#### Scenario: Real synchronization reports failure
- **WHEN** the recreated sidecar's first completed round reports one or more failed or unconfirmed channels, exits, or does not complete before the bounded timeout
- **THEN** deployment fails, restores the pre-deploy policy, recreates the sidecar with the restored policy, and verifies recovery against the captured pre-apply model sets

### Requirement: Model-sync receipts distinguish approval, mutation, inventory verification, and recovery
A model-sync receipt SHALL record policy and preview digests, whether the approved preview remained fresh, whether the file changed, the recreated container identity or equivalent provenance, sanitized synchronization totals, desired/observed per-channel set digests and counts, checks skipped, and final success or recovery-required outcome. It SHALL NOT contain the management key, request URLs carrying secrets, raw model inventories, or unbounded container logs.

#### Scenario: Deployment succeeds after updating models
- **WHEN** live execution and CPA read-back match the approved desired sets with no failures
- **THEN** the receipt stores only bounded aggregate counts, set digests, and verified policy/container identities

#### Scenario: Verification recovery is incomplete
- **WHEN** restoration or the recovered CPA model sets cannot be proven equal to the captured pre-apply sets
- **THEN** the receipt and final error report a recovery-required state rather than successful deployment
