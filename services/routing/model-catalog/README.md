# model-catalog

**Where:** Compose service `model-catalog` (TypeScript, Node LTS), listening on `:8090` in its own network namespace. `apisix-models` calls it for `GET /v1/models`; the front door's operator listener (`9083`) forwards `/models-table` to it. It reads CPA and AISIX through one-way relays and has no Internet egress.

**What:** It answers Codex model catalog requests (`GET /v1/models?client_version=…`) and serves the `/models-table` diagnostics page. Membership comes from CPA (plus the AISIX supplement and `static_models`). Metadata for each model comes from a source chain: bundled models.dev / modelparams.dev snapshots, the CPA row (`@self`) and the channel upstream row (`@cpa-upstream`). The chain logic is shared with the Pi plugin [`openai-api-pi-extension`](https://github.com/xz-dev/openai-api-pi-extension) through the `vendor/` submodule.

**How to use:**

- Configuration: start from [`config.yaml`](config.yaml) and keep production policy in the private config (`ops/ansible/private-config/services/routing/model-catalog/config.yaml`).
- Check a config: `/model-catalog validate-config -`.
- Deploy: `ansible-playbook ops.yml --tags deploy-models-enricher-config`.
- Diagnose: `http://127.0.0.1:9083/models-table`, and use its refresh button for a full-chain rebuild.
- Develop: `npm ci && npm run typecheck && npm test && npm run build`.

Reference (endpoints, environment, config keys, chain entries, caches, memory limits): [docs/models-enricher.md](../../../docs/models-enricher.md).
