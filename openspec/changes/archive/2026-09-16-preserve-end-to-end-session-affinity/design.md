## Context

See `proposal.md` for motivation and the two delta specs for the behavioral contract. This design covers the conversational inference paths that dispatch through OpenAI API-key accounts to AISIX, including Anthropic Messages compatibility conversion; it does not introduce a new identity or routing service.

Source inspection used Sub2API v0.2.4 (`5de5e2bed035d43591a2e10e51f420ef6a84eb98`), CPA v7.2.155 (`7fac6b15bcfe5ea55c18c9eaec8e5b7e6457d974`), and AISIX v1.2.0 (`adcf0523b9f84deed64fbdea311df292c5bc541b`, with the separately deployed cooldown deadlock fix). These are evidence baselines, not permission to upgrade or replace production images.

| Boundary | Relevant source behavior |
|---|---|
| Sub2API raw Chat | `openai_gateway_chat_completions_raw.go` restricts client headers; a supplied session does not automatically survive. |
| Sub2API Responses | `openai_gateway_service.go` permits `session_id`; `openai_gateway_forward.go` copies allowed headers. Generation/isolation behavior differs from the raw path. |
| Sub2API conversion and WS bridge | `openai_gateway_chat_completions.go`, `openai_ws_forwarder_ingress.go`, and `openai_ws_http_bridge.go` rebuild requests and replay history. Some generated identity paths involve the selected account. |
| Sub2API Anthropic Messages | `openai_gateway_handler.go` dispatches `/v1/messages` through the same OpenAI account scheduler. `openai_gateway_messages.go` and `openai_gateway_messages_chat_fallback.go` convert it to Responses or Chat Completions; Claude Code's explicit session input is currently handled by a narrower helper rather than the common explicit-session extractor. |
| Sub2API identity validation | `session_id.go` already accepts explicit protocol headers for correlation and rejects invalid UTF-8, control characters, and values longer than 255 Unicode characters. That persistence helper is not yet the affinity contract. Use a stricter 255-byte affinity boundary so CPA's 256-byte limit cannot reject a value AISIX already used. |
| Sub2API header overrides | `account_header_override.go` applies static values and blocks static session-header overrides; it is not a dynamic forwarding template. |
| AISIX | `routing.rs` hashes headers/cookies/caller-key/IP within priority tiers; it does not read JSON `prompt_cache_key` or retain fallback-session bindings. |
| AISIX to CPA | Standard-protocol forwarding uses `request.forward_client_headers`; the reserved `x-aisix-*` namespace is not forwarded. |
| CPA | `auth/selector.go` recognizes `Session-Id` and `Session_id` case-insensitively, validates explicit IDs, and owns provider/model/session credential bindings and native expiry/reselection. Session affinity defaults off; the checked local configuration has no `routing` block. Its in-memory cache has TTL cleanup but no persistent recovery or global entry cap. |

Existing main specs include historical deployment descriptions. This change does not reconstruct those older topologies: retain the current private networks, published surfaces, APISIX forwarding roles, and one CPA provider pool.

## Goals / Non-Goals

**Goals:**
- Make one validated explicit session identity usable by both selectors across Chat Completions, Responses, WS HTTP bridge, and Anthropic Messages conversion, with a small compatibility-scoped propagation change rather than a new selector.
- Keep affinity stable while route state is unchanged; allow native re-selection when eligibility, priorities, or membership change.
- Produce replayable offline evidence at actual forwarding boundaries before any production application.

**Non-Goals:**
- New session IDs, API-key/user namespaces, cross-key identity policy, content-prefix routing, content-derived propagated identity, or a session registry.
- Sticky fallback retention, new retry layers, another proxy/sidecar, or AISIX/CPA routing-core patches.
- Replacing provider-required session transformations, persisting WS history across processes, or guaranteeing KV-cache hits.
- Expanding the status page, adding a telemetry service, changing model permissions, or including the unrelated context-compression proposal.

## Decisions

### 1. Reuse explicit identity; use `session_id` on the gateway hop

Resolve the client's explicit identity once using Sub2API's existing session-signal precedence, before an affected path rewrites body or account-specific identity. Include `X-Claude-Code-Session-Id` and its already supported Messages metadata form at their existing precedence rather than adding a competing order. Normalize once as valid UTF-8, trim surrounding whitespace, reject control characters, and treat values longer than 255 bytes as absent. Carry the accepted value in request/session context and emit it as `session_id` to AISIX. Header names are case-insensitive; preserve the accepted value rather than hash it with a selected upstream account or credential ID.

