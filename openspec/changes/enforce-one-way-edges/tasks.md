## 1. Repository

- [x] 1.1 Replace the four direct AISIX networks with `*-aisix-relay` source/target pairs (public and private compose), preserving `aisix`/`cpa` names
- [x] 1.2 Add `bind=` to every host-ingress relay and `aisix-status-relay`
- [x] 1.3 AISIX `proxy.listeners` on 172.30.61.2/65.2/71.2:3000, metrics on 172.30.68.2:9090 (example and private config); verified with AISIX 1.6.0 that no wildcard listener remains
- [x] 1.4 APISIX `node_listen` pinned to relay targets plus loopback and `ssl.enable: false` for both data planes; catalog bridge listens on 172.30.41.3
- [x] 1.5 `validate.sh`: remove direct-edge exception; require relay `bind=`, explicit AISIX listener set and rendered APISIX listeners; negative cases rejected (no-bind relay, wildcard AISIX, TLS/wildcard APISIX)
- [x] 1.6 `test-socat-boundary.sh`: target cannot reach the relay listener
- [x] 1.7 README security model and validation contract updated
- [x] 1.8 Replace private `0.0.0.0:9083:9080` publication with loopback plus the explicit `TAILSCALE_IP`; validate effective short/long-form bindings and reject wildcard, omitted-host and public-IP variants

## 2. Production (separately approved window)

Operator decision (2026-10-20): deploy AISIX and 9083 together, verify, then push the deployment record; leave other agents' code untouched. Activation completed at 03:52 UTC; bounded verification completed at 04:09 UTC. See [deployment evidence and remaining limits](../../../docs/records/one-way-edges-deployment-2026-10-20.md).

- [x] 2.1 Snapshot compose, override, `.env`, `aisix/config.yaml`, both APISIX configs and the catalog template into `.rollback/one-way-edges-20261020T034458Z/`, and record container/network/volume maps and old baselines
- [x] 2.2 Upload files and recreate the coupled stack, including the provider-sidecar trio, without builds or volume deletion; remove the four obsolete networks before `up` because their subnets are reused
- [x] 2.3 Verify: 53 running, all 12 configured healthchecks healthy, 0 restarts/OOM; 5,798 ordinary-namespace socket checks match; three TUN-source destination packet checks pass; 240 Responses log entries target keeper and none loop back; authenticated Sub2API→AISIX catalog request succeeds; status and metrics work; 9083 binds only loopback and the chosen Tailscale IP. Control-plane latency and long-term stability remain open, not certified by this gate
- [x] 2.4 Prepare automatic failure recovery and a drift-guarded manual snapshot restore with coupled recreation; syntax-check the restore script. No failure required rollback, so its destructive branch was not exercised
