## Why

Sub2API can turn a request-level content-policy 403 into a retryable 502, while its Chat compatibility writer drops the policy code and rewrites the type. A downstream client can then retry, switch credentials or penalize a healthy provider instead of stopping the rejected request.

## What Changes

- Classify a finite set of structured content-policy errors before account retries, failover or account-health side effects.
- Preserve recognized safe policy code/type values across Responses and Chat Completions, with a fixed explanation rather than raw upstream text.
- Keep credential/endpoint denials, existing `cyber_policy` handling and the public APISIX 401/404 opacity unchanged.
- Prove the behavior with mock-only HTTP/protocol regressions, then deliver through the existing fork CI/image, compatibility ladder/lock, independent review and guarded Sub2API rollout.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `client-visible-upstream-errors`: add a request-scoped policy contract that survives protocol conversion without credential failover or account-health penalties.

## Impact

The change affects the Sub2API fork's shared OpenAI-compatible error classification, retry/side-effect decisions, Responses/Chat writers and their tests. Gateway integration changes are limited to the reviewed Sub2API image/compatibility lock, this specification and a sanitized deployment record. No schema migration, business-setting edit, new key, new production route or APISIX policy change is planned. The owner approved implementation and the gated delivery sequence on 2026-11-19; no real policy content may be used for acceptance.
