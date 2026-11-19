# Sub2API

**Where:** Compose services `sub2api`, `postgres` and `redis` in [`compose.yaml`](../../compose.yaml). This directory has no build context: the image is pinned by `SUB2API_IMAGE` (default in `compose.yaml`, override in `.env`). Persistent state lives in the ignored `data/sub2api/`.

**What:** [Sub2API](https://github.com/Wei-Shaw/sub2api) is the only client API-key, entitlement and quota authority. It receives client traffic from the front chain and sends it to the inner `apisix-models` pipe through `sub2api-cpa-relay` and to AISIX through `sub2api-aisix-relay`. Its namespace has no default route; every edge is an explicit socat relay (see [one-way relay](../firewall/one-way-relay/README.md)).

**How:**

- Accounts, Base URL and caller keys: [upstream configuration](../../docs/upstream-configuration.md).
- Model routing through Sub2API: [model routing](../../docs/model-routing.md).
- Error passthrough: [Sub2API error passthrough](../../docs/sub2api-error-passthrough-configuration.md).
- Health probe and recovery: [gateway health and recovery](../../docs/gateway-health-and-recovery.md).

## Current release and carried patches

The runtime default is the compatibility-lock image `8f835a12151d…`, reviewed
source [`ede9e5ff12bd`](https://github.com/xz-dev/sub2api/commit/ede9e5ff12bdf186c80b523ad21d4a71ccda20bc)
(branch `fix/preserve-policy-errors-20261119`): the exact upstream tag v0.2.22,
upstream's GPT-6.2 Sol/Astra support (`6107cf1748`, cherry-picked), and the
request-scoped content-policy repair. Responses and Chat preserve recognized safe
policy code/type with a fixed explanation, before account retry or health penalties;
credential/endpoint 403 and dedicated cyber-policy handling keep their existing semantics.
It retains the reviewed rerank, affinity, structured-error and route/billing patches,
body-spool/accounting, inner-capacity-error passthrough and named errors on converted
Chat requests. The WebSocket keepalive patch (PR #3172) was dropped, and the Codex
passthrough "instructions required" 403 was removed along with upstream. The front
bridge still owns WebSocket termination; account WS modes remain off. Current
publication, mock validation and rollout status are in the
[policy repair record](../../docs/records/sub2api-policy-errors-2026-11-19.md).
Migrations 246–251 and the earlier image-only rollback rehearsal are in the
[2026-11-18 record](../../docs/records/component-update-2026-11-18.md).

Image-only rollback targets: `e26d4b7dbef5…` (source `f249326936c2`, GPT-6.2 support,
without the policy repair), then `8e329cb79d9c…` (source `2f0213fd2c57`, no GPT-6.2 support),
then `56557012dc75…` (source `02918ea957f0`, pre-v0.2.22 schema-compatible as rehearsed).

The earlier `60dbe85` / `fcf582…` v0.2.19 migration candidate was validated in an
isolated fixture, not established as the final production deployment. Its
[record](../../docs/records/sub2api-v0.2.19-2026-11-02.md) and unchecked rollout
tasks remain historical. See the [final deployment and limitations](../../docs/records/maintenance-capacity-closeout-2026-11-09.md).
Public image pins do not supply the private deployment's spool/ledger mounts,
resource overrides or enabled feature flags; template capacity enablement is pending.
