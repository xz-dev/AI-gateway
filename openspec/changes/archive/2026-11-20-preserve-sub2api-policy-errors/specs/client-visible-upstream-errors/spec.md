## ADDED Requirements

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
