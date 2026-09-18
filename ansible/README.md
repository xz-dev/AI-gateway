# AI-gateway Ansible operations

Push-model deploy: edit local private config → review locally → upload + activate on remote.
Ansible never constructs configuration on the remote.

## Layout

- `inventory.example.ini` — copy to `inventory.local.ini` (gitignored); sets `ai_ops_config_dir`, `ai_ops_deploy_root`, `ai_ops_compose_project`, optional `ai_ops_compose_cmd` (default `docker compose`; use `podman compose` where applicable)
- `playbooks/deploy.yml` — component upgrade: push compose.yaml / compose.override.yaml / .env, then `up -d --no-deps --no-build <service>`
- `playbooks/deploy-squid.yml` — render local `data/egress-proxy/policy.json` with `scripts/render-egress-policy.py`, validate with pinned Squid image `-k parse`, upload rendered dir, restart egress-proxy
- `playbooks/deploy-aisix.yml` — validate local `aisix/resources.yaml`, report orphan direct-model candidates, upload, recreate AISIX only
- `playbooks/rollback.yml` — upload protected pre-deploy copies and reactivate; refuses if the remote changed independently after the deploy

## Drift gate

Every deploy-managed remote file is checksum-compared against the recorded baseline
(`.ai-ops-state/<file>.baseline`). A mismatch stops the deploy before any upload.
If the remote was edited by hand: review the shown diff, adopt the change into the
local file, and re-run.

## Usage

```bash
# plan (read-only: shows diff, no upload, no restart)
ansible-playbook playbooks/deploy.yml -e service=cli-proxy-api -e ai_ops_mode=plan

# apply
ansible-playbook playbooks/deploy.yml -e service=cli-proxy-api

# Squid policy
ansible-playbook playbooks/deploy-squid.yml -e ai_ops_mode=plan   # render + parse check only
ansible-playbook playbooks/deploy-squid.yml

# AISIX routes (orphan candidates are REPORTED only; delete locally then deploy)
ansible-playbook playbooks/deploy-aisix.yml -e adopted_direct_models='["direct/a","direct/b"]'

# rollback last deploy
ansible-playbook playbooks/rollback.yml -e service=cli-proxy-api
```

Secrets live only in untracked local files (`.env`); receipts under
`.ai-ops-state/` are sanitized (mode 0600, secret-bearing tasks use `no_log`).
