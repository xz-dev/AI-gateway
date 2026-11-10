# Sub2API

**Where:** Compose services `sub2api`, `postgres` and `redis` in [`compose.yaml`](../../compose.yaml). This directory has no build context: the image is pinned by `SUB2API_IMAGE` (default in `compose.yaml`, override in `.env`). Persistent state lives in the ignored `data/sub2api/`.

**What:** [Sub2API](https://github.com/Wei-Shaw/sub2api) is the only client API-key, entitlement and quota authority. It receives client traffic from the front chain and sends it to the inner `apisix-models` pipe through `sub2api-cpa-relay` and to AISIX through `sub2api-aisix-relay`. Its namespace has no default route; every edge is an explicit socat relay (see [one-way relay](../firewall/one-way-relay/README.md)).

**How:**

- Accounts, Base URL and caller keys: [upstream configuration](../../docs/upstream-configuration.md).
- Model routing through Sub2API: [model routing](../../docs/model-routing.md).
- Error passthrough: [Sub2API error passthrough](../../docs/sub2api-error-passthrough-configuration.md).
- Health probe and recovery: [gateway health and recovery](../../docs/gateway-health-and-recovery.md).

## Current release and carried patches

The runtime default is the compatibility-lock image `56557012dc75…`, reviewed
source [`02918ea957f0`](https://github.com/xz-dev/sub2api/commit/02918ea957f0639db932b23e7e5d00a6f2298308),
based on upstream v0.2.19. It retains the reviewed rerank, affinity, structured-error
and route/billing patches, adds body-spool/accounting and inner-capacity-error
passthrough, and preserves named errors on converted Chat requests. The front
bridge still owns WebSocket termination; account WS modes remain off.

The earlier `60dbe85` / `fcf582…` v0.2.19 migration candidate was validated in an
isolated fixture, not established as the final production deployment. Its
[record](../../docs/records/sub2api-v0.2.19-2026-11-02.md) and unchecked rollout
tasks remain historical. See the [final deployment and limitations](../../docs/records/maintenance-capacity-closeout-2026-11-09.md).
Public image pins do not supply the private deployment's spool/ledger mounts,
resource overrides or enabled feature flags; template capacity enablement is pending.
