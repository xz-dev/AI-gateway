# AI-gateway Ansible operations

**Where:** `ops/ansible/`. The playbook `ops.yml` runs from the operator machine against `inventory/hosts.ini`, and private desired state lives in the gitignored `private-config/`.

**What:** Push-model deploys. You edit the private config locally and review a read-only plan, then the playbook uploads and activates it on the remote host with guarded verification and rollback. Ansible never constructs configuration on the remote.

**How to use:**

```bash
cd ansible
ansible-playbook ops.yml --list-tags                                       # operation surface
ansible-playbook ops.yml --tags deploy -e service=cli-proxy-api -e ai_ops_mode=plan
ansible-playbook ops.yml --tags deploy -e service=cli-proxy-api \
  -e ai_ops_mode=apply -e ai_ops_front_approved_digest='<reviewed-plan-digest>'
ansible-playbook ops.yml --tags deploy-squid                               # egress policy
ansible-playbook ops.yml --tags deploy-models-enricher-config              # enricher config
```

Set variables in `inventory/group_vars/gateway.yml` or `inventory/host_vars/<host>.yml`. The template is `examples/vars.yml`.

All tags, variables, drift gates, guarded rollouts and recovery procedures: [docs/ansible-operations.md](../../docs/ansible-operations.md).
