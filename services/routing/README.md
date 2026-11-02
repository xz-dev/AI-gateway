# routing

**Where:** `services/routing/`. All four components are internal: none is reachable from the Internet except through the front door.

**What:**

| Directory | Compose service | Role |
|---|---|---|
| [`apisix-models/`](apisix-models/README.md) | `apisix-models` | Constant pipe: `/v1/models` → model-catalog, everything else → AISIX |
| [`aisix/`](aisix/README.md) | `aisix` | Logical model IDs, ordered fallback and cooldown over concrete CPA targets; the single `*` row sends any other ID to CPA |
| [`aisix-status/`](aisix-status/README.md) | `aisix-status` | No-JavaScript status page for AISIX |
| [`model-catalog/`](model-catalog/README.md) | `model-catalog` | Codex model catalog and `/models-table` diagnostics |

**How:** read [model routing](../../docs/model-routing.md) before changing routes, the AISIX `*` row or Sub2API accounts, and [model catalog path](../../docs/catalog.md) for the catalog edges.
