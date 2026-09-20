## Why

The `cpa-model-sync` Rust sidecar already supports per-channel `include`, `exclude`, and skip policy, but operators must currently edit its bind-mounted JSON on the server and restart the container manually. Managing this policy through the existing Ansible push model will make changes reviewable, drift-gated, recoverable, and operable without interactive server edits.

## What Changes

- Add a dedicated `deploy-model-sync` operation to the single Ansible operations playbook.
- Treat `ansible/private-config/cpa-model-sync/config.json` as the operator-owned desired policy and deploy it to the existing remote bind-mount path.
- Add sidecar-native validation and read-only preview modes so the same Rust parser and filtering logic can validate a candidate and calculate per-channel model additions/removals without PATCHing CPA.
- Make plan mode display the complete model-set diff and a deterministic approval digest; make apply recompute that preview and refuse mutation unless it matches the explicitly approved digest.
- Recreate only `cpa-model-sync` when approved policy content changes because the sidecar reads configuration only at startup.
- Verify the recreated sidecar through both its completed real synchronization summary and a CPA read-back proving the resulting per-channel model sets equal the approved desired sets.
- On activation or verification failure, restore the previous policy, recreate the sidecar, and compare recovery read-back with the captured pre-apply inventory snapshot before claiming recovery.
- Preserve file drift protection, pre-deploy recovery copies, rollback, no-op idempotence, and sanitized receipts without requiring interactive server edits.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `production-ops-tooling`: Extend the tag-selected Ansible operation surface and private desired-configuration layout to cover the model-sync policy.
- `production-operations-control`: Define preview-bound approval, safe activation, actual-inventory read-back, failure recovery, and receipt behavior for model-sync policy changes.

## Impact

Affected areas are `ansible/ops.yml`, the `ai_ops` role defaults and shared deploy/rollback behavior where needed, Ansible documentation/examples, focused validation/preview support in `cpa-model-sync`, and tests or fixture rehearsals for the new operation. The change does not alter model-sync filtering semantics, CPA credentials, channel discovery, the managed provider-kind set, AISIX routing, CPA serving behavior, or other containers.

The private production policy remains ignored and must be adopted from the current remote file before its first managed deployment. Live synchronization is intentionally allowed only after the read-only preview's complete model diff has been approved and revalidated against fresh CPA state. Successful process execution alone is insufficient: acceptance requires the observed CPA inventory to equal the approved desired inventory, while recovery must be reported incomplete if it cannot re-establish the captured pre-apply state.