The ordinary case is an existing client session header. If the existing extractor recognizes another explicit input, reuse that behavior rather than invent new sources. In particular, reading an already supported body cache key as a fallback signal does not authorize rewriting that body field or forcing it to equal a separately supplied session header. When no valid explicit identity exists, this feature leaves existing protocol behavior alone: do not add a content-fingerprint fallback or generate an ID for affinity. A pre-existing protocol-generated value is not a new explicit-client-session guarantee; native missing-key fallback applies only if the resulting downstream session header is absent.

This is not authentication. Clients are expected to use stable, sufficiently unique conversation IDs. Coincident IDs can share routing affinity without automatically sharing conversation contents. Existing authorization and security controls remain authoritative; adversarial shared-state isolation is not introduced as an affinity prerequisite.

Alternatives rejected: a new tenant namespace changes existing identity unnecessarily; a new custom header would require more interoperability work; `x-aisix-routing-key` alone cannot carry identity onward to CPA.

### 2. Repair only the affected Sub2API API-key forwarding paths

**Implementation notes (2026-09-16):**
- Affinity identity resolution uses `resolveOpenAISessionAffinityID(c, body, includeClaudeMessages bool)` in `session_id.go`. It reuses the existing `explicitOpenAIHeaderSessionNames` precedence, then `prompt_cache_key` body fallback, then (for Messages only) Claude Code header/metadata. Validation uses `sanitizeOpenAISessionAffinityID` which applies the existing `sanitizeSessionID` checks plus a 255-byte UTF-8 length limit.
- The resolved identity is stored in Gin context under `openai_session_affinity_id` via `rememberOpenAISessionAffinityID` at the start of each public forwarding entry point (`ForwardAsChatCompletions`, `ForwardAsAnthropic`, `Forward`, `proxyOpenAIWSHTTPBridgeTurn`).
- `applyOpenAISessionAffinityHeader(c, account, headers)` is called at the final outbound stage in each builder after all account overrides and identity rewrites. It only applies when `account.IsOpenAIApiKey()` and `account.Credentials["session_affinity_header_enabled"] == true`.
- Cache-key independence: a new `ExtractPromptCacheKey` method on `OpenAIGatewayService` returns the body `prompt_cache_key` verbatim when present, falling back to the session identity only when the body key is absent. Both Chat and Messages handlers now use this via `resolveOpenAISessionInputs` instead of `ExtractSessionID`.

Reuse the existing request builders and converge the existing session helpers on one accepted explicit identity. Cover raw Chat, Chat-to-Responses conversion, Responses passthrough/non-passthrough, WS HTTP bridge, and Anthropic Messages conversion to either Responses or raw Chat Completions. A raw-header allowlist edit alone is insufficient where another path generates, isolates, or overwrites identity later. Token counting, embeddings, images, audio, video, realtime, search, and unrelated native-provider accounts are outside this conversational affinity contract.

Scope the behavior to the OpenAI API-key forwarding configuration used for AISIX. Prefer an existing upstream compatibility control if it can express that scope; otherwise use the smallest upstreamable account-scoped opt-in, defaulting to current behavior for unrelated accounts. Do not detect AISIX by a hardcoded hostname or apply blanket header passthrough. Do not remove Codex/OAuth account isolation to make an API-key gateway test pass.

For an enabled path, the resolved explicit identity must survive request rebuilding, account selection, and retries. A WS connection carries its already resolved explicit identity into bridge turns; reconnects rely on the client resending the ID and whatever history the existing protocol requires. No additional persistent session map is needed.

Alternatives rejected: static `header_overrides` cannot interpolate a request identity; body-to-header logic in an extra sidecar adds another component while leaving inconsistent earlier transformations unresolved.

### 3. Configure native AISIX affinity, including its missing-key behavior

For the approved logical aliases, use this routing fragment rather than change target definitions:

```yaml
strategy: consistent_hash
hash_on:
  - type: header
    name: session_id
  - type: api_key
```

On the existing CPA ProviderKey, merge `session_id` into `request.forward_client_headers` without adding credential headers or wildcards. Check that no same-name static default header shadows the forwarded value. Keep the single provider pool and exact direct target names, including `/`; do not create nested logical targets or a default route.

Derive the affected routing inventory from current production resources immediately before implementation. Convert only routes that currently use dynamic balancing and contain multiple targets in at least one active priority tier. The expected initial set is `grok-4.6`, `memory-reflect`, and `memory-fast`, subject to exact live reconciliation. Preserve their current targets, weights, priorities, retries, fallback bounds, and cooldowns. Same-tier round-robin changes to hash-based distribution where affinity is enabled; singleton priority tiers still choose their one eligible member. Keep ordered routes, including `memory-consolidation`, on `failover` so backups do not receive healthy traffic merely to distribute sessions. Unrelated routes remain unchanged.

