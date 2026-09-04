# Proposal: Add APISIX Models Gateway

## Why

Clients calling `GET /v1/models?client_version=<codex-cli-version>` (Codex CLI custom-provider mode) need a rich ChatGPT Codex manifest (`slug`, `display_name`, `context_window`, `max_context_window`, `max_tokens`, `input_modalities`, `supported_reasoning_levels[].effort`, `default_reasoning_level`). CPA only serves complete metadata for channels with pre-written model entries; channels without a configured model list (e.g. OpenRouter) contribute zero models, and ~40% of served models lack `max_tokens` (measured: 49/114).

This change supersedes `add-bifrost-codex-models-gateway`: the Bifrost spike proved `/v1/responses` WebSocket is terminated and interpreted by Bifrost rather than transparently tunneled, which breaks the Codex Responses-over-WS traffic Sub2API sends on this hop, and its stock image cannot load Go plugins. APISIX is already deployed in this stack, proxies WebSocket/SSE transparently, and has built-in fallback (`ai-proxy-multi` `fallback_strategy`), load balancing (roundrobin/ewma/least_conn/chash), sticky sessions (`chash` + `hash_on`), and health checks.

## What Changes

- Add a **new dedicated APISIX instance** between Sub2API and CPA (the existing public-ingress APISIX is untouched), replacing the current direct socat relay hop (`sub2api-cpa-relay`). This instance owns the Codex models manifest today and inference model routing later.
- The new APISIX routes:
  - `GET /v1/models` with a non-empty `client_version` query → forwarded to a new `models-enricher` sidecar.
  - Everything else → transparent proxy to CPA (byte-preserving body, SSE streaming, `enable_websocket` for the Codex Responses WS transport).
- New `models-enricher` sidecar (small Go service) that answers Codex manifest requests:
  1. Lists all enabled non-OAuth key-type channels via the CPA management API.
  2. Fans out to each channel's models endpoint through CPA's `/v0/management/api-call` (`$TOKEN$` substitution; per-channel path overridable in YAML, default `/v1/models?client_version=<incoming value>`).
  3. Fills missing model metadata from models.dev (`/api.json`, `/models.json`, `/catalog.json`) and modelparams.dev, caching those data-source responses 10 minutes.
  4. Applies YAML per-channel/per-model overrides and include/exclude regex filters.
  5. Merges into CPA's native Codex manifest (fill-gaps by default; explicit overrides may overwrite) and returns it.
- The emitted manifest contains only prefix-qualified slugs (`<prefix>/<model>`); unprefixed model ids are filtered at the models layer because CPA's model routing is abandoned. Inference requests are never intercepted.
- Code/config under `./apisix-models/` (new APISIX instance config) and `./models-enricher/` (Go service, YAML config, Dockerfile) in this repo.

## Capabilities

### New Capabilities

- `apisix-cpa-gateway`: A dedicated APISIX instance carrying Sub2API → CPA traffic with transparent passthrough (SSE + WebSocket), query-var route split for Codex manifest requests, and the isolation conventions (one-way edges, digest pinning, no wildcard ports).
- `codex-models-enricher`: The sidecar behavior: channel discovery, api-call fan-out, metadata fallback with 10-minute source caching, overrides, regex filtering, prefixed-only slugs, and merge semantics.

### Modified Capabilities

(none)

## Impact

- **New code**: `./models-enricher/` (Go service, Dockerfile, YAML config).
- **Config**: new `./apisix-models/` instance config (apisix.yaml/config.yaml).
- **Compose**: new `apisix-models` and `models-enricher` containers plus relay/netns plumbing; `sub2api-cpa-relay` replaced by a relay targeting the new APISIX instance.
- **Secrets**: enricher requires the CPA management key (env/file mount, existing custody conventions).
- **Egress**: enricher needs outbound to models.dev and modelparams.dev via the squid egress path (TLS-inspection CA bundle).
- **Out of scope**: model routing rules for inference traffic (this new instance provides APISIX `ai-proxy-multi` / chash natively when needed later), CPA core changes, Sub2API changes, OAuth auth-file channels.
- **Superseded**: `add-bifrost-codex-models-gateway` (Bifrost approach; spike verdict NO-GO).
