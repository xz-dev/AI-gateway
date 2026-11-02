# AI-gateway Ansible operations

**Where:** `ops/ansible/`. One playbook per operation under `playbooks/`, run from the operator machine against the private inventory `private-config/inventories/production/` (gitignored; `inventories/example/` is the template). Private desired state lives in `private-config/`.

**What:** Push-model deploys. You edit the private config locally and review a `--check --diff` preview, then the playbook uploads and activates it on the remote host with guarded verification and rollback. Ansible never constructs configuration on the remote.

**How to use:**

```bash
cd ops/ansible
pip install -r requirements.txt && ansible-galaxy collection install -r requirements.yml -p collections
ansible-playbook playbooks/deploy.yml -e component=cli-proxy-api --check --diff
ansible-playbook playbooks/deploy.yml -e component=cli-proxy-api \
  -e ai_ops_component_approved_digest='<reviewed-plan-digest>'
ansible-playbook playbooks/deploy-config.yml -e component=egress-proxy      # egress policy
ansible-playbook playbooks/deploy-config.yml -e component=model-catalog     # model-catalog config.yaml (+ catalog.lua)
```

The model-catalog profile is `config.yaml` plus, when the private file exists (custom), `catalog.lua`; a custom script combined with `metadata.include_models` fails before upload, also under `--check`. Changes that couple the image, Compose and the profile use a reviewed manual window ([details](../../docs/ansible-operations.md#the-membership-script-cataloglua)).

Playbooks: `deploy`, `rollback`, `deploy-config`, `rollback-config`, `recovery`, `unlock`. Set variables in the private inventory's `group_vars/gateway.yml` or `host_vars/<host>.yml`; `inventories/example/group_vars/gateway.yml` lists every knob.

All playbooks, the migration table from `ops.yml`, variables, drift gates, guarded rollouts and recovery procedures: [docs/ansible-operations.md](../../docs/ansible-operations.md).
