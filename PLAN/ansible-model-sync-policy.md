# Ansible-managed CPA model-sync policy

Implement OpenSpec change `add-ansible-model-sync-deploy`.

## Local implementation

- [x] Rust read-only preview reuses synchronization filtering and emits deterministic approval data.
- [x] Rust fixtures prove preview parity, zero PATCH, validation failures, and stable output.
- [x] Shared deploy-file lifecycle verifies before baseline and proves recovery.
- [x] `deploy-model-sync` supports plan diff, approval digest, sidecar-only recreation, read-back, rollback, and sanitized receipt.
- [x] Ansible fixtures/documentation and repository validation pass.

## Production gates

- [x] Publish/deploy immutable preview-capable sidecar image.
- [x] Adopt reviewed remote policy into ignored local private config.
- [x] Review plan additions/removals and approve digest.
- [x] Apply, verify CPA read-back, and confirm unrelated container identities unchanged.
- [x] Rehearse bounded failure/recovery.

Production gates require separate explicit confirmation because they publish an image, access the production host, and mutate CPA model inventories.

## Residual items

- `rollback-model-sync` verification asserts `failed=0` on the restored round; when the old policy carries pre-existing upstream failures, rollback reports failure even though digest-level CPA recovery is proven. Semantics decision deferred.
- `kimi-cn-api` shows intermittent upstream failures unrelated to this change; watch and diagnose separately if it recurs.
- `egress-proxy` was externally recreated (SIGTERM, squid normal exit, no OOM) at 2026-09-20T01:12Z, unrelated to this change.
- `compose.yaml` mem-limit edits in the working tree predate this change and are not part of it.
