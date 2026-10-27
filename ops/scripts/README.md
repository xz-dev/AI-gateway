# ops/scripts

**Where:** `ops/scripts/`, run from the repository root.

**What:**

| Path | Contents |
|---|---|
| `init.sh`, `init-egress-proxy.sh`, `init-provider-sidecar-*.sh` | Create private runtime files (`.env`, CPA/AISIX config, egress policy, sidecar TLS); never overwrite |
| `validate.sh` | Static and runtime checks of the Compose stack, network rules and Lua tests (CI runs it) |
| `render-egress-policy.py`, `check-*.py` | Squid policy renderer and policy/affinity checks |
| `container-runtime.sh` | Selects Docker or rootless Podman; exports `AI_GATEWAY_COMPOSE` |
| [`host/`](host/) | Tooling installed on the deploy root: `gateway-recovery.py` with its systemd units, deploy helpers used by Ansible |
| [`tests/`](tests/) | `test-*` suites for relays, namespaces, Ansible roles, recovery and catalog routes |

**How:**

```bash
./ops/scripts/validate.sh .env.example   # tracked template
./ops/scripts/validate.sh                # private runtime config
source ./ops/scripts/container-runtime.sh && "${AI_GATEWAY_COMPOSE[@]}" ps
```

Details: [operations and validation](../../docs/operations.md), [gateway health and recovery](../../docs/gateway-health-and-recovery.md).
