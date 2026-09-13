## MODIFIED Requirements

### Requirement: Full cutover and cleanup retain recovery evidence

Production cutover SHALL require explicit approval, fresh complete recovery state, and stop thresholds. Today the affected production accounts serve directly from the AISIX hostname (`base_url=http://aisix:3000/v1`); the unified internal entrance is deployed and reachable but carries no active serving placement. Migration SHALL update each ordinary CPA-backed production account to target the unified internal entrance hostname (which selects CPA or AISIX per catalog membership), touching only the owned `base_url` and necessary routing fields within the latest complete credentials object, preserving account credentials, caller keys, and unrelated fields — including concurrent peer changes to pool/retry settings. Accounts opted into the separately approved Headroom change SHALL NOT be blindly overwritten: their participation is gated on that change's approved rebase so Headroom's downstream targets the internal entrance (`Sub2API → Headroom → internal APISIX → CPA/AISIX`) rather than bypassing the selector or losing compression. Rollback SHALL restore the prior AISIX serving behavior for affected accounts (including the previous direct-AISIX placement) while preserving independent peer configuration edits — not merely reverting entrance routes to a CPA catch-all. Failures SHALL be repaired in place or use captured data/account state without restoring New API or CPA model routing. After real-client and zero-routed-traffic verification, candidate test records/project and the superseded model-router edge SHALL be removed while private recovery snapshots remain. Account migration and any production rollout under this reconciliation remain separately approved operations.

#### Scenario: Approved full switch
- **WHEN** the operator approves full migration
- **THEN** each ordinary CPA-backed production account receives only the approved `base_url` and necessary routing-field changes toward the unified internal entrance, computed from the latest complete credentials object with unrelated fields preserved
- **AND** accounts opted into the parallel Headroom change are handled per the coordination gate rather than overwritten
- **AND** real client requests verify Responses, catalog entitlements, and backend selection before cleanup

#### Scenario: Rollback restores prior serving behavior
- **WHEN** a stop threshold fails and rollback is invoked
- **THEN** affected accounts' prior serving placement (direct AISIX or entrance as before migration) is restored while peer pool/retry configuration edits are preserved
- **AND** rollback does not silently leave accounts on a CPA-only catch-all that cannot serve AISIX-only models

#### Scenario: Post-switch regression
- **WHEN** a stop threshold fails
- **THEN** the fault is repaired without weakening security, or captured data/account state is used to recover service without restoring a retired logical-model router
