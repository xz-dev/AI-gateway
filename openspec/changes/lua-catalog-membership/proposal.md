## Why

Catalog membership in model-catalog is fixed code plus one glob list. After `union-catalog-membership`, the rule is: CPA IDs under a CPA prefix, CPA IDs matched by `membership.cpa_include`, every AISIX ID and the static models. Operators who want something else cannot express it. Examples: admit every CPA ID, exclude a few AISIX IDs, or prefer the AISIX entry when CPA and AISIX list the same ID. Each such wish needs a code change and an image release.

The membership filter should be part of the operator's profile, as a small Lua script next to `config.yaml`. The image's default script does exactly what the code does today, so nothing changes unless an operator edits it.

## What Changes

- **New profile file `catalog.lua`** for model-catalog. The image ships a default script that implements today's rule: the CPA prefix rule, compatibility and fallback modes, and an extra CPA admission list. The tracked repository holds the same script as a sample. A private override can live in `ops/ansible/private-config/services/routing/model-catalog/catalog.lua`.
- **Per-candidate interface.** The host calls `select(m)` once for every candidate ID from CPA, AISIX and the static models. `m` describes the ID: its sources, whether it carries a CPA prefix, the matched prefix, the prefix mode, and whether CPA would emit it as-is. The script returns `true` to keep the ID (optionally choosing its source), or `false` plus a reason. The script cannot add IDs. A `glob(pattern, id)` helper uses the existing standard-glob matcher.
- **Lua is the membership stage.** It runs during authority collection, before `include_models`, chain metadata and post-processing.
- **BREAKING (config)**: `membership.cpa_include` and the `membership` block are removed. They are rejected as legacy fields with a hint that points to `catalog.lua`.
- **Runtime**: wasmoon 1.16.0 (Lua 5.4 compiled to WebAssembly, MIT), pinned. The script runs in a sandbox with no `io`, `os`, `require`, code loading, `debug` or `coroutine`. Instructions, memory and wall time are bounded. It runs in a short-lived worker thread per authority collection.
- **Failure is safe.** A script error, limit or invalid return fails that collection only. The last good generation keeps serving, a WARN is logged and the models table shows the failed attempt. An empty catalog is never published.
- `validate-config` accepts `--script <path|->` and loads and dry-runs the script.
- `/models-table` lists IDs that the script rejects as hidden, with the script's reason.
- **Operations**: `deploy-config.yml -e component=model-catalog` deploys `catalog.lua` as a second managed file, with the same drift gate, baseline and rollback as `config.yaml`. The check rejects a profile that has both a custom `catalog.lua` and `metadata.include_models`.
- **Production migration**: the private profile moves `cpa_include: ["devin/*"]` and today's `include_models` hides into its `catalog.lua`. The served catalog stays byte-identical.
- `include_models` stays in the gateway config and in openai-api-pi-extension. The extension gets no change.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `codex-models-enricher`: membership is decided by the profile's `catalog.lua`; the script interface and its failure behavior; `membership.cpa_include` removed and rejected as legacy; prefix discovery and compatibility mode feed the script instead of applying a fixed rule.
- `models-enricher-runtime`: the Lua sandbox, its limits, thread placement and memory budget; `validate-config --script`.
- `models-table-force-refresh`: script-rejected IDs show as hidden with the script's reason.
- `production-ops-tooling`: `catalog.lua` is a managed model-catalog file; the deploy check rejects a custom script combined with `include_models`.

## Impact

- **model-catalog** (`services/routing/model-catalog/`):
  - new `src/membership.ts` (host side) and `src/membership-worker.ts` (Lua worker), and the default `catalog.lua`;
  - `authority.ts` admission, `build.ts` members, `config.ts` (remove `membership`, add the legacy entry), `project.ts` hidden reasons, `main.ts` (`validate-config --script`, `CATALOG_SCRIPT_PATH`);
  - `package.json` adds `wasmoon@1.16.0`; the bundle and the Dockerfile ship `glue.wasm` and the default script;
  - tests: sandbox limits, error fallback, default-script parity against the union parity fixture.
- **Compose**: `compose.yaml` mounts `./services/routing/model-catalog/catalog.lua` read-only and sets `CATALOG_SCRIPT_PATH`.
- **Ansible**: `ai_ops_model_catalog` deploy, verify and rollback handle two managed files; the combination check; `test-ansible-model-catalog-config.sh`.
- **Docs**: `services/routing/model-catalog/README.md` stays short; `docs/model-catalog.md` gets the script reference with examples; `docs/ansible-operations.md` and `ops/ansible/README.md` list the new file.
- **Production**: one image publish and one coordinated model-catalog cutover (image and private profile), on an `openai/*` model with the current playbooks.
- **Sequencing**: starts after `union-catalog-membership` has landed in production and is archived.
- **Unchanged**: openai-api-pi-extension; the `include_models` semantics; routing.
