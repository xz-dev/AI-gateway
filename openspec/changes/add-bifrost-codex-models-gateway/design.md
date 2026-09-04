# Design: Bifrost Codex Models Gateway

## Context

See proposal.md — Why. Verified facts that shape the approach:

- Current path: `sub2api → socat relay (sub2api-cpa-relay, :8317) → CPA :8317`. Sub2API talks plain OpenAI/Anthropic style with `Authorization` header; no gateway-specific headers.
- Sub2API's `CodexModels` handler fetches `{upstream}/v1/models?client_version=…` from its configured upstream and expects the ChatGPT Codex manifest format; group/model permissions are enforced inside Sub2API and are unaffected by this change.
- Bifrost (Go, fasthttp): `GET /v1/models` is registered with the inference middleware chain including `TransportInterceptorMiddleware`; plugins implement `HTTPTransportPreHook(ctx, *HTTPRequest) (*HTTPResponse, error)` where a non-nil response short-circuits. Plugins load as Go `.so` (or WASM) via `framework/plugins/soloader.go`.
- CPA management API (verified live): per-type channel lists under `/v0/management/*-api-key` and `/v0/management/openai-compatibility` (entries include `prefix`, `disabled`, `base-url`, `api-key-entries[].auth-index`, optional `models[]` with per-model metadata); `POST /v0/management/api-call` proxies arbitrary requests with `$TOKEN$` credential substitution and per-credential proxy chaining.
- Production CPA `/v1/models?client_version=1.0.0` already emits the Codex manifest (114 models; 49 missing `max_tokens`); OpenRouter's own `/models` returns parseable rich data (`context_length`, `top_provider.max_completion_tokens`, `architecture.input_modalities`, `supported_parameters`).

## Goals / Non-Goals

**Goals:**
- One new container (`bifrost`) replacing the `sub2api-cpa-relay` hop; Sub2API and CPA zero changes.
- Codex manifest enrichment via one Bifrost plugin (`codex-models-enricher`) with YAML config.
- Standard ai-gateway isolation: digest-pinned images, nonroot/read-only, one-way socat edges, no wildcard ports.

**Non-Goals:**
- Model routing rules (Bifrost config supports them natively; deferred).
- Changes to CPA core, CPA plugins, Sub2API, APISIX, or OAuth auth-file channels.
- Caching of channel model lists beyond a single manifest build (only models.dev/modelparams.dev source data is cached, 10 min).
- A general-purpose plugin framework; exactly one plugin.

## Decisions

### D1: Bifrost + Go `.so` plugin, not Portkey, not CPA plugin

Portkey's guardrail pipeline never runs for `/v1/models` and it demands `x-portkey-*` headers; CPA plugin hooks never see `/v1/models`. Bifrost's `HTTPTransportPreHook` fires on `/v1/models` and can short-circuit with a full response — the only evaluated option that satisfies interception without forking.

### D2: Enrichment happens in PreHook short-circuit, not by post-processing CPA's response body in Bifrost

The plugin calls CPA itself (management API + `/v1/models?client_version=` for the native manifest) and composes the final manifest, then returns it. Bifrost's native `listModels` aggregation is not involved in the Codex path. Rationale: full control over merge semantics and error tolerance; avoids Bifrost's parser choking on CPA's `{models:[…]}` shape (OpenAI parser expects `{data:[…]}`).

### D3: Channel model fetches go through CPA `/v0/management/api-call`, never direct

The plugin never holds channel credentials. api-call provides `$TOKEN$` substitution (access_token → api_key → token fallbacks), per-credential proxy-url handling, and global proxy fallback. Per-channel request path is YAML-configurable with per-type defaults:

| CPA channel type | List source (mgmt API) | Default models path |
|---|---|---|
| openai-compatibility | `GET /v0/management/openai-compatibility` | `/v1/models?client_version=<incoming>` |
| claude-api-key | `GET /v0/management/claude-api-key` | `/v1/models` (header `x-api-key: $TOKEN$`, `anthropic-version`) |
| gemini-api-key | `GET /v0/management/gemini-api-key` | `/v1beta/models` |
| codex-api-key | `GET /v0/management/codex-api-key` | `/v1/models?client_version=<incoming>` |
| xai-api-key | `GET /v0/management/xai-api-key` | `/v1/models` |
| vertex-api-key | `GET /v0/management/vertex-api-key` | `/v1/models` |
| interactions-api-key | `GET /v0/management/interactions-api-key` | `/v1/models` |

Each entry's `auth-index` (per key entry) is passed as `auth_index`. Channels are keyed by CPA's composite identity convention (trimmed name + normalized base URL); no new UUIDs.

### D4: Per-channel-type response adapters behind one pipeline

One uniform pipeline (fetch → parse → enrich → override → filter → merge); differences confined to a small adapter table (auth header name, path, response parser). OpenRouter-style rich payloads map `context_length → context_window/max_context_window`, `top_provider.max_completion_tokens → max_tokens`, `architecture.input_modalities → input_modalities`; plain `{data:[{id}]}` payloads yield IDs only and rely entirely on fallback sources.

### D5: Fallback sources cached 10 minutes, in-process

models.dev (`https://models.dev/api.json`) and modelparams.dev (`https://modelparams.dev/api`) responses cached in plugin memory for 10 minutes (lazy TTL, no background refresher). In-process only: the deployment is a single replica behind one relay; a shared cache is unwarranted complexity. `ponytail:`-style ceiling: restart clears cache — acceptable.

### D6: Merge semantics — fill gaps, explicit override wins, prefixed-only output

