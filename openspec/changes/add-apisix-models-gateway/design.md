# Design: APISIX Models Gateway

## Context

See proposal.md — Why. Verified facts:

- Current path: `sub2api → socat relay (sub2api-cpa-relay) → CPA :8317`. Sub2API talks plain OpenAI/Anthropic style with `Authorization`; no gateway-specific headers.
- Sub2API `CodexModels` fetches `{upstream}/v1/models?client_version=…` and expects the ChatGPT Codex manifest; group/model permissions stay inside Sub2API.
- Sub2API Codex WS mode (`GATEWAY_OPENAI_WS_MODE_ROUTER_V2_ENABLED=true`, confirmed in source `openai_ws_forwarder_*`) derives `wss://<account-base>/v1/responses` for API-key accounts — production WS flows on this exact hop. Any middle box must be an opaque WS tunnel. (Bifrost failed this; spike report `/tmp/bifrost-spike-report.md` — since superseded, archived in this change's docs for reference.)
- Existing APISIX (apache/apisix:3.18.0-debian, standalone mode) already runs in the stack with its own relay/netns topology and already uses `serverless-*` Lua functions. APISIX proxies WS transparently with `enable_websocket: true` on the route and streams SSE without buffering.
- APISIX routing headroom (all built-in): `ai-proxy-multi` `fallback_strategy` + `max_retries` + `retry_on_failure_within_ms`; upstream `type` roundrobin/ewma/least_conn/chash; sticky via `chash` + `hash_on` cookie/header/vars; active/passive health checks.
- CPA management API (verified live): per-type channel lists (`/v0/management/openai-compatibility`, `*-api-key`) with `prefix`/`disabled`/`base-url`/`api-key-entries[].auth-index`; `POST /v0/management/api-call` proxies arbitrary requests with `$TOKEN$` substitution and per-credential proxy chaining.
- models.dev API (per anomalyco/models.dev): `https://models.dev/api.json` (provider-keyed), `/models.json` (provider-agnostic), `/catalog.json` (both). Spike: `api.json` 200 (~4.4 MB) but requires a browser User-Agent (403 otherwise). `https://modelparams.dev/api` returned HTML — its real JSON endpoint must be verified during implementation.
- OpenRouter `/models` (verified via api-call) returns parseable rich data: `context_length`, `top_provider.max_completion_tokens`, `architecture.input_modalities`, `supported_parameters`.

## Goals / Non-Goals

**Goals:**
- One new dedicated APISIX instance (`apisix-models`) replacing `sub2api-cpa-relay` in the data path; owns Codex models manifest splitting today and inference model routing later.
- One small Go sidecar (`models-enricher`) holds all custom logic; zero Lua for the enrichment.
- Standard ai-gateway isolation: digest-pinned images, nonroot/read-only, one-way socat edges, no wildcard ports.

**Non-Goals:**
- Model routing for inference traffic (headroom via APISIX natives; deferred).
- Changes to CPA core, CPA plugins, Sub2API behavior/config beyond its upstream address, OAuth channels.
- Caching channel model lists beyond one manifest build (only data sources cached, 10 min).
- Reusing the existing public-ingress APISIX instance (this hop gets its own).

## Spike Results (2026-09-03, local Podman fixture, report: /tmp/apisix-spike-report.md)

- **Route split: pass.** `arg_client_version ~~ .+` on `/v1/models` routes Codex manifest requests to the enricher (exact body); absent or EMPTY `client_version` falls to the catch-all → CPA. No APISIX-specific client headers needed. APISIX adds `Server: APISIX/3.18.0` response header (acceptable).
- **Passthrough: pass with caveats.** Chat completions body SHA-256 equal through APISIX vs direct CPA. SSE requires `proxy-buffering` plugin `disable_proxy_buffering: true` on the route (default nginx buffering stalls no-Content-Length streams). Caveat: upstream 5xx may be rewritten to APISIX's own 502 JSON on the error path — happy-path bodies are byte-equal; error semantics slightly altered, accepted.
- **WebSocket: pass (the decisive test).** `/ws` echo via APISIX: 101 upgrade, frames echoed, 32s keepalive. `/v1/responses` upgrade attempt tunneled through to CPA (CPA's own WS error/401 returned — NOT an APISIX-local parse error; exactly what Bifrost failed).
- **Sources: resolved.** models.dev `/api.json` (4.4 MB, browser UA required), `/models.json`, `/catalog.json` reachable. modelparams.dev JSON API is **`/api/v1/models.json`** (+`/api/v1/schema.json`, per-model `/api/v1/models/<provider>/<id>.json`) — not `/api`.
- Standalone config requirements: `deployment.role: data_plane` + `config_provider: yaml` + `APISIX_STAND_ALONE=true`; `apisix.yaml` must end with `#END`.
- Image pinned: `docker.io/apache/apisix:3.18.0-debian@sha256:84e6b5e787e9f889ebff88161cb9a16599bafcffa236c6b54c7f779a0655940d`.
- Egress note: on the spike host, squid proxy `172.17.0.1:20171` was refused from rootless netns; direct registry/internet egress worked. Production egress via squid per convention (sidecar honors `HTTPS_PROXY`).

**Go/no-go: GO.** All seams proven; constraints baked into sections 2–3.

## Decisions

### D1: Existing APISIX + Go sidecar; no Bifrost, no Portkey, no CPA plugin

Bifrost terminates `/v1/responses` WS (breaks Codex WS) and its stock image cannot load Go plugins; Portkey's hooks never run for `/v1/models` and it demands `x-portkey-*` headers; CPA plugin hooks never see `/v1/models`. APISIX is a true reverse proxy already in production; the sidecar carries the custom logic in Go.

### D2: Route split by query var, enrichment in the sidecar

APISIX route A: `uri: /v1/models`, `methods: [GET]`, `vars: [["arg_client_version", "~~", ".+"]]` → upstream = models-enricher. Route B (catch-all, lower priority): `uri: /*`, `enable_websocket: true` → upstream = CPA. The sidecar never sees non-manifest traffic; APISIX never interprets bodies.

### D3: Sidecar fetches CPA's native manifest itself, then composes

The sidecar calls CPA's client-facing `/v1/models?client_version=<incoming>` (with a CPA client key) for the native manifest as merge base, and CPA management API for channel discovery + api-call fan-out. It composes and returns the final manifest. Rationale: full control over merge semantics and failure tolerance.

### D4: Channel model fetches via CPA api-call only

The sidecar never holds channel credentials. Per-channel request path YAML-configurable with per-type defaults:

| CPA channel type | List source (mgmt API) | Default models path |
|---|---|---|
| openai-compatibility | `GET /v0/management/openai-compatibility` | `/v1/models?client_version=<incoming>` |
| claude-api-key | `GET /v0/management/claude-api-key` | `/v1/models` (header `x-api-key: $TOKEN$`, `anthropic-version`) |
| gemini-api-key | `GET /v0/management/gemini-api-key` | `/v1beta/models` |
| codex-api-key | `GET /v0/management/codex-api-key` | `/v1/models?client_version=<incoming>` |
| xai-api-key | `GET /v0/management/xai-api-key` | `/v1/models` |
| vertex-api-key | `GET /v0/management/vertex-api-key` | `/v1/models` |
| interactions-api-key | `GET /v0/management/interactions-api-key` | `/v1/models` |

Per key entry, `auth-index` is passed as `auth_index`. Channel identity follows CPA's composite convention (trimmed name + normalized base URL); no new UUIDs.

### D5: Per-type response adapters behind one pipeline

One pipeline (fetch → parse → enrich → override → filter → merge); differences confined to an adapter table (auth header, path, response parser). OpenRouter-style rich payloads map `context_length → context_window/max_context_window`, `top_provider.max_completion_tokens → max_tokens`, `architecture.input_modalities → input_modalities`; plain `{data:[{id}]}` payloads yield IDs only and rely on fallback sources.

### D6: Fallback sources cached 10 minutes, in-process

models.dev (`/api.json` primary; `/models.json`/`/catalog.json` as needed) and modelparams.dev (endpoint verified at implementation time) cached in-process 10 minutes, lazy TTL. Single replica; restart clears cache — acceptable. Browser User-Agent required for models.dev.

### D7: Merge semantics — fill gaps, explicit override wins, prefixed-only output

Merge base = CPA native Codex manifest (authoritative for OAuth channels). For each discovered channel model: emit `<prefix>/<name>` slug; fill missing fields from channel fetch → data sources (models.dev first, modelparams.dev fallback; first hit per field wins); YAML overrides last (may replace any field). Include/exclude regex per channel before merge. Final filter: drop every entry whose slug lacks a `<prefix>/` — CPA model routing is abandoned; manifest filtering only, inference requests never intercepted.

### D8: Deployment topology

```
sub2api ──(netns)──> [relay socat] ──> [apisix-models (new dedicated instance)]
                                            |
                                            |-- GET /v1/models + client_version -> [models-enricher :8090]
                                            |-- everything else (WS-enabled) ---> [relay socat] -> CPA :8317
                                                                      models-enricher --> CPA mgmt/api-call (same CPA edge)
                                                                      models-enricher --> squid egress -> models.dev / modelparams.dev
```

Two new containers (apisix-models + models-enricher) + relay re-pointing. Compose/env under `./apisix-models/` and `./models-enricher/`. Both images digest-pinned (apisix-models reuses the already-pinned `apache/apisix:3.18.0-debian` digest); enricher built via pinned golang multi-stage Dockerfile, nonroot, read-only rootfs.

### D9: Secrets

CPA management key and the CPA client key (for native manifest fetch) mounted as env (`CPA_MANAGEMENT_KEY`, `CPA_CLIENT_KEY`) via existing secret custody; never in apisix.yaml/YAML, never logged.

## Deployment Log (2026-09-03, host 100.94.238.35)

Canary → cutover → rollback drill → cutover, all verified. Enricher egress required adding an `enricher` service to squid policy.json (models.dev/modelparams.dev paths) + `squid -k reconfigure` + `docker network connect` of egress-proxy to `enricher-squid-target`. Image delivery: local `podman build --network=host` + `podman save | ssh docker load` (host docker0 missing; compose `pull_policy: build` requires `--no-build` on up). Cutover: `sub2api-cpa-relay` retargeted to apisix-models:9080, cpa-netns dropped from `sub2api-cpa-target`. Rollback drill: restoring those stanzas returned native manifest (117 models/52 bare); re-cutover returned enriched (702/0 bare). Known residual: Command Code + gmicloud channels 403 on live `/models` fetch (provider-side block; YAML path overrides available); `codex/*` models listed but CPA has no declared models for that prefix (pre-existing routing gap); Sub2API groups with configured model catalogs bypass the upstream manifest entirely.

## Risks / Trade-offs

- **APISIX route-match correctness for the split**: `vars` on `arg_client_version` is standard; verified in the local spike before deploy.
- **WS on the catch-all route**: `enable_websocket: true` is required and long timeouts must be set (`timeout`/`read`/`send`) or long Codex WS sessions get cut. Verified in spike.
- **Single APISIX now serves two trust zones** (existing public ingress + this internal hop): routes are separated by listener/port and network membership; a mis-merged route could expose CPA — mitigated by standalone config review and the one-way relay topology (the internal listener has no public network membership).
- **Cold-request latency**: fan-out N channels via api-call per manifest request; mitigated by concurrency, per-channel timeout (default 8s), overall deadline, graceful degradation.
- **models.dev/modelparams.dev availability**: egress via squid + TLS inspection CA; outage degrades enrichment, never fails the response.
- **Superseded approach**: `add-bifrost-codex-models-gateway` — spike evidence archived; that change should not be applied.

## Migration Plan

1. Local Podman spike: APISIX standalone with the two routes + mock CPA/enricher; prove WS opaque passthrough, SSE, route split, no-header-required (Bifrost spike artifacts serve as the harness template). Go/no-go recorded here.
2. Build models-enricher + Dockerfile + compose wiring; spike-verify behavior locally.
3. Deploy to gateway host: canary one synthetic Sub2API upstream account through the new listener; diff manifest vs direct-CPA baseline.
4. Cutover: point Sub2API's CPA upstream at the APISIX relay; remove legacy `sub2api-cpa-relay`.
5. Rollback: restore legacy relay service definition; no state to migrate.
