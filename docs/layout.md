# Repository layout

Top-level directories and what they contain.

```text
.
├── apisix/
│   ├── apisix.yaml       # standalone routes and public response policy
│   ├── config.yaml       # APISIX data-plane configuration
│   └── lua/              # custom APISIX modules
├── aisix/                # pinned AISIX CI build, patch, public examples, private ignored runtime files
├── aisix-status/         # independent no-JavaScript server-rendered status page
├── apisix-models/        # retained catalog/protocol compatibility APISIX instance
│   ├── apisix.yaml       # models route split + WS/SSE catch-all
│   └── config.yaml       # standalone data-plane configuration
├── models-enricher/      # TypeScript Codex manifest enricher sidecar (Node LTS)
│   ├── Dockerfile
│   ├── config.yaml       # example metadata chains, include_models, static models
│   ├── src/              # server, build pipeline, caches
│   └── vendor/openai-api-pi-extension/ # pinned submodule: shared metadata compile core
├── model-catalog-sidecar/ # nginx single-path bridge: front APISIX → apisix-models (injects own CPA key)
├── cpa/
│   └── config.example.yaml
├── egress-proxy/
│   ├── Dockerfile
│   ├── unbound.conf         # filtered resolver used only by Squid
│   ├── policy.example.json # copied as a control-plane-only runtime policy
│   └── testdata/
├── data/                   # ignored persistent runtime state
│   ├── cpa/
│   ├── egress-proxy/
│   └── sub2api/
├── middleware/
│   ├── ai-sse-keepalive-proxy/ # pinned submodule and CI image
│   └── openai-api-cache-keeper/ # pinned submodule and CI image
├── scripts/
│   ├── init.sh
│   ├── init-egress-proxy.sh
│   ├── init-provider-sidecar-tls.sh
│   ├── init-provider-sidecar-override.sh
│   ├── container-runtime.sh
│   ├── render-egress-policy.py
│   ├── test-egress-proxy.sh
│   ├── test-netns-guard.sh
│   ├── test-socat-boundary.sh
│   ├── test-provider-sidecar-compose-candidate.sh
│   ├── test-provider-sidecar-tls-boundary.sh
│   └── validate.sh
├── .env.example
└── compose.yaml
```
