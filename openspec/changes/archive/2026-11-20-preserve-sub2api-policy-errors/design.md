## Context

Production Sub2API is the `f249326936c2d76e90e2e2fc67869c01564d62d6` fork image. `openai_gateway_upstream_errors.go` can send a normal 403 into account failover; `openai_gateway_handler.go` maps exhausted 403 to 502/`upstream_error`. `handleCompatErrorResponse` and `writeChatCompletionsError` reduce a 400 to type/message. The Responses deterministic-400 writer already supports structured code/type/param. Existing `cyber_policy` handling has earlier, protocol-specific behavior and must not regress.

The live account-10 path has platform `openai`, WebSocket mode `off` and no `openai_passthrough` flag. Its two error rules cover 429/5xx and 404/409/422, not 400/403. APISIX intentionally blanks 401/404 and clears their headers; that explains the public invalid-model observation but does not explain policy classification loss. The owner approved this repair and gated delivery on 2026-11-19.

## Goals / Non-Goals

**Goals:** introduce one small request-policy classification shared by retry decisions and protocol writers; preserve safe meaning before any account side effect; prove exact fields and absence of replay/penalties with harmless mock responses.

**Non-Goals:** general upstream-text parsing, universal 403 passthrough, broader retry redesign, new settings/dependencies/migrations, account/group edits, APISIX changes, real policy-content generation, or closing unrelated OpenSpec acceptance.

## Decisions

### A finite structured contract, not a text heuristic

Use HTTP 400/403 and the explicit OpenAI error envelope's code/type fields. The initial marker set is `content_filter`, `content_policy`, `content_policy_violation`, `content_moderation`: existing structured policy literals in production's `grok_upstream_errors.go:96–107`, excluding its provider-specific `new_sensitive` and separately handled `cyber_policy`. Normalize only whitespace/case; do not search message text or arbitrary descendants.

A non-empty unrecognized code must not be overridden by a policy-looking type. Preserve existing credential/access/endpoint classification precedence on conflicts. Code-absent recognized policy types can supply the missing stable policy code. Safe output types are the four policy markers plus `invalid_request_error` and `permission_error`; preserve a provided safe type. For a recognized policy code with missing/unsafe type, use `content_policy`. Preserve a recognized code; otherwise derive it from the recognized policy type. Return the fixed message `Request blocked by upstream content policy.` Never reflect unknown type/code values or the upstream message.

Keep dedicated cyber-policy handling before this new path. It retains its existing contract; this repair does not relabel cyber policy as generic content moderation. Keep platform-specific Grok behavior outside the shared OpenAI-compatible change unless a directly affected call site needs the shared helper without altering its established behavior.

### Decide before retries and side effects

Reuse the existing OpenAI-compatible error pipeline and JSON/SSE writers. Check the shared classification wherever a handled 400/403 would otherwise cause same-account retry, failover, cooldown, disabling or health attribution. Return the same policy result on native Responses and Chat compatibility paths. Do not rely solely on changing the final writer: by then an extra attempt or penalty may already have happened.

Use the existing protocol-correct terminal helpers when headers are committed. Add no dependency, fallback account or new retry budget. Credential/endpoint 403 keeps the baseline's internal semantics; public 401/404 opacity remains intact.

### Validate behavior at the upstream boundary

Use local HTTP mocks with synthetic fixed error objects and harmless prompts. The minimum matrix is HTTP 400/403 × Responses/Chat, with retries and alternate accounts made available so an accidental extra attempt is observable. Assert exact safe code/type and message, one upstream attempt, and no cooldown/disable/health-side-effect calls or account-state changes. Add missing-code, missing/unsafe-type, conflicting-code, malformed/plain-text, unknown-code, credential/endpoint 403 and existing cyber-policy regressions. Cover the selected protocol's terminal error after commitment without pretending a committed HTTP status can change.

## Risks / Trade-offs

- A finite vocabulary intentionally misses unknown future provider codes → keep them under existing non-policy handling; expand only with a sourced fixture and review.
- A writer-only repair can still harm account health → make retry and side-effect counters part of the acceptance seam, not merely test non-empty JSON fields.
- Protocol conversion can silently relabel errors → assert exact values on both endpoints, including committed-stream termination where supported.
- A broad 403 exception could hide credential outages → explicit negative controls and unchanged baseline credential behavior.

## Migration Plan

1. Commit and strictly validate these artifacts before source edits. Branch the Sub2API candidate from the exact deployed fork, without importing unrelated local work.
2. Implement and pass mock regressions and existing cyber/credential/capacity checks. Publish through the owned fork's existing CI/image workflow; verify source revision, digest and runtime identity.
3. Run the established compatibility ladder against mocks and update the lock only from its passing receipt. Obtain independent review of the exact candidate and gateway integration; resolve blocking findings before activation.
4. Take a fresh Sub2API-only guarded preview, require the reviewed digest and use the existing backed-up apply/rollback path. Preserve all business settings and neighbouring container identities. Verify readiness/image/config and approved zero-token errors only; do not send actual policy content or add a temporary production route/key.
5. Record mock evidence separately from zero-token production observations. Roll back the image if candidate-caused health, isolation or request-contract checks fail. Complete tasks only with evidence, synchronize this non-retired delta on archive, validate and push.
