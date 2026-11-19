# Client-Visible Upstream Errors Specification

## Purpose

Give authorized inference clients useful final failure information and promptly terminate failed HTTP, SSE, and WebSocket requests, while retaining bounded native recovery instead of hiding errors behind generic messages or broad account retry overrides.

## Requirements

### Requirement: Final failure preserves an actionable safe cause

When recovery is unavailable or exhausted, Sub2API SHALL send a non-empty machine-readable error code and a clear safe explanation. It SHALL reuse recognized native failure classifications and existing intentional compatibility mappings. An unrecognized failure SHALL produce a stable generic gateway failure rather than missing/null error information or copied unknown upstream text. Exact upstream wording and codes are not required; the implementation SHALL NOT invent an unavailable diagnosis or build a new free-text classification/filtering system to recover one.

#### Scenario: Request fails with a recognized native classification
- **GIVEN** an authorized inference request encounters a failure recognized by existing native handling
- **WHEN** the effective recovery policy decides the request cannot recover
- **THEN** the client receives that safe category, a clear fixed explanation, and a non-null usable code
- **AND** recognized distinctions such as rate limiting and upstream authentication are not erased merely to use one blanket message

#### Scenario: Recovery exhausts after an upstream failure
- **GIVEN** recovery attempts have recorded an actionable failure for the current request
- **WHEN** the budget is exhausted or account selection finds no further candidate
- **THEN** the terminal error retains the selected safe cause and indicates recovery exhaustion
- **AND** a later selection wrapper does not erase that cause or reuse a cause from another request

#### Scenario: Upstream supplies an opaque, malformed, or unrecognized failure
- **WHEN** no reliable native classification is available beyond the observed failure
- **THEN** Sub2API returns an honest non-null generic gateway failure code and a clear fixed safe message
- **AND** unknown upstream code/message fields are not copied, even if they look like valid JSON or plain text
- **AND** it does not invent an upstream HTTP status or claim to know an unavailable root cause

### Requirement: Recovery uses native scope and the current retry setting

The affected accounts SHALL retain their verified explicit same-account retry count of 1, without this change increasing the limit or unsetting the count to obtain the native default of 3. Confirmed broad account-level same-account retry-status overrides SHALL be removed using the selected version's unset/default semantics. Error-handling status filters, account-switch budgets, protocol-specific recovery, and temporary-unschedulable rules SHALL NOT be treated as interchangeable retry lists or silently reset. The effective count of initial attempts, same-account retries, and account switches SHALL be verified separately.

#### Scenario: Broad retry-status override returns to default
- **GIVEN** an affected account has a confirmed broad override of same-account retry statuses
- **WHEN** that override is removed using the supported unset/default operation
- **THEN** same-account status retry eligibility matches the pinned version's native default
- **AND** the retry count, account authentication, and unrelated account rules remain unchanged

#### Scenario: Failure is ineligible for native recovery
- **WHEN** a failure is not eligible for any remaining native recovery on the affected path
- **THEN** Sub2API returns the final failure without consuming unrelated retries or waiting through another backoff

#### Scenario: A permitted retry succeeds
- **GIVEN** a failure qualifies for native pre-output recovery and budget remains
- **WHEN** a subsequent attempt succeeds
- **THEN** the client receives the successful result without a preceding terminal error from a failed attempt
- **AND** actual attempt counts stay within the verified effective policy

### Requirement: Final failures terminate in the client's protocol

Before response commitment, Sub2API SHALL return an appropriate non-success HTTP response in the requested API's error shape. After an SSE response or WebSocket upgrade is committed, it SHALL emit a protocol-correct terminal failure and finish the failed response or turn. Once failure is final, it SHALL flush and terminate without further retry backoff, keepalive intervals, or waiting for a completion event that is not needed to report that failure. Existing legitimate incomplete-response semantics SHALL remain distinguishable from failure.

#### Scenario: HTTP error before response commitment
- **WHEN** a request fails finally before HTTP headers are sent
- **THEN** the client receives the correct safe error classification in a non-success HTTP response
- **AND** provider credential failures are not misrepresented as failure of the client's own authentication

