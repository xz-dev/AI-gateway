## Why

A live all-pairs reachability audit (2026-10-20) found that the gateway's "one-way edge" model held only for socat relays that bind a source address:

- The four AISIX edges (`sub2api-aisix`, `aisix-cpa`, `enricher-aisix`, `apisix-models-aisix`) were direct two-member internal networks. A Docker network has no direction, so AISIX could open connections to Sub2API `:8080`, CPA `:8317`, the enricher `:8090` and apisix-models `:9080`. The last one would let AISIX send its own inference requests back through the selector. The exception came from the 2026-09-12 AISIX cutover (`46d2d01`, archived `add-aisix-model-routing` design) and was encoded in `safe-aisix-cutover` and `validate.sh`. No rationale was recorded for it.
- Some listeners bound every interface: AISIX proxy `0.0.0.0:3000`, AISIX metrics `0.0.0.0:9090` (upstream default), `aisix-status-relay` `:3001`, the five host-ingress relays, and the catalog bridge `:8080`. Every peer on the owning namespace's networks could reach them. That included all AISIX peers reaching the status page and metrics, and CPA reaching all six host-ingress ports of its own host relay.
- Both APISIX data planes opened the default TLS listener `0.0.0.0:9443`, which has no certificate and no consumer.

## What Changes

- Replace the four direct AISIX networks with ordinary hardened socat relays between disjoint `*-source`/`*-target` networks. Keep the `aisix:3000` and `cpa:8317` names through relay aliases and `extra_hosts`.
- Make every relay listener bind its own source-side address.
- Bind AISIX proxy only to its three relay-target addresses (`proxy.listeners`), and bind Admin and metrics only to `aisix-status-admin`.
- Bind both APISIX data planes only to their relay-target addresses plus loopback, and disable the default TLS listener. Bind the catalog bridge to its source address.
- Validation requires these shapes: `bind=` on every relay listener, the explicit AISIX listener set, and rendered APISIX listeners. It also drops the direct-edge exception. The socat boundary test also proves the target cannot reach the relay listener.
- Adopt production's existing `early_hints on;` in the front APISIX config. This is production drift, unrelated to isolation, so tracked config matches what is running.

## Impact

- Network topology changes for the AISIX, Sub2API, CPA, enricher and apisix-models namespace owners. Rollout requires the coupled full-stack recreation described in README Operations, and needs a separate operator-approved window.
- Each AISIX hop gains one socat relay (same per-hop cost as every other internal edge).
