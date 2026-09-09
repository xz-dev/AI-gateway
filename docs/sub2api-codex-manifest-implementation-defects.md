# Sub2API codex-manifest compatibility layer — implementation defects

Source reviewed: `Wei-Shaw/sub2api` @ `578785ee7fb35030b094b69624efe25670a36f5f` (read-only snapshot).
Deployed image exhibiting the behavior: `docker.io/weishaw/sub2api:0.2.3`.
All file/line references are against the snapshot above.

## Defect 1 — `client_version` query-param sniffing hijacks `GET /v1/models`

`internal/server/routes/gateway.go:71` (registered at `:209` and `:367`):

```go
modelsHandler := func(c *gin.Context) {
	if c.Query("client_version") != "" {
		codexModelsHandler(c)
		return
	}
	h.Gateway.Models(c)
}
```

Any model-list request carrying a `client_version` query parameter is silently rerouted from the standard OpenAI model list (`Gateway.Models`) into the codex manifest pipeline. `client_version` is not codex-namespaced — it is a generic client-telemetry parameter that other OpenAI-compatible clients also send (pi sends `client_version=0.85.1-xz.…` on every discovery refresh).

Consequences of the silent switch:

- **Response schema changes**: OpenAI `{"data":[{"id":…}]}` → codex `{"models":[{"slug":…}]}`. Clients parsing the OpenAI schema receive an unparsable body.
- **Inventory source changes** (Defect 3) with no signal to the client that it happened.
- No path, User-Agent, or group-setting check distinguishes an actual Codex CLI from any other client; the hijack is unconditional.

`dispatchCodexModelsGateway` (`gateway.go:507`) then forks again on group platform: OpenAI groups → `OpenAIGateway.CodexModels`; every other platform → `Gateway.CodexModels`. Two more handler implementations, two more inventory semantics (below).

## Defect 2 — hardcoded fallback models are invented into the manifest

`internal/handler/gateway_handler.go` — `codexModelIDsForGroup` (`:1229`) falls back to `defaultCodexModelIDsForPlatform` (`:1481`) / `defaultModelIDsForPlatform` (`:1490`) when the group has no available models:

```go
case service.PlatformDeepseek:
	return []string{"deepseek-v4-pro", "deepseek-v4-flash"}
default:
	return defaultModelIDsForPlatform(platform)   // openai → openai.DefaultModelIDs(), …
```

A group with zero usable models for a platform therefore advertises a **static, compiled-in model list it cannot route**. Clients discover phantom models and only fail at the first real scheduling attempt. A model manifest should describe what the group can serve; an empty inventory must produce an empty manifest, never a synthesized one.

## Defect 3 — the same key has three divergent model inventories

For one API key, the inventory now depends on request shape, not on the group's entitlement:

1. `GET /v1/models` (no query) → `Gateway.Models` — the group entitlement list.
2. `GET /v1/models?client_version=…`, non-OpenAI group → `Gateway.CodexModels` — `GetAvailableModels` (account model mappings, `gateway_service.go:1378`) **minus** `FilterCodexModelIDsForGroup` (`service/openai_codex_models_service.go:55`: strips image/media models, wildcards, and `codexAuto*` entries unless explicitly enabled) **plus** Defect-2 hardcoded fallbacks when empty.
3. Same request, OpenAI group → `OpenAIGateway.CodexModels` (`handler/openai_codex_models_handler.go`) — group-configured manifest if `codex_models_manifest_config` is set; otherwise a pinned-account or scheduler-selected **upstream ChatGPT manifest** proxied and merged locally. The visible inventory then depends on upstream account health, retry/switch state and ETag interplay — not on the group's model list.

None of the three is a schema-only transformation of another: the codex view removes entitled models (media/wildcard/auto filtered out) and adds non-entitled ones (fallbacks). "Which models can this key use" has no single answer in the system.

Related smells in the same code:

- The platform gate in `OpenAIGateway.CodexModels` (`openai_codex_models_handler.go:33`, 404 unless OpenAI/Composite) is unreachable for non-OpenAI platforms because the dispatcher never routes them there — a dead guard that signals how tangled the dispatch has become.
- The OpenAI-group path lets a **read-only model-list request** drive scheduler account selection and upstream fetches (`SelectAccountForModelWithExclusions` + `FetchCodexModelsManifest` with up to 3 account switches), so a discovery refresh can be slow or 503 purely from upstream account state.

## Defect 4 — passthrough makes the public model list collapse to defaults by design

`internal/service/gateway_service.go:1423` (`GetAvailableModels`):

```go
// Passthrough routing accepts models independently of model_mapping. A stale
// mapping on any eligible passthrough account therefore cannot define the
// public whitelist; return nil so the handler uses its default model set.
if platform == PlatformOpenAI && acc.IsOpenAIPassthroughEnabled() {
    return nil
}
```

If **any** schedulable OpenAI account in the group has passthrough enabled, the entitlement enumeration returns `nil` and every no-`client_version` model list falls back to the hardcoded `openai.DefaultModels` (observed: 15 entries, 17 on v0.2.4) — regardless of what the group can actually serve. Passthrough ("accept any upstream model at request time") and enumerable discovery are mutually exclusive in this design; the only complete, self-maintaining inventory under passthrough is the codex-manifest path (Defect 1), which is why this gateway keeps forwarding `client_version` on the Sub2API leg.

## Production observations (this gateway, 2026-09-09)

- pi's discovery always carries `client_version`, so it permanently lives in the codex path; the entitlement leg of our APISIX intersection arrives in codex schema (`models[].slug`) — the shape switch is confirmed live.
- Per-key entitlement probes: `#0 full` → 183, `旋律` → 18, `jiaxin` → 9, `claw-memory` → 2. These numbers are the codex-manifest builder's group-mapping projection, not `Gateway.Models` output.
- Bare static `gpt-5.6-sol` was absent from `#0 full`'s projection while present in the catalog; the intersection dropped it until the group mapping is updated. Consistent with Defect 3: the projection is the authority for the codex path.

## Suggested upstream fixes

1. Route codex manifests by an explicit signal only: the existing `/backend-api/codex/models` path, a dedicated route, or a per-group opt-in — never by generic query-param sniffing on `/v1/models`.
2. Delete the default-model fallbacks from manifest builders; empty inventory ⇒ empty manifest.
3. Make all model-list responses projections of one entitlement source, differing only in wire schema.
4. Keep model-list reads out of scheduler account selection; serve from cached group state.

## Workaround (operator side, current deployment)

- Treat the codex-path projection as the effective discovery inventory: keep group account model mappings complete (that is what the manifest is built from).
- Clients needing the OpenAI schema must call `/v1/models` **without** `client_version`; our APISIX route `ai-api-models-intersect` intentionally matches `arg_client_version` and normalizes the intersection itself.