Without `session_id`, the configured native fallback hashes the caller API-key ID. Multiple conversations behind the same Sub2API-to-AISIX key can therefore concentrate on the same target. This is explicitly not the old round-robin distribution or a per-session guarantee; no special absent-key routing engine is added.

Recovery remains stateless:

```text
S -> A
     A excluded -> B
     A eligible again -> native ordering may select A again
```

There is no new session-to-target TTL, concurrent-binding protocol, or fallback store. An unchanged ring and eligibility set yield deterministic selection; concurrent failures or configuration changes can still produce different outcomes. Cooldown expiry restores eligibility, not proof of recovered health or quota.

### 4. Explicitly enable CPA native credential affinity for one hour

Before composed-path acceptance, set CPA's effective routing configuration to `session-affinity: true` with `session-affinity-ttl: "1h"`. This is a global CPA selector mode, not an AISIX-alias-local setting: it also lets CPA use its existing explicit and fallback identity extraction on other supported requests. Record that scope and verify unrelated protocol/provider regressions rather than describing the switch as local to the three AISIX routes.

Forward the chosen session header to CPA and let CPA's existing binding refresh, availability checks, expiry, result handling, and fallback identity behavior remain responsible for credential selection. A bound credential can remain preferred after a higher-priority credential recovers; that credential-level behavior is independent of AISIX's target-level recovery. The cache is process-local: restart loses bindings. Concurrent first requests may race before a binding exists; accept that bounded cold-start divergence rather than add per-session locking. Active accesses refresh the one-hour lifetime; this does not claim provider cache data lives for one hour.

No CPA selector patch is planned. Do not add a second credential pool or move real provider secrets into AISIX. Provider-specific onward session forwarding through CPA's existing `$Inbound-Header` mechanism is only appropriate where that provider already requires it; it is not a blanket part of this change. Do not forward `session_id` onward to external providers merely because CPA consumes it for credential selection.

Do not force `prompt_cache_key`, cache retention, or provider conversation fields to match `session_id`. CPA's compatibility cache-key handling is capability-gated, and an identity header alone does not prove the upstream stores or reuses a prefix. Switching target/model can lose cache locality; returning to an old target can still hit a surviving cache.

### 5. Preserve transport, history, and retry boundaries

APISIX and the existing SSE/egress hops continue forwarding; they do not own model or credential affinity. WS HTTP bridge continues reconstructing history and removing continuation fields where its current conversion requires it. Do not forward an opaque prior-response reference to a different provider as a substitute for replayable history.

Keep existing bounded pre-content retries at their current layers. No fallback after generated content has reached the client, no joined streams from different targets, and no extra session-recovery retry loop are introduced.

### 6. Verify with existing tools and synthetic traffic

Use the affected project's existing test framework and the gateway's existing mock/candidate patterns; do not add a new test framework or generic routing simulator. Extend a bounded harness to observe synthetic session headers, exact model names, anonymous credential markers, and attempts at the real boundaries.

Cover repeated turns and reconnects, distinct session keys, changed Sub2API accounts, absent/empty/invalid identity, supported header aliases, both a session header and a different cache key, raw/converted Chat and Responses paths, Anthropic Messages through both conversion branches, WS bridge replay, concurrent CPA cold starts, CPA restart, target exclusion/recovery, and post-content stream failure. Characterize current precedence for multiple explicit signals before changing builders; preserve that precedence, not whichever field happens to be easiest to forward.

Do not require two arbitrary different keys to choose different targets: use a fixed set against the real native ring. Do not interpret mock affinity as measured upstream cache improvement. Acceptance evidence contains synthetic IDs and sanitized fields, never private credentials or real prompts. Existing correlated request logs can supplement evidence, but a new observability service is unnecessary.

## Risks / Trade-offs

