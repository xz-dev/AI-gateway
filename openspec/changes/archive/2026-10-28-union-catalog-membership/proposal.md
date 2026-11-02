## Why

Production CPA runs a gateway-built image (`v8.0.38-pr6760-…`) with `routing.force-model-prefix: true`, and the catalog depends on both. Upstream fixed the xAI image prefix case that #6189 patched (upstream commits 45ccd9c3 and 8a3e7622, released in v8.0.44), so the custom build now exists mainly to keep the prefix-only listing. With CPA's default (`force-model-prefix: false`), every prefixed credential also registers its bare model IDs. The current catalog would then list those bare IDs and drop AISIX bare IDs as "already in CPA". So model-catalog gets its own fixed membership rule, and CPA can return to an official image with its default configuration.

## What Changes

- **Catalog membership** becomes the union of three sources, deduplicated by exact ID:
  - every AISIX models-API ID;
  - every CPA native-manifest ID that starts with a configured CPA prefix plus `/`;
  - configured static models.
- A new optional `membership.cpa_include` glob list admits CPA IDs that carry no CPA prefix. It uses the `include_models` matcher. Today it holds `devin/*`, because the Devin account has no prefix and its original IDs contain `/`.
- **CPA prefixes** come from the CPA Management API:
  - key-channel `prefix` values;
  - OAuth prefixes, inferred per account from its registered models: `P/x` and `x` both registered.

  The positive identity-admission logic (OAuth model definitions and alias name pairs) and the AISIX "AISIX minus CPA original inventory" supplement are removed.
- **Output order**: every catalog list model-catalog emits puts IDs without `/` first, then all other IDs, each group in Unicode code-point order. In the Codex manifest model-catalog sets `priority` from that order on every row, including rows passed through from CPA. `priority` is the only field it rewrites on a passthrough row.
- **Compatibility mode**: if CPA reports `force-model-prefix: true`, model-catalog admits every CPA ID containing `/`. This is today's set, so model-catalog can be deployed before the CPA switch and stays correct after a rollback.
- **BREAKING (operations)**: CPA switches to the official image `eceasy/cli-proxy-api` at the latest release (v8.0.44 at planning time, v8.0.45 at apply time), and `force-model-prefix` is turned off. The upgrade brings upstream's new embedded Devin model list (+7/-17 IDs between v8.0.38 and v8.0.45). It is accepted through a source-proven ledger: the observed difference must equal the embedded-file difference exactly. The stale AISIX direct model `devin/deepseek-v4-1-flash`, which v8.0.45 no longer registers, is removed in the same window. `stream-bootstrap-buffering` is turned on for upstream's partial fix of the Codex SSE framing defect (#6760 is not in the release). Both gateway patches are dropped: #6189 (superseded upstream) and #6760 (Codex SSE event boundaries, still open upstream; verification gate below).
- With `force-model-prefix` off, CPA also serves slash IDs that carry no CPA prefix through any credential that registers that exact ID, for example `deepseek/deepseek-v4.1-flash`. Before, only credentials without a prefix could serve them. The inner selector's slash rule is unchanged: bare IDs still go to AISIX.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `codex-models-enricher`: the membership rule, the `membership.cpa_include` list, CPA prefix discovery, compatibility mode, the output order and Codex `priority`, and updated include_models and authority-snapshot scenarios.
- `aisix-catalog-supplement`: AISIX contributes every ID instead of the difference against CPA's original inventory.
- `cpa-first-catalog-routing`: CPA runs an official upstream image with the default prefix behavior, gateway routing does not depend on `force-model-prefix`, and AISIX resources name only IDs that CPA registers.

## Impact

- **model-catalog** (`services/routing/model-catalog/`):
  - code: `authority.ts` admission and AISIX handling, `build.ts` members, output order and Codex `priority`, `project.ts` table order, `config.ts` with the new `membership` block;
  - tests and the tracked `config.yaml`;
  - private config: adds `membership.cpa_include: ["devin/*"]`.
  - Management reads drop `oauth-model-alias` and `model-definitions/<provider>`, and add `GET /v0/management/force-model-prefix`.
- **AISIX**: the private `services/routing/aisix/resources.yaml` loses the direct model `devin/deepseek-v4-1-flash` and its target in the `deepseek-v4.1-flash` route.
- **CPA**:
  - `compose.yaml`, `.env.example` and the private `.env` (`CLI_PROXY_IMAGE`);
  - the live CPA setting, changed through the Management API.
- **Docs**: `docs/model-catalog.md`, `docs/catalog.md`, `docs/add-openai-compatible-provider.md`, `docs/model-routing.md`, a deployment record.
- **Production**: two model-catalog image publishes (the second adds the output order) and deploys, one CPA image recreate, one AISIX config deploy, one live setting change. All production steps run on an `openai/*` model with the Ansible playbooks current at apply time; `standardize-ansible-operations` is being implemented in parallel.
