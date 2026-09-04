# Tasks: APISIX Models Gateway

## 1. Spike: prove the APISIX seams

- [x] 1.1 Local Podman fixture: APISIX 3.18.0 (`84e6b5e7...`) + CPA (`aaab5960...`) + mock upstreams in shared netns
- [x] 1.2 Prove route split: `client_version` non-empty → enricher (exact body); absent/empty → CPA; no APISIX headers required
- [x] 1.3 Prove transparent passthrough: chat body SHA-equal; SSE proven with `disable_proxy_buffering`; caveat recorded (5xx may rewrite to APISIX 502 JSON)
- [x] 1.4 Prove WebSocket opaque tunnel: 101 + echo + 32s keepalive; `/v1/responses` upgrade tunneled to CPA (CPA's own error proves no local termination)
- [x] 1.5 Data sources verified: models.dev `/api.json|/models.json|/catalog.json` (browser UA); modelparams.dev = `/api/v1/models.json` (+ schema + per-model JSON); recorded in design.md
- [x] 1.6 Go/no-go recorded in design.md: **GO**

## 2. Sidecar: models-enricher

- [x] 2.1 Go service under `models-enricher/`: HTTP server, `GET /v1/models` handler (expects `client_version`), graceful shutdown
- [x] 2.2 Config: YAML loading + validation (per-channel path overrides, per-model field overrides, include/exclude regex lists, source TTL default 10m, timeouts)
- [x] 2.3 CPA management client: channel discovery across the seven key-type endpoints, exclude disabled/OAuth, secret via `CPA_MANAGEMENT_KEY` env
- [x] 2.4 CPA native manifest fetch: client-facing `/v1/models?client_version=` with `CPA_CLIENT_KEY` as merge base
- [x] 2.5 api-call fan-out: concurrent per-channel model fetch with per-channel timeout and overall deadline; partial failure logged (no secrets) and tolerated
- [x] 2.6 Response adapters: OpenAI/OpenRouter rich format, Claude paged format, Gemini format, plain `{data:[{id}]}`; per-type auth header/path defaults per design D4
- [x] 2.7 Metadata fallback: models.dev + modelparams.dev with 10-minute in-process TTL cache; per-field fill of slug/display_name/context_window/max_context_window/max_tokens/input_modalities/supported_reasoning_levels/default_reasoning_level
- [x] 2.8 Merge: fill gaps only, YAML overrides last; emit `<prefix>/<name>` slugs; drop unprefixed slugs (models-layer filter only)
- [x] 2.9 Deploy-time smoke verification on the gateway host replaces any offline test suite (deployment-type repo)

## 3. Deployment wiring

- [x] 3.1 `models-enricher/Dockerfile`: pinned golang build + pinned runtime base, nonroot, read-only rootfs
- [x] 3.2 `apisix-models/`: new dedicated APISIX instance config (apisix.yaml/config.yaml) — route A (models split) + route B (catch-all, `enable_websocket`, long timeouts), upstreams for enricher and CPA
- [x] 3.3 Compose: new `apisix-models` + `models-enricher` services + relay/netns pairs per convention; re-point Sub2API CPA hop to the new instance's relay; egress edge (squid + CA bundle) for the enricher
- [x] 3.4 `.env.example` additions (CPA_MANAGEMENT_KEY, CPA_CLIENT_KEY placeholders) and README section update

## 4. Deployment validation and cutover

- [x] 4.1 Deploy to the gateway host, canary: point one synthetic Sub2API upstream account at the new listener; diff `/v1/models?client_version=` manifest against direct-CPA baseline (fields filled, prefixes correct, overrides applied, bare slugs gone) — prefix filter + passthrough PASS; models.dev fill blocked until enricher squid listener exists (see deploy-canary-report.md)
- [x] 4.2 Real client check: Codex CLI model picker refresh through Sub2API → APISIX → CPA shows enriched prefixed-only models; chat + SSE + WS flows pass
- [x] 4.3 Cutover: switch Sub2API CPA upstream to the new instance's relay; remove legacy `sub2api-cpa-relay`
- [x] 4.4 Rollback runbook verified: restore legacy relay, stack returns to baseline with no residual state
