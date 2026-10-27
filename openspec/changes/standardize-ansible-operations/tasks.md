## 1. Layout, inventory and toolchain

- [ ] 1.1 Create the D1 skeleton:
  - `ansible.cfg` with the private production inventory default and `roles_path`;
  - `.ansible-lint` at the `production` profile, excluding `private-config/`;
  - `requirements.txt` pinning `ansible-core` 2.21.x and `ansible-lint`;
  - `requirements.yml` pinning `community.docker` 5.4.0 and `community.library_inventory_filtering_v1` 1.1.5, installed into the gitignored `collections/`, with `collections_path = ./collections` in `ansible.cfg`;
  - `inventories/example/`, with `hosts.yml`, `group_vars/gateway.yml` that folds in `examples/vars.yml`, and `host_vars/example.yml`, all using TEST-NET addresses only.

  Verify: `ansible-inventory -i inventories/example --graph` shows `gateway.example`; after `ansible-galaxy collection install -r requirements.yml -p collections`, a test asserts that `ansible-doc community.docker.docker_compose_v2` resolves from `ops/ansible/collections` at version 5.4.0, not from `~/.ansible`.
- [ ] 1.2 Move the production inventory to `private-config/inventories/production/` as YAML (`hosts.yml` plus the current `host_vars`). Delete the tracked `inventory/` and `examples/`. Add the `validate.sh` leak guard (D7). Verify:
  - `ansible-inventory --graph` from `ops/ansible/` shows the production host;
  - `git ls-files ops/ansible | xargs grep -lE '100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.'` prints nothing;
  - the guard fails on a planted fixture.

- [ ] 1.3 Check the production host read-only for the D12 prerequisites: Docker CLI with the Compose plugin 2.18+, the Compose version (for D13), Python `requests` for the remote interpreter, Docker API 1.25+. Install nothing. Verify: the result is recorded in the change; if anything is missing, stop and ask the user before any host package change.
- [ ] 1.4 Add the test-only `community.docker` shim collection (D11): same module names and argument specs, every call logged to the fixture mutation log with its arguments and check-mode flag, canned results from the fixture state. Verify: a test asserts that each shim's argument spec equals the pinned module's spec, and fails when one option is removed from a shim.

## 2. Roles

- [ ] 2.1 `ai_ops` shared role: preflight, capacity gate, the `env -i` argv `ai_ops_compose_argv`, the module arguments (`ai_ops_compose_project_src`, `ai_ops_compose_files`, `ai_ops_compose_env_files`), `ai_ops_scripts_dir`, the read-only prerequisite asserts (D12) and the pinned renderer (D13), with argument specs and the D4 names. Verify:
  - a fixture run of a playbook that calls only `ai_ops` passes preflight; the argument spec rejects an unknown `ai_ops_runtime`;
  - with the renderer directory removed, preflight fetches the pinned binary and verifies its checksum; a tampered binary, a host Compose version that differs from the pin, and a missing `requests` each stop preflight with the named cause;
  - a test asserts that every playbook's `module_defaults` block is identical.
- [ ] 2.2 `ai_ops_managed_file` (prepare/deploy/rollback entry points) with the D5 check-mode rules and `diff: false` for secret labels. Activation and its rescue restore use `ai_ops_managed_file_activate` with `docker_compose_v2` (D12), whose argument spec requires `dependencies: false`. Verify: a fixture test covers five cases:
  - the drift gate blocks;
  - a no-op deploy reports no change;
  - check mode uploads nothing;
  - the `.env` canary secret never appears in `--check --diff` output;
  - the mutation log shows the module call with `services: [<svc>]`, `recreate: always`, `dependencies: false`, `build: never` and the explicit project name, files and env files, and shows no module mutation in check mode.
- [ ] 2.3 `ai_ops_component` (deploy/rollback) from the `front-*` and `component-*` task files. Rename the variables per D4; print the approval digest in check mode and require it otherwise; add static component choices. Keep the `env -i` Compose calls as `command` (D12); use `docker_image_pull` for the approved pull. Verify:
  - `test-component-apply.py` (all cases) and `test-component-deploy.py` pass on the new entry points;
  - a test asserts that the choices equal the keys of `ai_ops_component_profiles`;
  - a fixture with a shadowing variable in the remote environment shows that the render and the recreate still see only the `.env` value.
- [ ] 2.4 `ai_ops_lock` and `ai_ops_recovery`. The recovery role notifies handlers for `daemon-reload` and the timer restart, then `flush_handlers` before verification. Verify: the lock-recovery cases in `test-component-apply.py` and `test-gateway-recovery` pass; check mode on recovery writes no unit.
- [ ] 2.5 `ai_ops_squid`: render, parse and tarball as controller scratch (`check_mode: false`, `changed_when: false`); tree deploy; `ai_ops_squid_source_only`; the egress-proxy restart through `docker_compose_v2` `state: restarted`. Verify:
  - `test-ansible-squid-source.sh` passes, using the variable instead of the `sync-squid-source` tag;
  - a check run on an unchanged fixture tree reports `changed=0`.
