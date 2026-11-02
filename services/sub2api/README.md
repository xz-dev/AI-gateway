# Sub2API

**Where:** Compose services `sub2api`, `postgres` and `redis` in [`compose.yaml`](../../compose.yaml). This directory has no build context: the image is pinned by `SUB2API_IMAGE` (default in `compose.yaml`, override in `.env`). Persistent state lives in the ignored `data/sub2api/`.

**What:** [Sub2API](https://github.com/Wei-Shaw/sub2api) is the only client API-key, entitlement and quota authority. It receives client traffic from the front chain and sends it to the inner `apisix-models` pipe through `sub2api-cpa-relay` and to AISIX through `sub2api-aisix-relay`. Its namespace has no default route; every edge is an explicit socat relay (see [one-way relay](../firewall/one-way-relay/README.md)).

**Pinned build:** v0.2.19, upstream `dece26153621959ccd7635814f4b2d08304e681a`, owned source `60dbe853f694b5ecb37b0672a98a7d2abf8bf72b` (linux/amd64). The eight retained commits provide rerank, explicit cross-protocol session affinity and structured error fields. Only the two obsolete Responses WS keepalive commits were dropped: keeper and the front WS→HTTP bridge still own that transport. See the [maintenance receipt](../../docs/records/sub2api-v0.2.19-2026-11-02.md) for image, migration and acceptance evidence.

**How:**

- Accounts, Base URL and caller keys: [upstream configuration](../../docs/upstream-configuration.md).
- Model routing through Sub2API: [model routing](../../docs/model-routing.md).
- Error passthrough: [Sub2API error passthrough](../../docs/sub2api-error-passthrough-configuration.md).
- Health probe and recovery: [gateway health and recovery](../../docs/gateway-health-and-recovery.md).