#### Scenario: Error follows a committed keepalive
- **GIVEN** Sub2API has already committed HTTP 200 and a streaming keepalive
- **WHEN** the inference request reaches a final failure
- **THEN** a conforming stream terminal error reports the cause without attempting to change the committed HTTP status
- **AND** the failed stream finishes without a synthetic successful completion or further keepalives

#### Scenario: Upstream sends an error without a later failed-completion event
- **GIVEN** Sub2API has enough information to decide a request has finally failed
- **WHEN** the upstream sends only an error and keeps the connection open
- **THEN** the downstream failure completes without waiting indefinitely for another upstream event

#### Scenario: Valid incomplete response is not converted to failure
- **WHEN** the upstream legitimately terminates with an incomplete response and its reason
- **THEN** the existing incomplete-result semantics remain available to the client
- **AND** Sub2API does not mistake that valid terminal event for a missing-terminal failure

### Requirement: Truncation is not success and output is not replayed

A clean EOF before the required protocol terminal event and an exceptional transport failure SHALL both be handled as incomplete/failed transport when the operation requires a terminal event. Once generated content has reached the client, Sub2API SHALL NOT retry generation or switch accounts to append another generation. Client cancellation SHALL stop further recovery for that request.

#### Scenario: Clean premature EOF
- **WHEN** a stream closes cleanly after partial output but before its required terminal event
- **THEN** the client receives a failure identifying premature termination where the downstream remains writable
- **AND** no synthetic success event conceals the truncation and no new generation starts

#### Scenario: Transport exception after generated output
- **WHEN** the upstream transport fails after generated content was delivered
- **THEN** the failure surfaces on that response without another generation attempt
- **AND** an already disconnected client causes cleanup rather than attempts to send additional errors

#### Scenario: Client cancels during recovery
- **WHEN** the client cancels while Sub2API is waiting for a permitted retry
- **THEN** Sub2API cancels that wait and starts no further attempts for the request

### Requirement: Error transparency preserves security boundaries

Client-visible errors SHALL preserve useful safe meaning while excluding secrets, upstream credentials, prompt contents, private addresses, and stack traces. Existing deliberate public authentication/not-found opacity SHALL remain intact. Detailed operator diagnostics SHALL be correlated using a safe gateway reference rather than raw client-visible upstream dumps.

#### Scenario: Useful message contains a secret or internal address
- **WHEN** an upstream error combines a useful failure category with sensitive details
- **THEN** the client receives the existing safe category and a fixed explanation without copying the sensitive upstream text
- **AND** unsafe fields do not appear in HTTP bodies, SSE/WS failure events, or client-facing diagnostic references

#### Scenario: Public boundary denies access
- **WHEN** the public boundary rejects an unauthenticated or disallowed request under its existing opaque-error policy
- **THEN** that policy remains effective
- **AND** this upstream-error feature does not disclose hidden routes or credential validity

### Requirement: Pi observes failure completion rather than gateway waiting

Acceptance SHALL verify both the wire error and the selected Pi parser's terminal result for the affected HTTP/SSE/WS paths. A gateway-final failure SHALL produce a Pi error with useful safe text rather than only a null-code generic placeholder. Per-request termination SHALL be distinguished from any new request initiated by Pi's own retry policy. This change SHALL NOT claim a bound on normal generation that has produced neither a failure nor a terminal event.

#### Scenario: Parser receives a gateway-final error
- **GIVEN** Pi's additional retry layer is disabled for the per-request acceptance fixture
- **WHEN** it receives a final error produced by the affected gateway path
- **THEN** the parser ends that turn with a non-success result and useful safe error information
- **AND** it does not continue awaiting another completion event

#### Scenario: Client elects to retry a completed failure
- **WHEN** Pi's existing retry policy starts another request after receiving a completed gateway error
- **THEN** evidence identifies it as a new client attempt rather than an unterminated Sub2API request
- **AND** retry counts and elapsed time are not attributed entirely to Sub2API

### Requirement: Recognized content-policy rejection is request-scoped

