## 1. Role

- [x] 1.1 Add `ai_ops_recovery_init` (auto/systemd/openrc) and the OpenRC path variables to defaults and argument specs. Move the systemd tasks unchanged into `tasks/systemd.yml`.
- [x] 1.2 Add `tasks/openrc.yml`: read-only preconditions; install the init script, loop and logrotate file without backups; validate; enable and start the service; verify the cgroup ceilings. Add the OpenRC restart handler.

## 2. Tests and docs

- [x] 2.1 Add a fixture test: OpenRC check mode writes no file and makes no start/restart/enable call. Keep the existing systemd check-mode test passing; run `ansible-lint` and the playbook syntax checks.
- [x] 2.2 Document that `playbooks/recovery.yml` supports OpenRC hosts, and update the migration record's follow-up.

## 3. Production acceptance

- [x] 3.1 Run `playbooks/recovery.yml --check --diff` against production; it must show no file differences and no service action.
- [x] 3.2 Run one apply: no changed files, no restart. Then verify that the service is still running with the same start time, the cgroup ceilings are unchanged and the recovery log keeps reporting ok. Pass `openspec validate --all --strict`.

Evidence (2026-11-16): fixture tests 16/16 (`test-ansible-preflight.py`) and 12/12 (`test-ansible-operations.py`); `ansible-lint` production profile with 0 failures; all playbook syntax checks pass. Production check: 46 ok / 0 changed, `lxc=True`. Production apply at 10:55 UTC: 51 ok / 0 changed, no handler. The `supervise-daemon` PID was unchanged and running for 23h56; memory.max 67108864 and cpu.max `10000 100000`; no backup files; recovery log 8/8 ok.
