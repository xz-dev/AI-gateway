## Why

Users can wait through Sub2API recovery attempts without receiving a clear, protocol-correct final failure. The initial report mentioned a default of 3, but the approved live inspection found explicit same-account retry count 1 on all four affected accounts; the operator confirmed preserving 1 while removing the broad status override. The operator subsequently clarified the delivery goal: reuse an existing failure classification when available; otherwise send one clear generic failure. Exact upstream wording and a general-purpose classification/redaction system are not required.

## What Changes

- Trace synthetic HTTP, SSE, and WebSocket/http-bridge failures from the immediate upstream through Sub2API to the actual Pi error parser, locating where a final failure becomes missing, malformed, or delayed.
- Keep the verified same-account retry setting of 1 and native pre-output recovery. Record what the setting counts, distinguish retries from total attempts, and remove only confirmed overly broad account-level status overrides in favor of the selected Sub2API version's defaults. Do not replace them with another catch-all status list.
- For a non-recoverable failure or exhausted recovery budget, promptly emit a protocol-correct terminal error carrying a stable non-empty code and clear safe explanation, then finish the response. Reuse existing native classifications and fixed descriptions; unrecognized failures use an honest generic gateway failure without copying unknown upstream text.
- Preserve the selected failure through retry/failover exhaustion instead of losing it to a later generic no-account or stream-failed wrapper. Do not expose intermediate terminal errors while transparent recovery can still succeed.
- Distinguish errors before response commitment from errors after HTTP 200 or WebSocket upgrade. Handle clean premature EOF and transport exceptions explicitly; never synthesize successful completion for a failed/truncated stream or replay generated content.
- Retain deliberate public authentication/not-found opacity and secret redaction. Error transparency means actionable failure information for authorized inference requests, not raw upstream bodies, credentials, internal addresses, or stack traces.
- Verify error content, actual attempt counts, retry decisions, and time from final failure to client termination with offline mock traffic. Do not consume provider inference quota to demonstrate failure behavior.

No new retry engine, monitoring service, progress protocol, blanket error passthrough, Pi UI rewrite, or global long-generation timeout is planned. Normal waiting without a known failure is not conflated with failure suppression.

## Capabilities

### New Capabilities

- `client-visible-upstream-errors`: Useful, safe final inference errors and timely request termination across Sub2API HTTP/SSE/WS paths, while preserving bounded native recovery and using default retry policy instead of broad account overrides.

### Modified Capabilities

None. This adds a client-visible failure contract without changing AISIX's existing bounded-fallback/no-replay requirements or the separate session-affinity proposal.

## Impact

- Sub2API: account error-handling/recovery overrides, error-passthrough rules, failure classification, final error writers, and HTTP/SSE/WS bridge finalization. Prefer verified native configuration before a small upstreamable code correction at a shared failure boundary.
- AI-gateway: sanitized configuration reconciliation, focused mock acceptance, exact-version integration, and release/application guidance. Existing AISIX/CPA retry budgets and topology remain unchanged; observe their contribution to total attempts rather than silently retune them.
- Pi: acceptance target for both error message content and turn termination. Relevant formatter behavior exists in the inspected .155/.156 source revisions; the executing client version must be pinned during verification.
- Other hops: compare keepalive and gateway boundaries to avoid attributing their intentional sanitization or transport behavior to Sub2API. A necessary fix outside Sub2API/gateway configuration requires evidence and separate scope approval, not a silent expansion.
- The operator authorized local reproduction and necessary Sub2API fixes in `/home/xz/Code/ai/sub2api-worktrees/fix-error-propagation`, a detached worktree of the v0.2.4 source pin. Publication, production configuration edits, and deployment still require separate authorization. Both other changes and all main specs remain untouched.
