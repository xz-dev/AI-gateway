## ADDED Requirements

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
