# One-way edges: production deployment, 2026-10-20

## Outcome and scope

The operator explicitly approved deploying the new architecture first, checking it, then pushing the deployment record. Port 9083 was included in the same rollout, not deployed separately.

The controlled cutover ran from **03:49:26 to 03:52:11 UTC**. This is the operation window, not an exact measurement of client-visible downtime. Verification at **04:09:01 UTC** found:

- 53 running containers, including the four new AISIX relays; all 12 configured healthchecks healthy.
- Zero container restarts and zero `OOMKilled` states since recreation.
- All 49 existing services recreated with their **original image IDs**. No build, application upgrade, database migration or volume deletion.
- The existing `ai-gateway_response-keeper-cache` volume retained.
- Four obsolete direct AISIX networks removed, replaced with eight source/target networks.
- AISIX proxy/Admin/metrics, APISIX listeners and host port 9083 using the new bindings.

`models-enricher/identity.go`, its tests and other agents' working-tree changes were not edited, staged or deployed. The live enricher image remains `sha256:c98ad4daa2687cb84c7ce602bc120b554f9fdf3460217eb0691dfbd215a5eb04`.

## Activation and recovery

Snapshot:

```text
/root/AI-gateway/.rollback/one-way-edges-20261020T034458Z/
```

It contains the original Compose files, `.env`, AISIX config, both APISIX configs, catalog nginx template, `.ai-ops-state` baseline tree, container IDs and namespace/network graph, volume mapping, desired render, checksums and private command logs. It is root-only and contains secrets; do not publish the raw files.

The old and new network definitions reuse subnets. A coupled `docker compose down --remove-orphans --timeout 45` first released the old networks, without `--volumes`. The new files were then activated with:

```sh
docker compose -p ai-gateway \
  --project-directory /root/AI-gateway \
  -f /root/AI-gateway/compose.yaml \
  -f /root/AI-gateway/compose.override.yaml \
  --env-file /root/AI-gateway/.env \
  up -d --no-build --pull never --force-recreate --remove-orphans \
  --wait --wait-timeout 300
```

This included the provider-sidecar, tunnel and relay services. The apply script would restore the snapshot and recreate the coupled old stack if activation or its initial health gate failed. That recovery branch was **not needed or exercised**.

A self-contained manual restore is retained in the snapshot:

```sh
python3 /root/AI-gateway/.rollback/one-way-edges-20261020T034458Z/rollback.py --restore
```

It refuses to overwrite later configuration drift, restores the old files and coupled topology without deleting volumes, and uses `.env.recreate` for the exact original image bytes. Python syntax was checked; no destructive rollback rehearsal was performed on production. A later independent configuration change requires reconciliation before this point-in-time restore can run.

After successful verification, only the seven verified deployment files were adopted into `.ai-ops-state`; the previous baseline tree remains in the snapshot.

### Existing imported-image references

Preflight found four Docker Hub-style digest references that the local Docker store could not resolve, despite the corresponding running image IDs being present under existing `rollback-20261015/*` repository digests. Their `.env` selectors were changed to those locally resolvable references, retaining explicit version labels and identical image IDs for socat, cloudflared, PostgreSQL and Redis. The private local `.env` mirror was reconciled as well.

No registry pull or image substitution occurred. These imported references are **host-local recovery state**, not portable registry pins; a new host needs the saved images or valid registry manifest references. The original and normalized environment files are both retained in the snapshot.

## Live isolation checks

### Listeners and namespace ownership

- AISIX proxy: `172.30.61.2:3000`, `172.30.65.2:3000`, `172.30.71.2:3000`.
- AISIX Admin and metrics: `172.30.68.2:3002` and `172.30.68.2:9090`; pprof remains loopback-only.
- AISIX status relay: `172.30.66.2:3001`, not a wildcard listener in AISIX's namespace.
- Front APISIX: loopback plus `172.30.6.2`/`172.30.8.2:9080`; models APISIX: loopback plus `172.30.12.2`/`172.30.34.2`/`172.30.42.2:9080`.
- Neither APISIX has a 9443 listener. Their auxiliary control listeners remain loopback-only.
- Host 9083: exactly `127.0.0.1` and `100.94.238.35`; Docker PortBindings and live host sockets agree. No IPv4/IPv6 wildcard publication.
- Keeper shares the models APISIX namespace, with application/control sockets on loopback 8085/9091, no publication, UID 65534, read-only root, dropped capabilities and no-new-privileges.
- The seven application namespace owners have no default route. The four new relay containers retain their configured non-root/read-only/capability restrictions.

