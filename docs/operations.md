# Operations and validation

Coupled-stack operations, persistent state and the validation script.

## Operations

Sub2API, AISIX, CLIProxyAPI, model-catalog, AI SSE keepalive proxy, their namespace owners, and adjacent relays are one operational unit. Restart propagation is not reliable across shared namespaces and relay chains. Never restart or recreate a namespace owner alone, or blindly restart dependent applications together. A proven application-only image replacement may use Compose `up -d --no-deps --no-build <service>` while its namespace owner remains unchanged; broader network or CPA changes require the coupled stack. With the production override, include the provider-sidecar tunnel and both provider-sidecar relays in the same full-stack operation.

```bash
source ./ops/scripts/container-runtime.sh

# Follow all stack and relay logs
"${AI_GATEWAY_COMPOSE[@]}" logs -f

# Pull the fixed AISIX artifacts, then apply a coupled recreation
"${AI_GATEWAY_COMPOSE[@]}" pull aisix aisix-status
"${AI_GATEWAY_COMPOSE[@]}" up -d --build --force-recreate

# Stop and restart the complete stack without deleting bind-mounted data
"${AI_GATEWAY_COMPOSE[@]}" down
"${AI_GATEWAY_COMPOSE[@]}" up -d --build
"${AI_GATEWAY_COMPOSE[@]}" ps
```

Use explicit endpoint/readiness probes after recreation. Do not make correctness depend on engine healthcheck scheduling.

[Health and bounded recovery](gateway-health-and-recovery.md) separates local liveness from dependency readiness, documents the seven-application host recovery timer and its restart budgets, and documents full-stack host observations plus the remaining native Docker startup-probe gap. Application-only recovery preserves container image/configuration/volumes and its namespace owner; it is not permission to restart the coupled network stack.

Persistent application state lives under ignored `data/`, including the egress CA/policy, CPA config/auth/logs/plugins/runtime SQLite, and Sub2API PostgreSQL/Redis/application data. CPA and cpa-model-sync own `data/cpa/conf/config.yaml`: only CPA component plans check that live YAML parses as a mapping, never equality with a private seed or historical baseline. Other component plans do not gate on this file. Deploy/rollback must not overwrite it. See [worker-owned CPA configuration](ansible-operations.md#worker-owned-cpa-configuration) for missing-file bootstrap. Back it up before upgrades. Never commit `.env` or `data/`. Losing the egress CA breaks trust for bumped destinations; never rotate it as part of a routine redeploy.

## Validation

```bash
./ops/scripts/validate.sh              # private runtime config after init
./ops/scripts/validate.sh .env.example # tracked template only
```

Validation keeps three durable security contracts. First, the tracked fresh-install policy is the exact control-plane baseline documented in [Egress policy](egress-policy.md): missing, extra, or broadened GitHub rules fail offline validation. Second, egress is fail-closed: application namespaces have no direct route, Squid is sole provider egress, filtered DNS blocks private/reserved and rebinding answers, and policy tests distinguish domain, SNI/Host, method, and path allowlists. Third, every edge is a disjoint one-way relay pair: validation requires each relay listener to bind its own source address, rejects wildcard AISIX proxy/Admin/metrics listeners, and renders both APISIX configurations to reject any wildcard or TLS listener. Compose rendering, local image builds, APISIX/Squid syntax, explicit non-floating version tags, and untracked-secret checks are lightweight scaffold gates, not snapshots of service counts, fixed addresses, or application policy values. Like Compose startup, `validate.sh` automatically includes repo-root `compose.override.yaml` when present; `AI_GATEWAY_COMPOSE_OVERRIDE` selects a different explicit override.
