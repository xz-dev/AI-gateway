## Purpose

Route logical models through AISIX over one CPA upstream pool with per-target 429 cooldown isolation, bounded in-request fallback, and verbatim `/v1/responses` passthrough, while Sub2API keeps client authority and CPA keeps provider credentials.

## ADDED Requirements

### Requirement: One upstream pool supports explicit logical routing

AISIX SHALL accept logical model IDs from authorized callers and select among their configured concrete CPA targets through one shared CPA provider pool. Each direct AISIX target name SHALL preserve the exact CPA model name without transliteration. AISIX SHALL support ordered in-request failover, same-priority round-robin, and bounded retries per the approved policy. Sub2API SHALL retain client authentication and quota authority; CPA SHALL retain provider credential custody.

#### Scenario: Logical model selects a concrete target
- **WHEN** an authorized client requests a configured logical model
- **THEN** the request reaches an eligible concrete CPA target with only the model field rewritten to the concrete target ID
- **AND** other request fields pass through unmodified
- **AND** the direct AISIX target name is byte-identical to the CPA model name

#### Scenario: Route inventory has an unresolved discrepancy
- **WHEN** a required target, ordering, or cooldown policy cannot be represented or verified
- **THEN** production cutover stays blocked until the operator resolves it
- **AND** no silent best-effort translation is accepted

### Requirement: Rate-limit cooldown is isolated per target

An eligible target-level 429 SHALL put that target in cooldown for the configured duration (honoring a clamped upstream `Retry-After`), while targets sharing the same CPA endpoint/key, and unrelated logical models, remain routable. Cooldown SHALL be independent of the retry policy. After expiry the target SHALL be eligible again.

#### Scenario: One target is rate limited
- **WHEN** target A returns 429 while fallback B and unrelated target C share the same CPA provider pool
- **THEN** the eligible request falls back to B within the same request
- **AND** the next request skips A while A is cooling
- **AND** C remains routable

#### Scenario: Retry-After drives cooldown duration
- **WHEN** A's 429 carries `Retry-After: N`
- **THEN** A's cooldown is N seconds clamped to the configured maximum, and A is eligible again after it expires

#### Scenario: All eligible targets are cooling
- **WHEN** no eligible target remains
- **THEN** the request fails fast with an explicit bounded error
- **AND** unrelated model groups are unaffected

### Requirement: Responses passthrough preserves protocol fidelity

AISIX SHALL forward `/v1/responses` requests to the CPA OpenAI-compatible upstream as `/v1/responses` without protocol conversion. Streaming responses SHALL deliver the upstream SSE event sequence verbatim to the client, including reasoning and encrypted-content item types.

#### Scenario: Streaming Responses request
- **WHEN** a client sends a streaming `/v1/responses` request
- **THEN** the upstream path is `/v1/responses` (not chat completions)
- **AND** the client receives the upstream SSE events in order and unmodified apart from transport

#### Scenario: Non-streaming Responses request
- **WHEN** a client sends a non-streaming `/v1/responses` request
- **THEN** the response preserves the Responses object shape from upstream

### Requirement: Retries stay bounded and never replay generated output

The path SHALL enforce configured retry and fallback budgets. It SHALL NOT retry or switch targets after generated stream content has reached the client.

#### Scenario: Exhausted pre-content failures
- **WHEN** every attempted target fails before generated content
- **THEN** total upstream attempts stay within the configured bounds and the final failure is observable

#### Scenario: Stream fails after output began
- **WHEN** a target fails after streaming content to the client
- **THEN** the failure surfaces to the client without another generation attempt
