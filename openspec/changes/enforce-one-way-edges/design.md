## Context

A Docker internal network is not directional. Separate source/target networks remove the direct target-to-initiator route; the relay listens only on its source interface. The target cannot initiate across that relay, while responses on established TCP connections remain bidirectional. The relay itself still shares a network with the initiator.

## Decisions

- **Reuse the existing relay shape.** Each AISIX edge copies `apisix-models-cpa-relay`: `alpine/socat` running as 65534, read-only, no capabilities, `no-new-privileges`, with a source network and a target network. New subnets are .60/.61 (Sub2API), .64/.65 (enricher), .70/.71 (apisix-models) and .62/.63 (AISIX→CPA). The source subnets and source-side addresses are unchanged, so `aisix:3000` still resolves to 172.30.60.3/64.3/70.3. Those addresses now belong to relays.
- **Bind relay, AISIX, APISIX and catalog-bridge listeners explicitly** instead of adding host firewall rules. This does not claim that every application listener is explicitly bound. This keeps the existing portability constraint: no host firewall and no engine-specific options. Verified with rootless Podman (published port to a socat bound to a specific IP) and with AISIX 1.6.0, the production digest.
- **AISIX** uses `proxy.listeners` (supported in both 1.5.0 and 1.6.0). `proxy.addr` stays because it is required, but it is ignored when `listeners` is set. Metrics move from the upstream default `0.0.0.0:9090` to `172.30.68.2:9090`. `aisix-status` already derives `<admin host>:9090`, so it needs no change. The pprof listener stays on its upstream default, loopback `127.0.0.1:9091`.
- **APISIX** pins `node_listen` to the relay-target addresses plus `127.0.0.1` (used by healthchecks and the `self` upstream), and sets `ssl.enable: false`. The offline `apisix test` gets `net.ipv4.ip_nonlocal_bind=1` because those addresses exist only in production networks. Validation renders `nginx.conf` and rejects any listener that is not loopback, a 172.30 :9080 address, or a unix socket.

## Residual trust boundary

A relay can reach the backend on its target network and listeners exposed by its source-side peer on their shared source network. Sub2API, the enricher and AI SSE middleware retain wildcard listeners inside isolated namespaces. The CPA host relay uses a fixed shell supervision loop; other relays use fixed socat commands. Compromise of a relay therefore retains access to both of its directly attached networks, not to an unrestricted bridge or the initiator's other networks.

## Rollout

Namespace-owner network membership changes for AISIX, Sub2API, CPA, the enricher and apisix-models. Per README Operations, this requires one coupled full-stack recreation, not per-service recreation. The old and new network definitions reuse subnets, so production stopped the coupled stack and removed the obsolete networks before starting the new topology, without deleting volumes. The provider-sidecar trio was included. [Deployment evidence](../../../docs/records/one-way-edges-deployment-2026-10-20.md) records the checks and limitations.
