# Tasks: automate-production-ops

Scope: repository implementation only (ansible/, scripts/ops/, skills/, fixtures, docs). No production changes, no private-state adoption, no commits without explicit request.

Deployment model (per owner): edit/review local private config files → one Ansible deploy command uploads them to the remote deployment root and activates (restart/reload). Ansible never constructs config on the remote.

## 1. Ansible scaffold and shared lifecycle

- [x] 1.1 Create `ansible/` layout (ansible.cfg, playbooks/, README) with private inventory/config kept outside the repo; verify `ansible-playbook --syntax-check` passes for every playbook
- [x] 1.2 Implement shared preflight: verify SSH/auth without fallback, resolve deployment root + Compose project + explicit compose file order, capture runtime/compose versions; verify with a local Podman fixture that a plan run performs zero mutations (container IDs and remote files unchanged)
- [x] 1.3 Implement drift gate: checksum-compare each deploy-managed remote file against the recorded last-deployed baseline, show local-vs-remote diff, stop on mismatch; verify fixture where the remote file was hand-edited aborts before any upload
- [x] 1.4 Implement protected state dir on the remote (0700) holding last-deployed baseline copies + sanitized receipts, with no_log for secret-bearing tasks; verify fixture receipt contains no secret sentinel values and state dir mode is 0700
- [x] 1.5 Implement recovery path: upload protected pre-deploy file copies and reactivate; verify fixture recovery restores the prior file content and refuses to proceed when the drift check shows independent remote change after the deploy

## 2. Component deployment operation (CPA image upgrade profile)

- [x] 2.1 Implement `playbooks/deploy.yml` with a component profile (service name, compose files, activation = `up -d --no-deps --no-build <service>`); verify plan mode on fixture shows old/new image selector diff and does not touch containers
- [x] 2.2 Apply: pull exact image before interruption, upload edited compose/env files, record pre-deploy copies, recreate only the profile service; verify fixture (two trivial containers) recreates only the target and preserves the other's container ID
- [x] 2.3 Verify idempotence: second apply with unchanged local files reports no effective change and the target container ID is unchanged
- [x] 2.4 Verify drift gate integration: hand-edit the remote compose file after a deploy, re-run, assert abort before upload/restart
- [x] 2.5 Verify activation failure recovery: fixture with a failing readiness check uploads pre-deploy copies, reactivates, and reports recovery outcome
- [x] 2.6 Add capacity/interruption preflight (fresh mem/disk read, no swap tuning) as plan-visible checks blocking apply on failure; verify a forced-fail gate stops before any container change

## 3. Squid ACL operation

- [x] 3.1 Implement `playbooks/deploy-squid.yml`: render local policy.json via `scripts/render-egress-policy.py` into a local staging dir; verify plan mode writes nothing remote and a custom sibling entry adopted in the local policy survives rendering
- [x] 3.2 Validate rendered config with pinned Squid image `-k parse` in local Podman; verify an intentionally broadened wildcard path rule is rejected before upload
- [x] 3.3 Implement upload + activation (reconfigure/restart per profile) and drift gate on the remote generated dir; verify fixture applies the new rule and an identical second run performs no reload/restart
- [x] 3.4 Verify recovery: activation failure uploads the pre-deploy rendered dir, reactivates, and verifies the prior deny boundary is restored

## 4. AISIX route lifecycle operation

- [x] 4.1 Implement `playbooks/deploy-aisix.yml`: validate local resources.yaml (`scripts/check-aisix-resource-policy.py` + `scripts/ops/check-aisix-orphans.py`), show route/target diff vs remote, upload, activate via application-only recreation; verify fixture route edit deploys and custom fields in the local file are present remotely after activation
- [x] 4.2 Orphan report integration: plan output lists orphan candidates and standalone retentions; verify shared-target and standalone fixtures produce empty candidate lists and an unreferenced adopted model is listed as candidate only
- [x] 4.3 Approved deletion: deleting an orphan declaration from the local file deploys and the effective AISIX inventory no longer lists it — **proven with pinned image `ghcr.io/xz-dev/ai-gateway-aisix:1.2.0-deadlockfix-7d6d14b`**: restarted fixture loaded edited resources.yaml and `/admin/v1/models` (auth `Authorization: Bearer <key>`) showed `['logical-a','direct/a']` after removing `direct/b`
- [x] 4.4 Verify retry-field guard: an edit setting literal `retries: -1` is rejected by the pre-deploy validation
- [x] 4.5 Verify drift gate + recovery for the resources file (remote hand-edit aborts; failure recovery re-uploads pre-deploy copy)

## 5. Sub2API management API skill

- [x] 5.1 Write `skills/sub2api-ops/SKILL.md`: version-matched supported API workflow (inspect → propose exact fields → approve → fresh read → minimal update → read-back), no SQL fallback, secret hygiene, timeout read-before-retry rule; verify it references verified endpoints only (mark unverified contracts as pending)
- [x] 5.2 Add a minimal API-double fixture demonstrating preserve-unrelated-fields update flow and lost-response read-back; verify the fixture script passes locally

## 6. Operator docs and closeout

- [x] 6.1 Write `ansible/README.md` runbook: local private config layout, edit/review/deploy commands per operation, drift-gate and recovery behavior; verify command names match implemented playbooks via syntax check
- [x] 6.2 Run all fixture checks end-to-end and `openspec validate automate-production-ops --type change --strict --no-interactive`; all pass; pinned-AISIX fixture proven (4.3), Sub2API endpoint contract remains pending live-version verification (documented in skill)
