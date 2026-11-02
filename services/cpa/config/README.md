# CPA config sample

**Where:** `services/cpa/config/config.example.yaml`. No container mounts this directory.

**What:** the public CLIProxyAPI configuration template: loopback-only listener, remote management with a generated key, `proxy-url` pointed at the egress relay, and pprof on a namespace-local listener.

**How:** `./ops/scripts/init.sh` renders it into the ignored live file `data/cpa/conf/config.yaml` with generated secrets and never overwrites an existing file. After that, the live file belongs to CPA and cpa-model-sync; do not treat this sample as desired state. See [installation](../../../docs/installation.md) and [Ansible operations](../../../docs/ansible-operations.md#worker-owned-cpa-configuration).
