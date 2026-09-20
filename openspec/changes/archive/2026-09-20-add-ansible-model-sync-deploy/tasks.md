## 1. Sidecar Read-Only Preview

- [x] 1.1 Refactor config loading, regex compilation, channel discovery, inventory fetching, and desired-set calculation so daemon synchronization and preview share one implementation; verify existing Rust tests plus new parity fixtures prove both paths calculate identical desired sets.
- [x] 1.2 Add a versioned `--preview -` CLI mode that reads candidate JSON from stdin, requires the existing management credential, performs GET/source reads only, and emits deterministic canonical JSON; verify tests reject unknown fields, invalid limits/regexes, and any PATCH attempt while preserving existing daemon and `--once` behavior.
- [x] 1.3 Include per-channel current/desired/add/remove/unchanged data plus policy, image-input, source-inventory, current-set, desired-set, and global approval digests; verify repeated fixture runs are byte-stable and credentials/raw account records never appear.
- [x] 1.4 Build the pinned versioned image and run preview against controlled CPA/source fixtures; verify the preview desired sets match a subsequent `--once` result and that invalid candidates leave fixture inventories unchanged.

## 2. Shared Deployment Transaction

- [x] 2.1 Extend `deploy-file.yml` with the minimum optional post-activation and recovery-verification hooks needed to delay baseline recording until verification succeeds; verify existing callers behave unchanged when hooks are absent.
- [x] 2.2 Preserve protected candidate, approved desired-set, and pre-apply current-set artifacts with mode `0600`; verify candidate failure restores the old file, reruns activation, and records a new baseline only after recovery proof succeeds.
- [x] 2.3 Add recovery-required reporting when restored-policy reconciliation cannot reproduce the captured pre-apply sets; verify fixtures never report candidate success or complete recovery for timeout, malformed evidence, or set mismatch.

## 3. Model-Sync Plan and Approval

- [x] 3.1 Add the dedicated `deploy-model-sync` tag, private local/remote policy paths, and `ai_ops_model_sync_approved_digest`; verify `ansible-playbook ops.yml --list-tags`, syntax check, role defaults, and inventory override behavior.
- [x] 3.2 Stream changed candidate policy bytes to `--preview -` inside the running sidecar container and render complete per-channel additions/removals with counts and the global digest; verify plan uploads no file, starts no container, issues no PATCH, and recreates nothing.
- [x] 3.3 Make apply recompute preview and require exact approved-digest equality before upload; verify changed policy, running image, channel identities, current sets, source inventories, or desired sets all stop apply before mutation.
- [x] 3.4 Preserve first-adoption and no-op behavior; verify matching reviewed local/remote bytes establish a missing baseline without preview/recreation and matching local/remote/baseline bytes remain a complete no-op.

## 4. Activation, Read-Back, and Recovery

- [x] 4.1 Upload only after fresh approval and force-recreate only `cpa-model-sync` with no dependencies or build; verify CPA and unrelated container identities remain unchanged.
- [x] 4.2 Capture the new container/image boundary, parse its first bounded synchronization summary, require zero failed/unconfirmed channels, and require the service to remain running; verify success, exit, timeout, malformed/missing summary, failed, and unconfirmed fixtures.
- [x] 4.3 Re-read CPA after candidate synchronization and compare actual per-channel sets with the approved desired sets rather than newly calculated source results; verify a process-success/set-mismatch fixture fails closed.
- [x] 4.4 On any activation or read-back failure, restore the previous policy, recreate only the sidecar, wait for its first round, and compare CPA sets with the captured pre-apply sets; verify both complete recovery and explicit recovery-required outcomes.
- [x] 4.5 Implement guarded manual rollback using the same old-policy reconciliation and read-back proof; verify independent remote drift blocks rollback before overwrite.

## 5. Evidence and Documentation

- [x] 5.1 Write sanitized receipts containing policy/image/container identities, approval/freshness result, mutation state, synchronization totals, per-channel counts/digests, and recovery outcome; verify receipts exclude management keys, environment dumps, raw account records, complete model lists, and unbounded logs.
- [x] 5.2 Update Ansible README, usage comments, role defaults, and inventory examples for image prerequisite, private policy adoption, preview review, approval digest, apply, read-back, and recovery; verify every tunable uses the `ai_ops_` prefix and is documented once with its default.
- [x] 5.3 Add local fixture/rehearsal coverage for invalid policy, unintended removals visible in plan, stale approval, first adoption, no-op, candidate success, process-success/set-mismatch, partial mutation, recovery success, and recovery mismatch; verify focused Rust and Ansible test commands pass.
- [x] 5.4 Run `openspec validate add-ansible-model-sync-deploy --strict` and relevant repository validation scripts; verify the implementation diff contains no unrelated Compose service, credential, filtering-semantic, or routing changes.

## 6. Production Adoption and Live Acceptance

- [x] 6.1 Build/publish the preview-capable image, obtain its immutable identity, and deploy it through the existing approved component path; verify ordinary periodic synchronization remains unchanged before enabling policy deployment.
- [x] 6.2 Fetch the current remote policy into ignored `ansible/private-config/cpa-model-sync/config.json`, review it, prove local/remote equality, and establish the baseline without recreation; verify Git reports the private file ignored.
- [x] 6.3 Make the intended local include/exclude change and run plan mode; review every affected channel's complete additions/removals and approve the resulting digest, explicitly checking that no legitimate model is unintentionally removed.
- [x] 6.4 Apply with the approved digest and let the recreated sidecar perform its normal immediate synchronization; verify fresh-preview equality, zero failed/unconfirmed counts, CPA read-back equals the approved desired sets, unrelated container identities are unchanged, and the receipt is sanitized.
- [x] 6.5 Rehearse a bounded candidate failure or set-mismatch path, then verify restored-policy reconciliation returns CPA to the captured pre-apply sets; record any unresolved mismatch as recovery-required before declaring production acceptance.
