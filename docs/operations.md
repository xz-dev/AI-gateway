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

## Capacity ledger and spool safety

Use the [final deployment receipt](records/maintenance-capacity-closeout-2026-11-09.md)
as release/resource history, not a private desired-state template. Runtime
spool destinations are keeper's store under `/var/tmp/openai-api-cache-keeper`,
bridge/Sub2API `/spool`, AISIX `/var/lib/aisix-spool`, CPA `/var/lib/cpa-spool`;
the shared ledger is `/run/capacity-ledger`. Resolve actual host backing paths
from the approved deployment, never from copied examples. Spools use disk-only
mode-0700 directories, anonymous mode-0600 files and no copy-up; they can hold
sensitive payload data while open. Never back them up as request archives.
The mounted keeper cache (including any hidden legacy owner) is separate
protected state, not an obsolete spool to remove.

For a leaked capacity reservation:
1. Read the live-ticket metric (count, age, size and inode). No TTL authorizes
   deleting it. Record current floors and existing traffic without new load.
2. Match the ticket inode in host `lslocks` or `/proc/locks`, then map the holder
   PID to its container. A PID can appear as 0 inside keeper across namespaces;
   that is not proof that the lease has no owner.
3. If a holder is stuck, obtain a scoped recovery approval and restart **only
   that hop**, preserving its namespace owner, resources, image and mounts.
4. Let keeper release the ticket after all spool leases close. Verify idle
   ledger and floors. **Never unlink a ticket while any lock is held**, edit
   `ledger.lock`, sweep cache/spool directories, or invent expiry/GC credit.

The host admission gate reserves overlapping anonymous+kernel charges against
MemAvailable, with 200 MiB floor and 32 MiB margin; it does not add available
dirty/writeback pages again. CI's conservative working-set metric separately
includes dirty/writeback and kernel pages. Disk keeps a 1 GiB reserve plus
store headroom. A healthy service may refuse 245 MB admission; do not lower
floors to force success. Production Sub2API is 512 MiB with 320 MiB actual
swap, not the PR ladder's 320 MiB/no-swap configuration. Rollback must preserve
the reviewed current resource/configuration snapshot, not an old 320 MiB one.

The tracked template's capacity mounts/flags/validator remain pending. Never
apply it over private production desired state simply because the image pins
match the lock. Existing volume ownership and `nocopy` migration need a reviewed
window; changing an image alone is not that migration. Keeper defaults to its
published immutable image. Development builds must use an explicit separate
local tag, never the published digest. Do not rerun the legacy AISIX publisher
against an existing immutable version.
