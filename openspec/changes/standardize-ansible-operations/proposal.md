## Why

The Ansible tooling under `ops/ansible/` does not follow Ansible conventions:
- one 1375-line play dispatches every operation through `--tags`;
- one role holds all logic;
- the read-only preview is a custom `-e ai_ops_mode=plan`, not `--check`;
- the tracked `inventory/hosts.ini` contains the production host address;
- `ansible-lint` reports 296 violations at the `basic` profile.

Operators and agents must learn a private dialect, lint cannot guard changes, and the inventory leaks production data into git.

## What Changes

- **BREAKING (operations)**: one playbook per operation under `ops/ansible/playbooks/`:
  - `deploy.yml` and `rollback.yml` for component transactions;
  - `deploy-config.yml` and `rollback-config.yml` for private configuration files;
  - `recovery.yml` for the host recovery guard;
  - `unlock.yml` for stale lock recovery.

  The target is selected with `-e component=<name>` and validated by role argument specs. Tags only filter tasks; they never select an operation. `ops.yml` is removed.
- **BREAKING (operations)**: the read-only preview is standard `--check --diff`. `ai_ops_mode` is removed. Approval digests are printed in check mode and still required for apply.
- **BREAKING (operations)**: the production inventory moves to the gitignored `ops/ansible/private-config/inventories/production/`. The repository tracks only `inventories/example/`. `ansible.cfg` defaults to the production path. The tracked `inventory/hosts.ini`, `group_vars/` and `host_vars/` are removed.
- The single `ai_ops` role is split into a shared role (`ai_ops`) and one role per concern:
  - `ai_ops_managed_file`, `ai_ops_component`, `ai_ops_lock`, `ai_ops_recovery`;
  - `ai_ops_squid`, `ai_ops_aisix`, `ai_ops_model_sync`, `ai_ops_model_catalog`.

  Each role has `meta/argument_specs.yml`, and handlers where a restart is event-driven.
- Variables follow role-prefixed names. The few operator variables whose role changes are renamed; the rename table is in design D4.
- `.ansible-lint` at the `production` profile with zero violations, run in CI with `ansible-playbook --syntax-check` for every playbook. `requirements.txt` pins `ansible-core` and `ansible-lint`.
- Retired operations are deleted: `deploy-response-keeper`, `deploy-responses-path` and `restart-response-keeper` with their task files. The dedicated `deploy-ws-keepalive` operation is also removed; the generic `ai-sse-keepalive-proxy` component profile replaces it (see design Open Questions).
- The Squid source-only sync becomes `deploy-config.yml -e component=egress-proxy -e ai_ops_squid_source_only=true`. It no longer uses the `sync-squid-source` tag.
- Tests, docs, READMEs and the operations spec move to the new entry points.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `production-ops-tooling`: the tooling shape changes:
  - inventory location and privacy;
  - roles with argument specs;
  - one playbook per operation instead of tag dispatch;
  - standard check mode instead of `ai_ops_mode`;
  - the variable naming scheme;
  - the recorded deviations;
  - a lint gate.

  The model-sync, model-catalog and front-component operation requirements are reworded for the new entry points. The stale `models-enricher` operation names become `model-catalog`.

## Impact

- `ops/ansible/`: new `playbooks/`, `roles/*`, `inventories/example/`, `ansible.cfg`, `.ansible-lint`, `requirements.txt`, README. Removed: `ops.yml`, `inventory/` and `examples/vars.yml` (folded into `inventories/example/`).
- `ops/ansible/private-config/inventories/production/`: new, gitignored. The tracked host address leaves the working tree, but it stays in git history (see design Open Questions).
- `ops/scripts/tests/test-ansible-*`, `test-component-apply.py`, `test-component-deploy.py`, `test-standard-cache-topology.py`: entry points and the check-mode invocation.
- `.github/workflows/validate.yml`: lint and syntax-check job.
- `docs/ansible-operations.md`, `docs/gateway-health-and-recovery.md`, service READMEs: commands.
- Production: no host change. Remote `.ai-ops-state` keys depend on file labels and component names, not tags, and both are kept. Final check: every deploy playbook in `--check --diff` reports no change.