### Reachability matrix and TUN exception

The socket inventory covered 38 namespaces and 161 per-interface endpoints. For sources without TUN interception, **5,798 TCP checks** yielded **59 allowed connections, 5,739 blocked connections and zero expectation mismatches**. Endpoints included unused interfaces, control ports and disabled APISIX 9443, not only known positive sockets.

The initial raw scan also made 159 provider-sidecar/TUN-source checks. The TUN can accept a local TCP handshake before attempting its proxy path, producing 158 apparent mismatches. Those handshake results are **inconclusive**, not proof of access and not counted as passing isolation checks.

Three protected destinations were checked separately: AISIX Admin, Sub2API and the models namespace's keeper port. In each case:

1. The TUN accepted the local connection but returned no application bytes.
2. Packet capture in the actual destination namespace saw **zero test SYN packets**.
3. An authorized-source positive control produced **one captured SYN** and a real HTTP response.

This verifies those three destination boundaries; it does not claim an exhaustive application-layer matrix for every TUN destination. No inference payloads were used.

A relay itself still has access to its two attached networks, including listeners exposed by its source-side peer. Some source applications retain wildcard listeners. This is not an absolute isolation guarantee against a compromised relay, nor does it make established TCP traffic unidirectional.

## Functional checks

Twelve explicit HTTP checks passed:

- `/status` and `/models-table` returned 200 over both loopback and Tailscale bindings.
- The status renderer could read AISIX metrics at the new private address.
- The enricher and Sub2API namespaces reached the AISIX catalog through their respective relays: authenticated 200 with 92 catalog entries. An unauthenticated request was correctly rejected with 401.
- Both `/v1/responses` and `/rk/v1/responses` reached keeper and returned its expected unknown-model 404, without making backend inference calls.
- Keeper and models APISIX health endpoints returned 200.

Protocol-level checks from the Sub2API namespace also passed through its actual database relays: PostgreSQL SSLRequest returned `N` in 29.7 ms (server accepts this protocol exchange but does not negotiate TLS), and Redis returned `+PONG` in 4.2 ms. These are connectivity checks, not a new claim of database transport encryption.

Observed natural traffic from activation through 04:09 UTC:

| Layer | Observation |
| --- | --- |
| Front APISIX | 127 Responses HTTP 200 and 7 WebSocket upgrades (101) |
| Models APISIX | 188 Responses HTTP 200 and 48 WebSocket upgrades (101), plus four intentional unknown-model 404 probes |
| Responses traversal | All 240 logged upstream addresses were `127.0.0.1:8085`; zero upstream addresses were `127.0.0.1:9080` |
| AISIX | 189 completed Responses requests with HTTP 200 |

These are layer-local access-log counts, not unique-request totals or a WebSocket success ratio. A 101 proves an upgrade, not the completion of a conversation.

The upstream check parsed the `upstream_addr` column. A whole-line grep for `127.0.0.1:9080` is insufficient: a direct local probe legitimately puts that value in the request Host/URL while still using keeper on 8085. The existing broad grep in the scoped Ansible verifier was not changed during this code-frozen rollout; record this false-positive risk before using it with local HTTP/1.1 probe logs.

## Remaining observations and limits

- Another session observed slow administration responses: the account list eventually returned complete HTTP 200 with 50,664 bytes but took 23.343 seconds client-side. Other list endpoints also took seconds. A separate recent server-side account-list record was 200 / 33 ms; these were not correlated as the same request, so the delay's location is not established. PostgreSQL had no observed lock wait and Redis/database relay checks passed. Functional recovery does **not** close the control-plane latency issue.
- Existing client WebSocket ping timeouts still appeared. They are not counted as fixed by this topology change. No extra production inference or stress probes were added by this rollout.
- The original 24-hour observation, WebSocket continuation/full-mode assessment and valid 0.5-CPU performance acceptance remain open. Zero OOM/restarts during this short window is not a long-term stability result.
- Host socket and Docker publication inspection prove the 9083 binding change. The previous public-IP self-probe did not establish external firewall behavior; no new Internet-side reachability claim is made.

Private evidence files include `network-verification.json`, `network-summary.json`, `tun-verification.json`, `http-verification.json`, `database-paths.json` and `final-verification.json` under the snapshot directory.
