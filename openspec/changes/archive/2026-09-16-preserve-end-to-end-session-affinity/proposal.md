## Why

Clients can already supply stable conversation identifiers, but Sub2API's conversational inference paths do not consistently carry the same identifier to AISIX and CPA. Reusing that identity across reconnects can reduce unnecessary logical-target and credential changes without inventing a new session service or promising upstream cache hits.

## What Changes

- Preserve one validated explicit client session identity through every affected conversational inference path from Sub2API to AISIX and CPA: Chat Completions, Responses conversion and passthrough, Responses WebSocket through HTTP bridge, and Anthropic Messages conversion to Responses or Chat Completions.
- Use the existing `session_id` header as the downstream interoperability contract. Reuse the current explicit-session precedence, including Claude Code's explicit session input, rather than generate a new identity or derive one from changing prompt content.
- Configure only logical routes that currently balance multiple targets within the same active priority tier to use AISIX native `consistent_hash`. Preserve target membership, weights, priority tiers, retry limits, cooldown policy, and ordered-failover routes such as `memory-consolidation`.
- Forward only `session_id` from AISIX to CPA and explicitly enable CPA credential session affinity with a one-hour inactivity lifetime. Do not forward caller credentials.
- Treat invalid explicit identifiers consistently as absent for affinity, preserve existing cache-key and protocol behavior, and document that affinity state is in memory, may cold-start concurrently, and is lost on CPA restart.
- Verify the contract with synthetic requests and mock upstreams, including reconnects, missing or invalid identity, multiple sessions, concurrent cold starts, target recovery, process restart, and stream-failure cases. Do not spend provider inference quota on routing demonstrations.

No content-prefix routing, new API-key/user namespace, session-to-target database, fallback-retention state, routing sidecar, or prompt-cache-key unification is included. Existing authentication, authorization, quota enforcement, and security protections remain in place.

## Capabilities

### New Capabilities

- `session-affinity-propagation`: Reuse validated explicit client session identity across affected conversational HTTP and WebSocket forwarding paths, and preserve existing cache, history, and credential boundaries.

### Modified Capabilities

- `aisix-model-routing`: Add session-keyed consistent hashing to the supported logical-routing policy while retaining native priority recovery, one CPA pool, exact target names, and bounded pre-content fallback.

## Impact

- Sub2API: explicit-session validation, precedence, and propagation in the affected OpenAI API-key builders, including raw Chat Completions, Chat-to-Responses conversion, Responses, WS HTTP bridge, and Anthropic Messages conversion. The deployed reference is v0.2.4; implementation must use the actual selected release source rather than an older local checkout.
- AI-gateway: current production route/account inventory, AISIX resource examples/configuration guidance, exact-image integration, CPA effective routing configuration, and focused offline acceptance checks. Production resource or CPA configuration changes require a separate approved application step.
- AISIX: native routing and `ProviderKey.request.forward_client_headers` configuration; no routing-core patch planned. CPA: explicitly enabled native session affinity with a one-hour inactivity lifetime; no selector rewrite planned. APISIX: preserve current forwarding and security boundaries; no new state owner.
- External dependency: any necessary Sub2API patch belongs in an explicitly authorized upstream/source workspace, with a reproducible release or candidate image consumed by this repository. Planning here does not authorize sibling-repository edits, publication, or deployment.
- Existing clients without a valid explicit session identity remain accepted and receive no new per-session affinity guarantee. When the downstream session header is absent, hash-enabled routes use their documented native missing-key fallback rather than a new session generator; pre-existing protocol behavior is not removed.
- The unrelated `add-headroom-context-compression` change and all main specs remain untouched during proposal creation.
