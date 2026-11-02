# firewall

**Where:** `services/firewall/`. These components decide which traffic may cross a boundary.

**What:**

| Directory | Compose services | Role |
|---|---|---|
| [`apisix/`](apisix/README.md) | `apisix` (+ `apisix-netns`, host and ingress relays) | Front door: public route allowlist, limits, opaque 404, catalog intersection, `9083` models table |
| [`egress-proxy/`](egress-proxy/README.md) | `egress-proxy` | Squid + Unbound: the only Internet egress, fail-closed allowlist |
| [`one-way-relay/`](one-way-relay/README.md) | every `*-relay` and `*-netns` | Directed TCP edges and route-stripped namespaces |

**How:** [security model](../../docs/security.md), [egress policy](../../docs/egress-policy.md), [public API behavior](../../docs/public-api.md).
