## Context

The systemd path copies `gateway_health.py`/`gateway-recovery.py`, verifies the probes, installs `ai-gateway-recovery.service`/`.timer` with the deploy root substituted, runs `systemd-analyze verify` and enables the timer. The OpenRC equivalent is already tracked and in production: `ops/scripts/host/openrc/ai-gateway-recovery`, a `supervise-daemon` service with `rc_cgroup_settings` of 64 MiB and 10% CPU; `ai-gateway-recovery-loop`, which waits 120 s after boot and runs a 120 s-bounded cycle every 30 s; and `ai-gateway-recovery.logrotate`. The live host reports `ansible_service_mgr=openrc`, and `openrc.ai-gateway-recovery` currently has `memory.max=67108864` and `cpu.max=10000 100000`.

## Goals / Non-Goals

**Goals:** One recovery operation for both init systems, reusing the tracked files unchanged, with the same check-mode guarantees: no writes, no service start/restart/enable.

**Non-Goals:** Managing the `ai-gateway` stack service or `cgroup-delegate`, changing the loop's timing or ceilings, or changing `gateway-recovery.py`.

## Decisions

1. Select the init system with `ai_ops_recovery_init: auto|systemd|openrc` (default `auto`). `auto` gathers only the `service_mgr` fact and fails unless it is `systemd` or `openrc`. The existing systemd tasks move to `tasks/systemd.yml` unchanged; OpenRC tasks live in `tasks/openrc.yml`.
2. Install paths are role variables: `ai_ops_recovery_openrc_dir=/etc/init.d`, `ai_ops_recovery_sbin_dir=/usr/local/sbin`, `ai_ops_recovery_logrotate_dir=/etc/logrotate.d`, `ai_ops_recovery_cgroup_root=/sys/fs/cgroup`. File modes match production: 0755 init script, 0700 loop, 0644 logrotate.
3. **No `backup: true` in these directories.** OpenRC treats every file in `/etc/init.d` as a service, and logrotate reads every file in `/etc/logrotate.d`, where a backup copy would duplicate the log stanza. Git is the backup source.
4. Preconditions are read-only and run in check mode too: `cpu` and `memory` must be in `<cgroup root>/cgroup.subtree_control`. If `ansible_virtualization_type == 'lxc'`, `rc-update show boot` must list `cgroup-delegate`.
5. A changed init script or loop restarts the service through a handler; the loop then waits its 120 s boot delay again before the first cycle. A logrotate change needs no restart. After activation, outside check mode, the role reads the service cgroup and requires `memory.max=67108864` and `cpu.max=10000 100000`.
6. Validation uses `sh -n` on both scripts and `rc-service ai-gateway-recovery describe`.

## Risks / Trade-offs

- [A restart briefly pauses recovery for one boot-delay window] → Restart only when the init script or loop content changes.
- [Hosts without delegated controllers would lose the ceilings silently] → The precondition fails before any file is written.

## Migration Plan

Implement with a fixture test. Run `playbooks/recovery.yml --check --diff` against production; continue only if it shows no file differences and no service action. Then run one apply, which must report no changed files and no restart, and confirm that the service, cgroup ceilings and recovery log are unchanged. If a difference appears, stop and decide with the operator. Rollback is reverting the role; the host files are not modified by a no-diff apply.