- [ ] 2.6 `ai_ops_aisix`, with `ai_ops_aisix_adopted_direct_models` and `ai_ops_aisix_standalone_retained`, upload mode `0444`, and the admin read through `docker_compose_v2_exec` (`check_mode: false`). Verify: `test-ansible-aisix-mode.py` passes, reading the role tasks.
- [ ] 2.7 `ai_ops_model_sync` (deploy/rollback), with `docker_container_info`/`docker_image_info` for identity, `docker_compose_v2_exec` for the read-only env check, and the log read as a `command` argv filtered in Jinja. Verify: `test-ansible-model-sync.sh` passes with `--check` in place of `ai_ops_mode=plan`, including the stale-approval, failure and recovery-mismatch cases.
- [ ] 2.8 `ai_ops_model_catalog` (deploy/rollback), with `validate-config` and `healthcheck` through `docker_compose_v2_exec` (`check_mode: false`) and identity through `docker_container_info`. Verify: `test-ansible-model-catalog-config.sh` passes on the new entry points.

- [ ] 2.9 Add `ops/scripts/tests/test-compose-module-parity.sh` (D11) on a real Docker Engine with the Compose plugin at the renderer version. Verify: it passes in CI, and locally wherever a Docker Engine socket exists; it fails when `dependencies: false` is removed from the activation (the dependency is then recreated).

## 3. Playbooks and retirements

- [ ] 3.1 Add `playbooks/deploy.yml`, `rollback.yml`, `deploy-config.yml`, `rollback-config.yml`, `recovery.yml` and `unlock.yml` per D2. Each validates `component` before connecting, and its gates are tagged `always`. Verify:
  - `ansible-playbook --syntax-check` passes for each;
  - a test shows that an unknown or missing component fails before any connection and lists the choices;
  - `--tags verify` still runs argument validation and preflight.
- [ ] 3.2 Delete `ops.yml`, the old `roles/ai_ops/tasks/*` that moved, and the retired operations: `deploy-ws-keepalive` (user decision, with its source-revision check), `deploy-response-keeper`, `deploy-responses-path`, `restart-response-keeper`, `verify-ws-keepalive.yml`, `verify-response-keeper.yml`, `verify-responses-path.yml` and `test-ansible-ws-keepalive.sh`. Add the replacement `ai-sse-keepalive-proxy` no-op case to `test-component-apply.py`. Verify: `git grep -nE 'ops\.yml|ai_ops_mode|--tags (deploy|rollback|recover|sync)' -- ':!openspec/changes/archive' ':!docs/records'` hits only this change's artifacts.
- [ ] 3.3 Add the state-compatibility test from D8: it asserts the managed-file label set, the `front-<component>` snapshot naming and the lock owner fields. Verify: the test passes, and it fails when a label is changed.

## 4. Lint, CI and docs

- [ ] 4.1 Make `ansible-lint` at the `production` profile pass with zero violations, with no `skip_list`; every `noqa` names its rule and gives a reason. Add the CI `ansible` job (D9): pinned requirements and collections, lint, syntax-check, tests, parity test. Add a grep test that allows the `docker compose` or `docker` CLI in a task only at the kept calls listed in D12. Verify: `uvx ansible-lint` and every `ops/scripts/tests/test-*` pass locally, `ops/scripts/validate.sh .env.example` passes (with `compose.override.yaml` moved aside and restored), and CI is green after the push.
- [ ] 4.2 Update the commands and the rename table in:
  - `ops/ansible/README.md`, `docs/ansible-operations.md` and `docs/gateway-health-and-recovery.md`;
  - the service READMEs: cpa-model-sync, egress-proxy, aisix, model-catalog;
  - `ops/scripts/README.md`.

  Verify: the local link check finds 0 broken links, and `openspec validate --all --strict` passes.
- [ ] 4.3 Commit unsigned and push. Verify: CI is green for the commit.

## 5. Production verification

- [ ] 5.1 On an `openai/*` model, from the Podman workstation with no `docker-compose` on `PATH` and the renderer directory removed first, run `--check --diff` against production for:
  - `deploy.yml` with every profile;
  - `deploy-config.yml` with `aisix`, `egress-proxy`, `cpa-model-sync` and `model-catalog`;
  - `recovery.yml`.

  Verify: every run reports `changed=0 failed=0`; preflight fetched and checksum-verified the pinned renderer itself and found the host Compose version equal to the pin; the output contains no secret values; `docker ps` and the `.ai-ops-state` listing are unchanged before and after; no hand-fetched binary was used.
