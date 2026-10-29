# Repository layout

Top-level directories and what they contain. Every directory under `services/` and `ops/` has its own README.

```text
.
├── services/                       # one directory per component, grouped by role
│   ├── sub2api/                    # README only; image-only service (Sub2API, PostgreSQL, Redis)
│   ├── cpa/
│   │   ├── config/                 # CLIProxyAPI config.example.yaml
│   │   └── cpa-model-sync/         # Rust channel-membership sync; docs/, docs/records/ (dated reports)
│   ├── routing/
│   │   ├── apisix-models/          # inner constant-pipe APISIX: apisix.yaml, config.yaml, start.sh
│   │   ├── aisix/                  # pinned AISIX build files, patch, public examples, private ignored runtime files
│   │   ├── aisix-status/           # independent no-JavaScript server-rendered status page
│   │   └── model-catalog/          # TypeScript Codex model catalog service (Node LTS)
│   │       ├── Dockerfile
│   │       ├── config.yaml         # example metadata chains, include_models, static models
│   │       ├── src/                # server, build pipeline, caches
│   │       ├── tests/
│   │       └── vendor/openai-api-pi-extension/ # pinned submodule: shared metadata compile core
│   ├── middleware/
│   │   ├── ai-sse-keepalive-proxy/ # pinned submodule and CI image
│   │   ├── openai-api-cache-keeper/ # pinned submodule and CI image
│   │   └── responses-ws-sse-bridge/ # README; image built in its own repository
│   └── firewall/
│       ├── apisix/                 # front door: apisix.yaml routes, config.yaml listeners, lua/ custom modules
│       ├── egress-proxy/           # Squid + Unbound; policy.example.json, testdata/
│       └── one-way-relay/          # README for the socat relay and netns-owner Compose anchors
├── ops/
│   ├── ansible/                    # playbook, roles, inventory; private-config/ (gitignored)
│   └── scripts/                    # init, validate, render, check, container-runtime
│       ├── host/                   # host-side tooling installed on the deploy root (gateway-recovery.py, units)
│       └── tests/                  # test-* suites run by CI and validate.sh
├── data/                           # ignored persistent runtime state (cpa/, egress-proxy/, sub2api/)
├── docs/                           # current guides; docs/records/ holds dated records and verification evidence
├── openspec/                       # specifications and change history
├── skills/                         # agent skills for operating the stack
├── .env.example
└── compose.yaml
```

The production deploy root (`/root/AI-gateway`) mirrors this tree. The private desired state in `ops/ansible/private-config/` uses the same relative paths.
