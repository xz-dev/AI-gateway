# Tasks: Bifrost Codex Models Gateway

## 1. Spike: prove the two risky seams

- [x] 1.1 Local fixture: docker compose with stock CPA (pinned digest), Bifrost (pinned digest), and a mock Sub2API client; Bifrost configured with single openai-compatible provider `cpa` pointing at CPA
- [x] 1.2 Prove plain + SSE passthrough: semantic pass (non-stream chat content identical; streaming SSE via mock proven). CAVEAT: Bifrost re-encodes JSON and injects `extra_fields` — NOT byte-equal; CPA fixture limitation prevented end-to-end `/v1/responses` SSE via CPA
- [ ] 1.3 Prove WebSocket passthrough: **FAILED** — Bifrost terminates/interprets `/v1/responses` WS (101 upgrade works, frames are parsed, not tunneled). No opaque-passthrough mode found in source
- [x] 1.4 Prove plugin seam: **PASS with caveat** — stock image cannot load `.so` (static musl); source-built dynamic glibc binary + same-core plugin short-circuits `/v1/models?client_version=x` exactly, other traffic unaffected
- [ ] 1.5 Go/no-go decision recorded in design.md: spike results recorded; **Bifrost NO-GO**; awaiting user decision on fallback architecture

## 2. Plugin: codex-models-enricher

- [ ] 2.1 Go module under `bifrost/plugin/`, pinned to the deployed Bifrost version's `core/schemas`; build as `.so` via pinned golang builder (multi-stage Dockerfile)
- [ ] 2.2 Config: YAML loading + validation (per-channel path overrides, per-model field overrides, include/exclude regex lists, source TTL default 10m, timeouts)
- [ ] 2.3 CPA management client: channel discovery across the seven key-type endpoints, exclude disabled/OAuth, secret via `CPA_MANAGEMENT_KEY` env
- [ ] 2.4 api-call fan-out: concurrent per-channel model fetch with per-channel timeout and overall deadline; partial failure logged (no secrets) and tolerated
- [ ] 2.5 Response adapters: OpenAI/OpenRouter rich format, Claude paged format, Gemini format, plain `{data:[{id}]}`; per-type auth header/path defaults per design D3
- [ ] 2.6 Metadata fallback: models.dev + modelparams.dev fetch with 10-minute in-process TTL cache; per-field fill of slug/display_name/context_window/max_context_window/max_tokens/input_modalities/supported_reasoning_levels/default_reasoning_level
- [ ] 2.7 Merge: start from CPA native manifest, fill gaps only, apply YAML overrides last; emit CPA prefix-slug convention (`<prefix>/<name>`)
- [ ] 2.8 Unprefixed slug filter: drop every manifest entry whose slug has no `<prefix>/` (CPA routing abandoned; models-list filtering only, inference requests never intercepted)

Note: this is a deployment-type repository — no dedicated test suite. Correctness is established by the feasibility spike (section 1) and post-deploy behavior verification (section 4).

## 3. Deployment wiring

- [ ] 3.1 `bifrost/Dockerfile`: pinned golang build + pinned runtime base, nonroot, read-only rootfs
- [ ] 3.2 `bifrost/config.json`: single `cpa` provider, plugin load path, datasheet/pricing URLs left default (unused for enrichment), no secrets in file
- [ ] 3.3 Compose: `bifrost` service + relay-in/relay-out socat pairs + two-member networks, replacing `sub2api-cpa-relay`; egress edge to squid with CA bundle mount
- [ ] 3.4 `.env.example` additions (CPA_MANAGEMENT_KEY placeholder) and README section update

## 4. Deployment validation and cutover

- [ ] 4.1 Deploy to the gateway host, then canary on the real environment: point one synthetic Sub2API upstream account at Bifrost; diff `/v1/models?client_version=` manifest against direct-CPA baseline (fields filled, prefixes correct, overrides applied)
- [ ] 4.2 Real client check: Codex CLI model picker refresh through Sub2API → Bifrost → CPA shows enriched models; chat + SSE + WS flows pass
- [ ] 4.3 Cutover: switch Sub2API CPA upstream to Bifrost relay; remove legacy `sub2api-cpa-relay`
- [ ] 4.4 Rollback runbook verified: restore legacy relay, stack returns to baseline with no residual state