Start from CPA's native Codex manifest (authoritative for OAuth channels). For each discovered channel model: emit the prefix slug form (`<prefix>/<name>`); fill any missing field from channel fetch → data sources (models.dev first, modelparams.dev as fallback — both consulted, first hit wins per field); finally apply YAML overrides, which may replace any field. Include/exclude regex lists apply per channel before merge.

CPA's model routing is abandoned: the emitted manifest contains ONLY prefix-qualified slugs (`<prefix>/<name>`). Bare slugs (no `/` prefix) are filtered out at the models layer, whether they came from CPA's native manifest or from a channel fetch. Inference requests are never intercepted — manifest filtering only.

### D7: Deployment topology

```
sub2api ──(existing netns)──> [bifrost-relay socat :8317] ──> [bifrost :8080]
                                                                  | (non-codex traffic)
                                                                  v
                                                          [cpa-side socat] ──> CPA :8317
                                                                  | (plugin mgmt/api-call)
                                                                  v
                                                          CPA /v0/management/* (same edge)
```

One container, two directed edges (in: from sub2api net; out: to cpa net) plus an egress edge to squid for models.dev/modelparams.dev. The existing `sub2api-cpa-relay` container is replaced by the bifrost pair (relay-in + service + relay-out). Compose/env under `./bifrost/`, image digest-pinned, config.json + plugin YAML + plugin `.so` built via pinned golang builder image (multi-stage Dockerfile, output digest-pinned runtime base).

### D8: Secrets

CPA management key mounted as env `CPA_MANAGEMENT_KEY` from the existing secret custody path (same pattern as other services; never in config.json/YAML, never logged).

## Spike Results (2026-09-03, local Podman fixture, report: /tmp/bifrost-spike-report.md)

- **S1 passthrough: semantic pass, NOT byte-equal.** Bifrost re-encodes responses and injects `extra_fields`/`provider_response_headers`. Violates the `bifrost-gateway` spec's byte-equivalence scenario.
- **S2 bare-model routing: pass.** Bare model names route to the single configured provider; `/v1/models` IDs get `cpa/`-prefixed by Bifrost's aggregator (the enricher short-circuit bypasses it).
- **S3 WebSocket: FAIL for our requirement.** `/v1/responses` upgrade succeeds (101) through Bifrost, but Bifrost **terminates and interprets** the WS as an inference endpoint (`isInferenceWSEndpoint`); it is not an opaque tunnel. Sub2API (`openai_ws_forwarder_*`, `GATEWAY_OPENAI_WS_MODE_ROUTER_V2_ENABLED=true`) derives `wss://<account-base>/v1/responses` for API-key accounts — so production Codex WS traffic flows on exactly this hop and would break.
- **S4 plugin seam: pass with caveat.** Stock image `maximhq/bifrost` (static musl) CANNOT load `.so` plugins (`Dynamic loading not supported`). A source-built dynamically-linked glibc `bifrost-http` + plugin compiled against the same core tree works: short-circuit returned exactly `{"models":[{"slug":"spike-ok"}]}`, other traffic unaffected. Deployment would require a custom source-built Bifrost image.
- **S5 sources: mixed.** `https://models.dev/api.json` reachable (200, ~4.4 MB, needs browser UA else 403). `https://modelparams.dev/api` returns HTML, not JSON — the correct JSON endpoint must be verified during implementation.
- Extra Bifrost operational requirements found: `allow_private_network: true` on the provider, air-gapped `pricing_url`/`model_parameters_url` as `file://` or it may fail to boot without getbifrost.ai.

**Go/no-go: Bifrost as the Sub2API↔CPA middle gateway is NO-GO (WS non-transparent + response re-encoding + custom source build). Decision on the fallback recorded below after user consultation.**

## Risks / Trade-offs

- **WebSocket transport (biggest risk)**: Sub2API has Codex Responses-over-WS enabled (`GATEWAY_OPENAI_WS_MODE_ROUTER_V2_ENABLED=true`). Bifrost WS handling of `/v1/responses` upgrades to an openai-compatible custom provider must be proven by spike before full implementation. If it fails: fallback is splitting WS traffic around Bifrost (dedicated relay), which weakens the single-gateway story — decision point after spike.
- **PreHook short-circuit on `/v1/models`**: middleware wiring is confirmed in source; actual behavior (headers, status, body integrity through fasthttp) proven by the same spike.
- **Bifrost upgrade coupling**: plugin compiles against Bifrost's `core/schemas`; pin Bifrost version and rebuild plugin on upgrades. Trade-off accepted for not maintaining a fork.
- **Cold-request latency**: each Codex manifest request fans out to N channels via api-call (serial would be slow). Mitigation: concurrent fan-out with per-channel timeout budget (default 8s) and overall deadline; failures degrade gracefully.
- **models.dev/modelparams.dev availability**: egress through squid with TLS inspection; CA bundle mount required. Outage degrades enrichment (fields stay missing) but never fails the response.

## Migration Plan

1. Spike (WS passthrough + PreHook short-circuit) on a local compose fixture; go/no-go.
2. Build plugin + Dockerfile + compose wiring in `./bifrost/`. Deployment-type repository: no dedicated test suite — correctness is proven by the spike (step 1) and post-deploy verification (steps 3-4).
3. Deploy to the gateway host, canary one synthetic Sub2API upstream account through Bifrost, compare manifest fields against direct-CPA baseline; then switch the shared relay and remove the old one.
4. Rollback: restore `sub2api-cpa-relay` service definition; Bifrost containers removed. No state to migrate.
