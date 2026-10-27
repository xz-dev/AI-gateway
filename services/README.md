# services

**Where:** every gateway component, one directory each, grouped by role. [`compose.yaml`](../compose.yaml) at the repository root wires them together.

**What:**

| Group | Components | Role |
|---|---|---|
| [`firewall/`](firewall/README.md) | `apisix/` (front door), `egress-proxy/`, `one-way-relay/` | Admit or deny traffic: public route allowlist, Internet egress allowlist, directed TCP edges |
| [`middleware/`](middleware/README.md) | `ai-sse-keepalive-proxy/`, `openai-api-cache-keeper/`, `responses-ws-sse-bridge/` | Request-path helpers between the front door and Sub2API |
| [`sub2api/`](sub2api/README.md) | Sub2API, PostgreSQL, Redis | Client API keys, entitlement and quota |
| [`routing/`](routing/README.md) | `apisix-models/`, `aisix/`, `aisix-status/`, `model-catalog/` | Model selection, logical-model fallback, model catalog |
| [`cpa/`](cpa/README.md) | `config/`, `cpa-model-sync/` | Provider credentials, pool execution, channel membership |

**How:** each component directory has a README that says where it runs, what it does and how to use it. Architecture: [docs/architecture.md](../docs/architecture.md). Tree: [docs/layout.md](../docs/layout.md).