For OpenAI-compatible Responses and Chat Completions, Sub2API SHALL recognize a finite set of structured content-policy markers on upstream HTTP 400 and 403 responses before any same-account retry, account failover, cooldown, disabling or health penalty. Recognized request-policy rejection SHALL terminate that request without changing credential/account health and without starting another upstream attempt. Classification SHALL use explicitly supported structured fields, not raw-message substrings, arbitrary nested strings or the HTTP status alone.

The supported content-policy markers SHALL be `content_filter`, `content_policy`, `content_policy_violation` and `content_moderation`. Existing dedicated `cyber_policy` handling SHALL remain intact rather than being silently replaced by this classifier. Unknown, malformed, conflicting credential/endpoint classifications and bare 403 responses SHALL retain existing non-policy handling.

#### Scenario: Policy rejection is not retried or penalized
- **GIVEN** either supported endpoint receives an upstream 400 or 403 with a recognized content-policy code
- **WHEN** the gateway handles the failure with retries and alternate accounts otherwise available
- **THEN** it terminates the request with the recognized policy classification after exactly one upstream attempt
- **AND** it does not retry, select another account, cool down or disable the account, or apply a provider-health penalty

#### Scenario: Policy is identified by a structured type when code is absent
- **GIVEN** an upstream 400 or 403 has no error code and its structured error type is a supported content-policy marker
- **WHEN** the gateway handles the failure
- **THEN** the request terminates as a policy rejection with a stable recognized code and that safe policy type
- **AND** it does not infer policy from unrelated free text

#### Scenario: Credential or endpoint denial is not a policy rejection
- **GIVEN** an upstream 403 is an authentication, permission, credential, entitlement or endpoint failure, or lacks a recognized policy classification
- **WHEN** the gateway handles it
- **THEN** its established internal-error, retry/failover and account-health semantics remain unchanged
- **AND** a conflicting or unknown non-empty code is not overridden solely because another field or message mentions policy

### Requirement: Policy meaning survives Responses and Chat conversion

Before HTTP response commitment, a recognized policy rejection SHALL retain its HTTP 400 or 403 status and return an API-compatible JSON error containing non-empty safe `code`, `type` and a fixed explanation. Recognized safe upstream classification values SHALL be preserved rather than replaced with generic `upstream_error` or a different generic type. Unknown values SHALL NOT be copied into client-visible classification fields. Missing or unsafe classification fields SHALL receive a documented safe policy-specific fallback without losing the known policy meaning. Credentials, addresses, prompts, provider text and other upstream details SHALL NOT be copied into the explanation.

After response commitment, the rejection SHALL terminate in the selected protocol's error shape with the same safe policy meaning; it SHALL NOT fabricate HTTP 400/403 after HTTP 200 is committed, wait for a later completion, or start another generation. Existing public authentication/not-found opacity SHALL remain unchanged.

#### Scenario: Preserve exact safe code and type on both endpoints
- **GIVEN** an upstream policy response contains `code: content_policy_violation` and `type: permission_error`
- **WHEN** either Responses or Chat Completions returns the final uncommitted HTTP error
- **THEN** its status remains the original 400 or 403 and its JSON error retains those exact safe code and type values with the fixed policy explanation
- **AND** unknown upstream fields or message contents are not exposed

#### Scenario: Complete missing policy fields without copying unsafe values
- **GIVEN** the gateway recognizes policy but one classification field is missing or outside the safe finite vocabulary
- **WHEN** it forms the client error
- **THEN** both code and type are non-empty documented safe policy classifications
- **AND** it does not copy the unknown value or erase a recognized safe value

#### Scenario: Policy failure after a committed stream
- **GIVEN** a Responses or Chat stream has committed its HTTP response before the policy failure becomes final
- **WHEN** the gateway terminates the request
- **THEN** the protocol-correct terminal error carries the safe policy code/type and fixed explanation
- **AND** no retry, account penalty or synthetic success follows

#### Scenario: Preserve existing exceptional and public-boundary handling
- **WHEN** the existing `cyber_policy` handling or public APISIX 401/404 boundary is exercised
- **THEN** its established behavior and protection remain intact
- **AND** this change does not broadly expose upstream credential 403 responses or disable public error opacity
