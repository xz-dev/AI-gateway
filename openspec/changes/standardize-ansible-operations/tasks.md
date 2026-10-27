## 1. Layout, inventory and toolchain

- [ ] 1.1 Create the D1 skeleton:
  - `ansible.cfg` with the private production inventory default and `roles_path`;
  - `.ansible-lint` at the `production` profile, excluding `private-config/`;
  - `requirements.txt` pinning `ansible-core` 2.21.x and `ansible-lint`;
  - `inventories/example/`, with `hosts.yml`, `group_vars/gateway.yml` that folds in `examples/vars.yml`, and `host_vars/example.yml`, all using TEST-NET addresses only.

  Verify: `ansible-inventory -i inventories/example --graph` shows `gateway.example`.
- [ ] 1.2 Move the production inventory to `private-config/inventories/production/` as YAML (`hosts.yml` plus the current `host_vars`). Delete the tracked `inventory/` and `examples/`. Add the `validate.sh` leak guard (D7). Verify:
  - `ansible-inventory --graph` from `ops/ansible/` shows the production host;
  - `git ls-files ops/ansible | xargs grep -lE '100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.'` prints nothing;
  - the guard fails on a planted fixture.

## 2. Roles

- [ ] 2.1 `ai_ops` shared role: preflight, capacity gate, Compose argv (`ai_ops_compose`, `ai_ops_compose_argv`) and `ai_ops_scripts_dir`, with argument specs and the D4 names. Verify: a fixture run of a playbook that calls only `ai_ops` passes preflight; the argument spec rejects an unknown `ai_ops_runtime`.
- [ ] 2.2 `ai_ops_managed_file` (prepare/deploy/rollback entry points) with the D5 check-mode rules and `diff: false` for secret labels. Verify: a fixture test covers four cases:
  - the drift gate blocks;
  - a no-op deploy reports no change;
  - check mode uploads nothing;
  - the `.env` canary secret never appears in `--check --diff` output.
- [ ] 2.3 `ai_ops_component` (deploy/rollback) from the `front-*` and `component-*` task files. Rename the variables per D4; print the approval digest in check mode and require it otherwise; add static component choices. Verify:
  - `test-component-apply.py` (all cases) and `test-component-deploy.py` pass on the new entry points;
  - a test asserts that the choices equal the keys of `ai_ops_component_profiles`.
- [ ] 2.4 `ai_ops_lock` and `ai_ops_recovery`. The recovery role notifies handlers for `daemon-reload` and the timer restart, then `flush_handlers` before verification. Verify: the lock-recovery cases in `test-component-apply.py` and `test-gateway-recovery` pass; check mode on recovery writes no unit.
- [ ] 2.5 `ai_ops_squid`: render, parse and tarball as controller scratch (`check_mode: false`, `changed_when: false`); tree deploy; `ai_ops_squid_source_only`. Verify:
  - `test-ansible-squid-source.sh` passes, using the variable instead of the `sync-squid-source` tag;
  - a check run on an unchanged fixture tree reports `changed=0`.
- [ ] 2.6 `ai_ops_aisix`, with `ai_ops_aisix_adopted_direct_models` and `ai_ops_aisix_standalone_retained`, and upload mode `0444`. Verify: `test-ansible-aisix-mode.py` passes, reading the role tasks.
- [ ] 2.7 `ai_ops_model_sync` (deploy/rollback). Verify: `test-ansible-model-sync.sh` passes with `--check` in place of `ai_ops_mode=plan`, including the stale-approval, failure and recovery-mismatch cases.
- [ ] 2.8 `ai_ops_model_catalog` (deploy/rollback). Verify: `test-ansible-model-catalog-config.sh` passes on the new entry points.

## 3. Playbooks and retirements

- [ ] 3.1 Add `playbooks/deploy.yml`, `rollback.yml`, `deploy-config.yml`, `rollback-config.yml`, `recovery.yml` and `unlock.yml` per D2. Each validates `component` before connecting, and its gates are tagged `always`. Verify:
  - `ansible-playbook --syntax-check` passes for each;
  - a test shows that an unknown or missing component fails before any connection and lists the choices;
  - `--tags verify` still runs argument validation and preflight.
- [ ] 3.2 Delete `ops.yml`, the old `roles/ai_ops/tasks/*` that moved, and the retired operations: `deploy-ws-keepalive` (per Q1), `deploy-response-keeper`, `deploy-responses-path`, `restart-response-keeper`, `verify-ws-keepalive.yml`, `verify-response-keeper.yml`, `verify-responses-path.yml` and `test-ansible-ws-keepalive.sh`. Add the replacement `ai-sse-keepalive-proxy` no-op case to `test-component-apply.py`. Verify: `git grep -nE 'ops\.yml|ai_ops_mode|--tags (deploy|rollback|recover|sync)' -- ':!openspec/changes/archive' ':!docs/records'` hits only this change's artifacts.
- [ ] 3.3 Add the state-compatibility test from D8: it asserts the managed-file label set, the `front-<component>` snapshot naming and the lock owner fields. Verify: the test passes, and it fails when a label is changed.

## 4. Lint, CI and docs

- [ ] 4.1 Make `ansible-lint` at the `production` profile pass with zero violations, with no `skip_list`; every `noqa` names its rule and gives a reason. Add the CI `ansible` job (D9). Verify: `uvx ansible-lint` and every `ops/scripts/tests/test-*` pass locally, `ops/scripts/validate.sh .env.example` passes (with `compose.override.yaml` moved aside and restored), and CI is green after the push.
- [ ] 4.2 Update the commands and the rename table in:
  - `ops/ansible/README.md`, `docs/ansible-operations.md` and `docs/gateway-health-and-recovery.md`;
  - the service READMEs: cpa-model-sync, egress-proxy, aisix, model-catalog;
  - `ops/scripts/README.md`.

  Verify: the local link check finds 0 broken links, and `openspec validate --all --strict` passes.
- [ ] 4.3 Commit unsigned and push. Verify: CI is green for the commit.

## 5. Production verification

- [ ] 5.1 On an `openai/*` model, run `--check --diff` against production for:
  - `deploy.yml` with every profile;
  - `deploy-config.yml` with `aisix`, `egress-proxy`, `cpa-model-sync` and `model-catalog`;
  - `recovery.yml`.

  Verify: every run reports `changed=0 failed=0`, the output contains no secret values, and `docker ps` and the `.ai-ops-state` listing are unchanged before and after.
