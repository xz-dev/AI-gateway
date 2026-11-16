## Why

Production moved to an Alpine OpenRC LXC on 2026-11-15. `playbooks/recovery.yml` (role `ai_ops_recovery`) can install only the systemd service and timer, so on the new host the tracked OpenRC files in `ops/scripts/host/openrc/` were installed by hand. The recovery guard can therefore no longer be reinstalled, updated or verified through the documented operation.

## What Changes

- `ai_ops_recovery` detects the host's service manager (`systemd` or `openrc`; override with `ai_ops_recovery_init`) and refuses anything else.
- On OpenRC hosts it installs the existing tracked `ai-gateway-recovery` init script, `ai-gateway-recovery-loop` and logrotate file. The deploy root in the loop is rendered the same way as in the systemd units. It then validates the scripts, enables the service in the default runlevel, starts it, and verifies that the service cgroup enforces the 64 MiB / 10% CPU ceilings.
- Preconditions on OpenRC: the cgroup root must delegate the `cpu` and `memory` controllers. In an LXC, `cgroup-delegate` must be enabled in the `boot` runlevel; otherwise the ceilings would silently disappear after a reboot.
- Behaviour on systemd hosts is unchanged.

Out of scope: the `ai-gateway` stack service and `cgroup-delegate` themselves, which stay manual host bootstrap on both init systems, and `gateway-recovery.py` (its `startup_unit` still reports `unknown` on OpenRC).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `production-ops-tooling`: the host recovery guard install supports OpenRC hosts.

## Impact

`ops/ansible/roles/ai_ops_recovery`, a fixture test in `ops/scripts/tests/test-ansible-preflight.py`, and the docs on OpenRC hosts. Production acceptance is `playbooks/recovery.yml --check --diff` with no file differences, followed by one apply that is expected to change nothing.
