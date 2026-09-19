# AI-gateway Ansible operations

Push-model deploy: edit local private config → review locally → upload + activate on remote.
Ansible never constructs configuration on the remote.

## Layout

- `ops.yml` — the single entry playbook. Select the operation with `--tags` (see Usage).
- `inventory/hosts.ini` — gateway hosts.
- `inventory/group_vars/gateway.yml` / `inventory/host_vars/<host>.yml` — operator
  configuration (YAML). `examples/vars.yml` is the documented template to copy from.
- `roles/ai_ops/defaults/main.yml` — every operator-tunable variable, with default +
  comment. Override from inventory; never edit role files for local settings.
- `roles/ai_ops/tasks/` — `preflight` (compose detection, connectivity, state dir),
  `deploy-file` (drift gate + upload + activate), `rollback-file` (guarded restore).
- `private-config/` — **gitignored** operator's private config directory
  (`compose.yaml`, `compose.override.yaml`, `.env`, `aisix/resources.yaml`,
  `data/egress-proxy/policy.json`), adopted from production at baseline.

## Usage

```bash
# plan (read-only: shows diff, no upload, no restart)
ansible-playbook ops.yml --tags deploy -e service=cli-proxy-api -e ai_ops_mode=plan

# component deploy
ansible-playbook ops.yml --tags deploy -e service=cli-proxy-api

# Squid policy (local render + pinned `-k parse` validation, then upload + restart)
ansible-playbook ops.yml --tags deploy-squid -e ai_ops_mode=plan
ansible-playbook ops.yml --tags deploy-squid

# AISIX routes (orphan candidates are REPORTED only; delete locally then deploy)
ansible-playbook ops.yml --tags deploy-aisix -e ai_ops_adopted_direct_models='["direct/a"]'

# rollback last deploy of a component's file set
ansible-playbook ops.yml --tags rollback -e service=cli-proxy-api

# discover the operation surface
ansible-playbook ops.yml --list-tags
```

## Variables

Every operator-facing variable is declared with a default and a comment in
`roles/ai_ops/defaults/main.yml`, and summarized in `examples/vars.yml`. All use
the `ai_ops_` prefix. Set them in `inventory/group_vars/gateway.yml` (group-wide)
or `inventory/host_vars/<host>.yml` (per-host); inventory always wins over role
defaults. Key ones:

| Variable | Default | Meaning |
|---|---|---|
| `ai_ops_config_dir` | `ansible/private-config/` | private config source dir (gitignored) |
| `ai_ops_deploy_root` | `/root/AI-gateway` | remote deployment root |
| `ai_ops_compose_project` | `ai-gateway` | compose project name |
| `ai_ops_compose_cmd` | auto-detect | compose provider (`docker compose` / `podman compose` / `podman-compose`) |
| `ai_ops_mode` | `apply` | `plan` = read-only diff |
| `ai_ops_min_mem_mb` / `ai_ops_min_disk_mb` | `256` / `1024` | capacity gate thresholds |
| `ai_ops_squid_image` | `ai-gateway-squid:6.13-2-deb13u2` | pinned image for offline parse validation |
| `ai_ops_adopted_direct_models` / `ai_ops_standalone_retained` | `[]` | AISIX orphan-report approval/exclusion lists |

## Drift gate

Component deploys (`--tags deploy`, `deploy-aisix`) checksum each managed remote
file against the recorded baseline (`.ai-ops-state/<file>.baseline`); a mismatch
stops the deploy before any upload. Squid policy deploys (`--tags deploy-squid`)
upload one tarball + a `sha256sum` manifest over all generated files; the drift
gate verifies the whole remote tree against the last deployed manifest
(`sha256sum -c`), which keeps the same guarantee at ~2 SSH round trips instead of
per-file checks (177 policy files would cost ~900 connections). If the remote was
edited by hand: review the shown diff/failing files, adopt the change into the
local config, and re-run. Rollback likewise refuses to overwrite a remote file
that changed independently after the deploy.

## Operational notes

- **AISIX loads `resources.yaml` only at startup.** Because it is a bind mount, a
  file-only change does not alter the container config hash and plain
  `up -d` no-ops. `deploy-aisix` therefore activates with `--force-recreate`.
  Fixture tests miss this (fixture containers are freshly created); caught live
  on rainyun-la 2026-09-19.
- **The post-deploy Admin API route check is optional** (`failed_when: false`):
  the internal relay path currently returns 401, so effective-route verification
  is pending a working credential path. Until then, verify via
  `docker logs ai-gateway-aisix-1 | grep 'resources loaded'` and live traffic.

## Deliberate deviations from Ansible conventions

These are accepted design decisions, not defects (see the
`normalize-ansible-structure` change's design.md):

- **Hand-rolled `ai_ops_mode=plan|apply` instead of `--check`**: check-mode support
  across `command`/`shell`/`copy` is partial, and plan must show real remote diffs.
- **`command`/`shell` compose invocation instead of the `docker_compose_v2` module**:
  the drift gate already performs change detection; the module would add a
  collection dependency without changing guarantees.
- **Custom drift-gate/baseline/receipt state machine**: remote hand-edits are a real
  incident source for this deployment; Ansible has no native equivalent.

Secrets live only in gitignored local files (`private-config/`); receipts under
`.ai-ops-state/` are sanitized (mode 0600, secret-bearing tasks use `no_log`).
