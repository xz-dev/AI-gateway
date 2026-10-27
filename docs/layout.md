# Repository layout

Top-level directories and what they contain.

```text
.
├── services/
│   ├── sub2api/                    # README only; image-only service
│   ├── cpa/
│   │   ├── config/                 # CLIProxyAPI config.example.yaml
│   │   └── cpa-model-sync/         # Rust channel-membership sync; docs/records/ holds dated reports
│   ├── routing/
│   │   ├── apisix-models/          # inner selector APISIX: apisix.yaml, config.yaml, routing_selector.lua, start.sh
│   │   ├── aisix/                  # pinned AISIX CI build, patch, public examples, private ignored runtime files
│   │   ├── aisix-status/           # independent no-JavaScript server-rendered status page
│   │   └── model-catalog/          # TypeScript Codex manifest enricher sidecar (Node LTS)
│   │       ├── Dockerfile
│   │       ├── config.yaml         # example metadata chains, include_models, static models
│   │       ├── src/                # server, build pipeline, caches
│   │       └── vendor/openai-api-pi-extension/ # pinned submodule: shared metadata compile core
│   ├── middleware/
│   │   ├── ai-sse-keepalive-proxy/ # pinned submodule and CI image
│   │   ├── openai-api-cache-keeper/ # pinned submodule and CI image
│   │   └── responses-ws-sse-bridge/ # README; image built in its own repo
│   └── firewall/
│       ├── apisix/                 # front door: apisix.yaml routes, config.yaml, lua/ custom modules
│       ├── egress-proxy/           # Squid + Unbound; policy.example.json, testdata/
│       └── one-way-relay/          # README for the socat relay and netns-owner Compose anchors
├── model-catalog-sidecar/          # nginx single-path bridge: front APISIX → apisix-models (injects own CPA key)
├── ops/
│   ├── ansible/                    # playbook, roles, inventory; private-config/ (gitignored)
│   └── scripts/                    # init, validate, render, check, container-runtime
│       ├── host/                   # host-side tooling installed on the deploy root
│       └── tests/                  # test-* suites run by CI and validate.sh
├── data/                           # ignored persistent runtime state (cpa/, egress-proxy/, sub2api/)
├── docs/                           # current guides; docs/records/ holds dated records and verification evidence
├── openspec/
├── skills/
├── .env.example
└── compose.yaml
```
