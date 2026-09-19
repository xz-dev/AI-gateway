# Tasks

## 1. Role skeleton + variable audit

- [x] 1.1 Create `ansible/roles/` with role directories for preflight / deploy-file / rollback-file (single `ai_ops` role with task files, or three roles — implementer's call per design Open Question), each with `defaults/main.yml`. Verify: `ansible-playbook --syntax-check` passes on a trivial playbook referencing the roles.
- [x] 1.2 Sweep existing playbooks/tasks and produce the variable inventory: every `ai_ops_*`, `_ai_*`, `_compose`, `service`, `adopted_direct_models`, `standalone_retained`, `squid_image` etc., classified as operator-tunable vs internal. Verify: the inventory list exists as a working note and every operator-tunable has a target `defaults/main.yml` home.
- [x] 1.3 Move every operator-tunable default into role `defaults/main.yml` with a comment; rename stray variables to the `ai_ops_` prefix; leave internal temporaries underscore-prefixed and undeclared. Verify: `rg '\|\s*default\(' ansible/playbooks ansible/roles/*/tasks` returns only non-tunable internal uses; all tunables resolve from defaults.

## 2. Inventory + examples

- [x] 2.1 Create `ansible/inventory/group_vars/gateway.yml` and `ansible/inventory/host_vars/` layout, and `ansible/examples/vars.yml` as the documented copy-template. Verify: template contains every operator-facing variable with a comment.
- [x] 2.2 Update `ansible.cfg` to point at `inventory/`; retire `inventory.local.ini`. Verify: `ansible-inventory --graph` resolves rainyun-la with all variables from YAML sources.
- [x] 2.3 Update `.gitignore`: ignore `ansible/private-config/` and operator inventory overrides, keep `examples/` tracked. Verify: `git status --ignored` shows private-config and operator inventory as ignored; `git add -A` staging test stages nothing from either.

## 3. Port logic into roles + single entry playbook

- [x] 3.1 Port `tasks/preflight.yml`, `tasks/deploy-file.yml`, `tasks/rollback-file.yml` into role task files **verbatim** (no behavior edits). Verify: file diff of task bodies shows only include-path/mechanical changes.
- [x] 3.2 Create the single entry playbook with `tags: deploy / deploy-squid / deploy-aisix / rollback`; compose command prefix defined once (role default or group_vars) and referenced by all operation paths. Verify: `--list-tags` shows the four operations; `rg 'compose.yaml' ansible/playbooks` shows the -f/-env-file prefix string exactly once.
- [x] 3.3 Update `ansible/README.md`: new layout, tag usage, variable reference, private-config location, and the deliberate-deviation rationale from design D6. Verify: README documents every tag and every operator-facing variable.

## 4. Private config migration

- [x] 4.1 Move operator config `~/.ai-gateway-ops/config/` → `ansible/private-config/`; set default `ai_ops_config_dir` accordingly with inventory override preserved. Verify: plan run resolves all managed files from the new path.

## 5. Parity verification (behavior unchanged)

- [x] 5.1 Fixture regression: superseded by direct real-host verification (fixtures were cleaned up from the previous change; the restructure was verified against rainyun-la directly, which is stronger evidence).
- [x] 5.2 rainyun-la smoke check: plan-mode run of each tag (zero remote writes), then apply no-op deploy — drift gate reports "no effective change" for compose.yaml/compose.override.yaml/.env, CPA container ID unchanged, receipts written to `.ai-ops-state/`. deploy-squid verified with the new tarball+manifest mechanism: first apply deployed + `sha256sum -c` verified, hand-edit to a policy file detected as drift and blocked, tree restored, re-apply idempotent (no effective change, no restart).
- [x] 5.3 Deleted old `playbooks/deploy*.yml`, `playbooks/rollback.yml`, `tasks/*.yml`, `inventory.local.ini` in the same change; repo tree matches design layout.

## 6. Close-out

- [x] 6.1 Run `openspec validate normalize-ansible-structure --strict` and fix all findings.
- [x] 6.2 Final review: every spec requirement in `specs/production-ops-tooling/spec.md` maps to a task above; README accurate; no stray references to old playbook paths remain (`rg 'deploy-squid.yml|deploy-aisix.yml|inventory.local.ini' ansible/` returns nothing outside git history).
