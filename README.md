# AI Gateway

> [!IMPORTANT]
> You can use this project exactly as it is, but consider it merely a starting point—your imagination will surely enable you to create something even better than what I have built.
> This project is also my protest against the proliferation of AI gateways that lack any standardization.
>
> 这个项目你可以原样使用，也是抛砖引玉，你的想象力一定能创造比我更美好的东西。此外本项目是我对于 AI 网关满天飞但是却毫不规范的抗议。

A reusable Docker Compose stack that builds a self-owned AI gateway from separate modules: [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI), [Sub2API](https://github.com/Wei-Shaw/sub2api), [AISIX](https://github.com/api7/aisix), [Apache APISIX](https://apisix.apache.org/), [ai-sse-keepalive-proxy](https://github.com/xz-dev/ai-sse-keepalive-proxy), [openai-api-cache-keeper](https://github.com/xz-dev/openai-api-cache-keeper), [Squid](https://www.squid-cache.org/), [socat](http://www.dest-unreach.org/socat/) and [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/). Each module keeps one job: provider credentials, client API keys, logical-model routing, public routing and Internet ingress stay separate. Components are cleanly separated and no script hides the configuration, so an AI agent can deploy and change it while a human reviews it.

```mermaid
flowchart LR
  Client --> Cloudflare --> APISIX[APISIX front door]
  APISIX --> Keepalive[SSE keepalive] --> Keeper[cache keeper + WS/SSE bridge] --> Sub2API
  Sub2API --> Models[apisix-models] --> CPA[CLIProxyAPI]
  Models --> AISIX --> CPA
  APISIX -->|versioned /v1/models| Sidecar[model-catalog-sidecar] --> Models --> Enricher[models-enricher]
  Sync[cpa-model-sync] --> CPA
  CPA --> Squid[Squid egress allowlist] --> Providers
```

Full graph: [docs/architecture.md](docs/architecture.md).

> [!WARNING]
> Every intentionally public host port needs an external firewall or cloud security group. Tracked defaults bind host ports to `127.0.0.1`; never change a `*_BIND_ADDRESS` to `0.0.0.0`. See [docs/security.md](docs/security.md).

## Components

| Component | Where | What it does | How to use |
|---|---|---|---|
| Cloudflare Tunnel | service `cloudflared` | Internet ingress to the front APISIX | Public Hostname origin `http://apisix-ingress:9080` ([installation](docs/installation.md#cloudflare-tunnel)) |
| APISIX front door | `apisix/`, service `apisix`, `127.0.0.1:9080` | Public AI route allowlist, rate and body limits, opaque `404` | Routes in [`apisix/apisix.yaml`](apisix/apisix.yaml) ([public API](docs/public-api.md)) |
| AI SSE keepalive proxy | `middleware/ai-sse-keepalive-proxy`, operator entrance `127.0.0.1:8087` | Heartbeats for silent AI SSE streams | No configuration ([security](docs/security.md)) |
| Cache keeper + WS/SSE bridge | `middleware/openai-api-cache-keeper`, services `openai-api-cache-keeper`, `responses-ws-sse-bridge` | Responses history, background replay, WebSocket → HTTP/SSE | Pi Codex setup in [responses path](docs/responses-path.md) |
| Sub2API | services `sub2api`, `postgres`, `redis`; admin `127.0.0.1:8086` | The only client API-key, entitlement and quota authority | Accounts in [upstream configuration](docs/upstream-configuration.md) |
| apisix-models | `apisix-models/`, `127.0.0.1:9081` | Inner selector: IDs with `/` → CPA, others → AISIX; catalog compatibility | [Model routing](docs/model-routing.md) |
| AISIX + status page | `aisix/`, `aisix-status/`; `127.0.0.1:3001/status` | Logical model IDs, ordered fallback, cooldown; no-JS status page | Private `aisix/resources.yaml` ([upstream configuration](docs/upstream-configuration.md)) |
| CLIProxyAPI (CPA) | `cpa/`, service `cli-proxy-api`, `127.0.0.1:8317` | Provider credentials and pool execution | Management UI with `CPA_MANAGEMENT_KEY` ([upstream configuration](docs/upstream-configuration.md)) |
| cpa-model-sync | `cpa-model-sync/` | Syncs model membership of five CPA API-key channel kinds | [cpa-model-sync/README.md](cpa-model-sync/README.md) |
| models-enricher | `models-enricher/` | Codex model catalog with metadata from a shared source chain | [models-enricher/README.md](models-enricher/README.md) |
| model-catalog-sidecar | `model-catalog-sidecar/`, `127.0.0.1:9083` / Tailscale | Catalog bridge; `/models-table` diagnostics and refresh | [Catalog path](docs/catalog.md) |
| Egress proxy | `egress-proxy/`, service `egress-proxy` | Fail-closed Squid allowlist; the only Internet egress | Edit `data/egress-proxy/policy.json` ([egress policy](docs/egress-policy.md)) |
| provider-sidecar (optional) | private `compose.override.yaml` | Generic OpenAI-compatible sidecar behind TLS relay and tunnel | [Provider sidecar](docs/provider-sidecar.md) |
| Ansible ops | `ansible/` | Guarded plan/apply deploys and rollbacks to the host | [ansible/README.md](ansible/README.md) |

## Quick start

Requirements: Linux with `/dev/net/tun`, Docker Engine with Compose v2.33.1+ or rootless Podman with a Compose provider, OpenSSL, and a remotely managed Cloudflare Tunnel.

```bash
git clone --recurse-submodules https://github.com/xz-dev/AI-gateway.git "$HOME/AI-gateway"
cd "$HOME/AI-gateway"
./scripts/init.sh                 # private .env, CPA and AISIX files (never overwrites)
# edit .env (ADMIN_EMAIL, CLOUDFLARED_TUNNEL_TOKEN) and aisix/resources.yaml
./scripts/init-egress-proxy.sh    # render data/egress-proxy/policy.json
./scripts/validate.sh
source ./scripts/container-runtime.sh
"${AI_GATEWAY_COMPOSE[@]}" up -d --build
```

Full steps: [docs/installation.md](docs/installation.md). Day-2 operations: [docs/operations.md](docs/operations.md).

## Documentation

Guides and reference:

- [Architecture](docs/architecture.md) — full component graph
- [Installation](docs/installation.md) — first install and Cloudflare Tunnel
- [Security model](docs/security.md) — boundaries, one-way edges, hardening
- [Egress policy](docs/egress-policy.md) — Squid allowlist rules
- [Upstream configuration](docs/upstream-configuration.md) — CPA, AISIX and Sub2API wiring
- [Responses path](docs/responses-path.md) — Pi Codex transports and cache keeper
- [WebSocket termination](docs/websocket-termination.md) — topology decision and limits
- [Model routing](docs/model-routing.md) — inner selector and deploy checklist
- [Provider sidecar](docs/provider-sidecar.md) — optional sidecar role
- [Public API behavior](docs/public-api.md) — routes and opaque responses
- [Operations and validation](docs/operations.md) — coupled stack, state, `validate.sh`
- [Gateway health and recovery](docs/gateway-health-and-recovery.md) — readiness and bounded recovery
- [Ansible operations](docs/ansible-operations.md) — playbook tags, variables, guarded rollouts
- [models-enricher](docs/models-enricher.md) — catalog enricher reference
- [Model catalog path](docs/catalog.md) — front-door intersection and models table
- [Models table scheduled refresh](docs/models-table-scheduled-refresh.md)
- [Add an OpenAI-compatible provider](docs/add-openai-compatible-provider.md)
- [Sub2API error passthrough](docs/sub2api-error-passthrough-configuration.md)
- [Sub2API Codex manifest defects](docs/sub2api-codex-manifest-implementation-defects.md)
- [Repository layout](docs/layout.md)

Verification evidence: [catalog identity](docs/catalog-identity-verification.md), [layered metadata](docs/layered-metadata-verification.md) ([deployment](docs/layered-metadata-deployment.md)), [output-token fields](docs/output-token-field-semantics-verification.md), [models-table SSR](docs/models-table-ssr-verification.md), [Go enricher read cache (historical)](docs/enricher-read-cache-verification.md), [Go provider prefix (historical)](docs/enricher-provider-prefix.md), [catalog round closeout](docs/catalog-round-closeout.md).

Dated deployment records (point-in-time, not current state): [2026-09-07 CPA](docs/cpa-v7.2.153-deployment-2026-09-07.md), [2026-09-07 read cache](docs/enricher-read-cache-deployment-2026-09-07.md), [2026-09-08 bare chain](docs/enricher-bare-chain-deployment-2026-09-08.md), [2026-09-08 positive admission](docs/catalog-positive-admission-deployment-2026-09-08.md), [2026-09-08 provider prefix](docs/enricher-provider-prefix-deployment-2026-09-08.md), [2026-09-08 models-table SSR](docs/models-table-ssr-deployment-2026-09-08.md), [2026-09-08 output-token fields](docs/output-token-field-deployment-2026-09-08.md), [2026-09-08 source chain](docs/source-chain-deployment-2026-09-08.md), [2026-09-15 aihub](docs/aihub-provider-deployment-2026-09-15.md), [2026-09-16 force refresh](docs/models-table-force-refresh-deployment-2026-09-16.md), [2026-09-30 tunnel 103](docs/cloudflare-tunnel-103-diagnosis-2026-09-30.md), [2026-09-30 keepalive late error](docs/keepalive-late-error-deployment-2026-09-30.md), [2026-10-01 header wait](docs/keepalive-header-wait-10s-deployment-2026-10-01.md), [2026-10-09 upgrades](docs/component-upgrade-2026-10-09.md) ([round 2](docs/component-upgrade-2026-10-09-round2.md)), [2026-10-09 memory/swap](docs/memory-swap-2026-10-09.md), [2026-10-11 upgrades](docs/component-upgrade-2026-10-11.md), [2026-10-12 CPA](docs/cpa-upgrade-2026-10-12.md), [2026-10-15 upgrades](docs/component-upgrade-2026-10-15.md), [2026-10-20 one-way edges](docs/one-way-edges-deployment-2026-10-20.md), [2026-10-23 batch](docs/deployment-batch-20261023.md).

## License

This deployment template is licensed under the [MIT License](LICENSE). Container images and upstream applications retain their own licenses.
