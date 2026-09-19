## Why

The `ansible/` tree created by `automate-production-ops` works (drift gate, rollback, receipts are rehearsal-proven) but is written in a non-standard style that diverges from Ansible community conventions (reference: matrix-docker-ansible-deploy): shared logic lives in `include_tasks` snippets instead of roles, configuration lives in ini `[group:vars]` instead of `host_vars`/`group_vars` YAML, variable defaults are scattered across `set_fact` and `| default()` filters with no single declaration point, four parallel playbooks duplicate the same compose invocation boilerplate, and the operator's private config directory lives in the operator's home directory (`~/.ai-gateway-ops/config/`) instead of inside the project. These are structure problems, not behavior problems — the deployment guarantees (push model, drift gate, pre-deploy recovery, receipts) must stay identical.

## What Changes

- Restructure `ansible/` into conventional roles: shared deploy-file/rollback-file/preflight logic becomes roles with `defaults/main.yml` declaring every variable, its default, and its documentation in one place.
- Move operator configuration from `inventory.local.ini` `[group:vars]` ini keys to `inventory/group_vars/` + `inventory/host_vars/` YAML, with `examples/vars.yml` as the documented template operators copy.
- **BREAKING**: Move the private config directory from `~/.ai-gateway-ops/config/` into the project at `ansible/private-config/` (gitignored). The repo is the single source location; the private directory holds only operator secrets/state that never commit. Adoption flow (scp-from-production to establish baseline) is unchanged, only the location moves.
- Collapse the four parallel playbooks (`deploy.yml`, `deploy-squid.yml`, `deploy-aisix.yml`, `rollback.yml`) into a single entry playbook with tags selecting the operation (e.g. `--tags deploy-squid`), eliminating the triplicated compose-command boilerplate.
- Unify variable naming under the `ai_ops_` prefix; declare all tunables in role defaults instead of inline `set_fact`.
- Record the deliberate deviations (hand-rolled `ai_ops_mode=plan|apply` instead of `--check`; `command`/`shell` compose invocation instead of `docker_compose_v2`; custom drift-gate state machine) in design.md as accepted decisions, not defects.

## Capabilities

### New Capabilities
- `production-ops-tooling`: Structure and conventions of the Ansible operations tooling — where operator configuration lives, how shared logic is packaged (roles, not task snippets), how operations are invoked (single entry playbook + tags), and how variables are declared (role defaults). The deployment *behavior* (push model, drift gate, rollback, receipts) is owned by the existing `production-operations-control` capability from `automate-production-ops` and is intentionally not re-specified here.

### Modified Capabilities

(none — the existing `production-operations-control` capability lives in the unarchived `automate-production-ops` change, so there is no main-spec base to delta against; its behavioral requirements are unchanged by this restructure)

## Impact

- **Code**: `ansible/` reorganized (roles/, inventory/group_vars/, single entry playbook); `ansible/README.md` rewritten to match; `.gitignore` updated for `ansible/private-config/`.
- **Operator workflow**: inventory.local.ini replaced by `inventory/` YAML; the private config dir moves from `$HOME` into the project; operators re-point their adopted production files on first run after this change.
- **No behavior change**: drift gate semantics, rollback guarantees, receipt format, activation commands, and the push model itself are preserved bit-for-bit. All existing fixture proofs and the rainyun-la rehearsal evidence remain valid because the deployed artifacts and remote state layout do not change.
- **Out of scope**: changing the push model, adding new managed components, altering remote paths, or re-deciding the deliberate deviations listed above.
