# Proposal: Add Bifrost Codex Models Gateway

> **SUPERSEDED by `add-apisix-models-gateway` (2026-09-03): spike proved Bifrost terminates `/v1/responses` WebSocket (breaks Codex WS) and its stock image cannot load Go plugins. Do not apply this change.**

## Why

Clients calling `GET /v1/models?client_version=<codex-cli-version>` (Codex CLI custom-provider mode) need a rich ChatGPT Codex manifest (`slug`, `display_name`, `context_window`, `max_context_window`, `max_tokens`, `input_modalities`, `supported_reasoning_levels[].effort`, `default_reasoning_level`). Today CPA only serves complete metadata for channels whose model entries were pre-written into config; channels without a configured model list (e.g. OpenRouter) contribute zero models, and ~40% of served models lack `max_tokens` (measured on production: 49/114 missing).

The CPA plugin route was evaluated and abandoned: CPA plugin interceptors only fire on the model-execution path, never on `/v1/models`, and plugin route registration is management-only. Portkey was evaluated and rejected: its guardrail hooks never run for `/v1/models` (`shouldSkipHook` restricts to chatComplete/complete/embed/messages) and it requires `x-portkey-*` headers Sub2API will never send.

Bifrost (maximhq/bifrost) supports the required seam natively: `HTTPTransportPreHook` runs on the `/v1/models` route and may short-circuit with a complete `*HTTPResponse`.

## What Changes

- Add a new `bifrost` service to the compose stack, inserted into the Sub2API → CPA data path (replacing the current direct socat relay hop), following existing one-way relay/netns/firewall conventions.
- Ship a custom Go plugin (`codex-models-enricher`) loaded by Bifrost that, for `GET /v1/models` requests carrying a `client_version` query parameter:
  1. Lists all enabled non-OAuth key-type channels via the CPA management API (`openai-compatibility`, `claude-api-key`, `gemini-api-key`, `codex-api-key`, `xai-api-key`, `vertex-api-key`, `interactions-api-key`).
  2. Fans out to each channel's models endpoint through CPA's `/v0/management/api-call` (`$TOKEN$` substitution; per-channel path overridable in YAML, default `/v1/models?client_version=<incoming value>`).
  3. Fills missing model metadata fields from models.dev and modelparams.dev (data-source responses cached 10 minutes).
  4. Applies YAML per-channel/per-model field overrides and include/exclude regex filters.
  5. Merges results into CPA's native manifest (fill-gaps by default; explicit YAML overrides may overwrite) and short-circuits the Codex-format response.
- The emitted manifest contains only prefix-qualified slugs (`<prefix>/<model>`); unprefixed model ids are filtered out at the models layer because CPA's model routing is abandoned. Inference requests are never intercepted.
- Requests to `/v1/models` without `client_version`, and all other traffic, pass through untouched (plain proxy to CPA; no enrichment).
- All code, Bifrost `config.json`, and plugin YAML live under `./bifrost/` in this repo.

## Capabilities

### New Capabilities

- `bifrost-gateway`: The Bifrost service deployed in the Sub2API → CPA path: transparent passthrough of all CPA-bound traffic (including SSE and WebSocket), single `cpa` upstream provider, deployment/networking conventions (one-way edges, nonroot, read-only, digest-pinned images).
- `codex-models-enricher`: The Bifrost plugin behavior for Codex manifest enrichment: channel discovery, api-call fan-out, metadata fallback with 10-minute source caching, overrides, regex filtering, and merge semantics.

### Modified Capabilities

(none)

## Impact

- **New code**: `./bifrost/` (Bifrost config, plugin Go module, plugin YAML, Dockerfile/build pinning).
- **Compose**: new `bifrost` container plus relay/netns plumbing; `sub2api-cpa-relay` target re-pointed (or replaced) so Sub2API reaches CPA via Bifrost.
- **Secrets**: plugin requires the CPA management key (env/file mount, same custody conventions as existing services).
- **Egress**: plugin needs outbound access to models.dev and modelparams.dev via the existing squid egress path (TLS inspection CA bundle).
- **Out of scope**: model routing rules (future work — Bifrost config supports it natively), CPA core changes, Sub2API changes, OAuth (auth-files) channels.
