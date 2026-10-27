# Sub2API

**Where:** Compose services `sub2api`, `postgres` and `redis` in [`compose.yaml`](../../compose.yaml). This directory has no build context: the image is pinned by `SUB2API_IMAGE` (default in `compose.yaml`, override in `.env`). Persistent state lives in the ignored `data/sub2api/`.

**What:** [Sub2API](https://github.com/Wei-Shaw/sub2api) is the only client API-key, entitlement and quota authority. It receives client traffic from the front chain and sends it to the inner model selector through `sub2api-cpa-relay` and `sub2api-aisix-relay`. Its namespace has no default route; every edge is an explicit socat relay (see [one-way relay](../firewall/one-way-relay/README.md)).

**How:**

- Accounts, Base URL and caller keys: [upstream configuration](../../docs/upstream-configuration.md).
- Model routing through Sub2API: [model routing](../../docs/model-routing.md).
- Error passthrough: [Sub2API error passthrough](../../docs/sub2api-error-passthrough-configuration.md).
- Health probe and recovery: [gateway health and recovery](../../docs/gateway-health-and-recovery.md).