- **Client does not resend a stable ID after reconnect** -> Keep existing request behavior, document caller-key fallback when the downstream header is absent, and do not add ID generation or claim session-level distribution.
- **A conversion path overwrites the explicit value** -> Assert the actual HTTP request received after each builder/conversion, including account changes and WS turns.
- **Provider rejects an extra session header** -> Scope propagation to the opted-in gateway API-key accounts; keep unrelated accounts and direct OAuth behavior unchanged.
- **Native recovery reduces cache locality** -> This is the accepted availability/priority policy; no fallback-retention state is added.
- **Enabling CPA affinity changes global credential selection** -> Verify the effective switch and one-hour TTL plus unrelated provider/protocol selection; treat rollout as a new in-memory binding epoch, not continuity of prior round-robin or heuristic choices.
- **Unbounded distinct session IDs grow CPA's TTL map** -> Reuse the 255-byte validation boundary, retain existing API-key admission limits, and record this accepted residual risk; do not add a new cache implementation in this change.
- **Concurrent cold starts or CPA restart lose immediate credential continuity** -> Document and test native convergence; do not add locks, persistence, or distributed state.
- **Missing-ID requests concentrate behind a shared caller key** -> Explain the native fallback explicitly and verify it offline; distinct-session distribution requires explicit identities.
- **A newer release differs from the audited source** -> Pin and verify the actual selected release; do not apply evidence from the older local checkout or stale graph index blindly.
- **A required Sub2API patch creates maintenance overhead** -> Prefer an upstream contribution/release and a narrowly scoped compatibility change; avoid accumulating private routing logic.

## Migration Plan

1. In the separately requested apply phase, confirm the affected Sub2API source workspace, current image/config baseline, actual conversational endpoint paths, current production AISIX route inventory, and CPA effective routing state. If an upstream release already satisfies the contract, consume it rather than maintain a duplicate patch.
2. Implement and verify any missing Sub2API propagation in an explicitly authorized source workspace. Keep the gateway changes limited to consuming the exact result, native AISIX configuration, documentation, and offline checks.
3. When a custom candidate is necessary, publish through GitHub Actions/GHCR with a fixed readable tag and recorded source revision/digest. Do not compile production images on the small production host. Publication and source-repository operations require their own authorization.
4. Run the candidate with mock upstreams first. Save reproducible commands and sanitized outcomes; do not send provider inference requests to demonstrate affinity, balancing, cooldown, or caching.
5. Obtain explicit approval for the exact production image, AISIX resource diff, and CPA routing diff. Snapshot prior image references and complete relevant configurations, validate the candidate configuration, and use the established safe in-place application procedure. Check service health/reachability and sanitized affinity evidence without claiming a provider cache benchmark.
6. If approved application regresses behavior, restore the recorded image/config pair using the approved rollback scope. Do not clean up old candidates or touch unrelated services without confirmation.

## Source References

- [Sub2API explicit session extraction](https://github.com/Wei-Shaw/sub2api/blob/5de5e2bed035d43591a2e10e51f420ef6a84eb98/backend/internal/service/openai_gateway_scheduling.go)
- [Sub2API raw Chat builder](https://github.com/Wei-Shaw/sub2api/blob/5de5e2bed035d43591a2e10e51f420ef6a84eb98/backend/internal/service/openai_gateway_chat_completions_raw.go)
- [Sub2API outbound header handling](https://github.com/Wei-Shaw/sub2api/blob/5de5e2bed035d43591a2e10e51f420ef6a84eb98/backend/internal/service/openai_gateway_forward.go#L1407-L1448)
- [Sub2API bridge conversion](https://github.com/Wei-Shaw/sub2api/blob/5de5e2bed035d43591a2e10e51f420ef6a84eb98/backend/internal/service/openai_ws_http_bridge.go)
- [Sub2API Anthropic Messages conversion](https://github.com/Wei-Shaw/sub2api/blob/5de5e2bed035d43591a2e10e51f420ef6a84eb98/backend/internal/service/openai_gateway_messages.go)
- [AISIX native selection](https://github.com/api7/aisix/blob/adcf0523b9f84deed64fbdea311df292c5bc541b/crates/aisix-proxy/src/routing.rs)
- [AISIX forwarded-header policy](https://github.com/api7/aisix/blob/adcf0523b9f84deed64fbdea311df292c5bc541b/crates/aisix-gateway/src/upstream_headers.rs)
- [CPA session extraction](https://github.com/router-for-me/CLIProxyAPI/blob/7fac6b15bcfe5ea55c18c9eaec8e5b7e6457d974/sdk/cliproxy/auth/selector.go)
- [CPA credential-affinity configuration](https://github.com/router-for-me/CLIProxyAPI/blob/7fac6b15bcfe5ea55c18c9eaec8e5b7e6457d974/sdk/cliproxy/service_config.go)
- [CPA process-local session cache](https://github.com/router-for-me/CLIProxyAPI/blob/7fac6b15bcfe5ea55c18c9eaec8e5b7e6457d974/sdk/cliproxy/auth/session_cache.go)
- [CPA capability-gated cache-key handling](https://github.com/router-for-me/CLIProxyAPI/blob/7fac6b15bcfe5ea55c18c9eaec8e5b7e6457d974/internal/runtime/executor/openai_compat_executor.go#L873-L915)
