# AISIX Model Routing Specification

## Purpose

Route logical models through AISIX over one CPA upstream pool with per-target 429 cooldown isolation, bounded in-request fallback, and verbatim `/v1/responses` passthrough, while Sub2API keeps client authority and CPA keeps provider credentials.

## Requirements

### Requirement: One upstream pool supports explicit logical routing

AISIX SHALL accept logical model IDs from authorized callers and select among their configured concrete CPA targets through one shared CPA provider pool. Each direct AISIX target name SHALL preserve the exact CPA model name without transliteration. AISIX SHALL support ordered in-request failover, configurable same-priority round-robin or session-keyed consistent hashing, and bounded retries per the approved policy. Sub2API SHALL retain client authentication and quota authority; CPA SHALL retain provider credential custody.

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

### Requirement: Session hashing replaces only same-tier dynamic balancing

A logical route SHALL be configured for session affinity only when its current production policy dynamically balances multiple targets within at least one active priority tier. Such a route SHALL use AISIX native consistent hashing with the incoming `session_id` as its first hash source and caller API-key identity as its fallback source. Target membership, priorities, weights, retry limits, cooldowns, exact concrete model names, and fallback bounds SHALL retain their approved values. Hashing SHALL distribute keys within each priority tier, not flatten the tiers. Routes whose policy is ordered failover, including `memory-consolidation`, SHALL retain ordered failover rather than be converted solely for affinity. Missing session identity SHALL use the declared native fallback hash source and SHALL NOT be represented as per-session affinity.

#### Scenario: Healthy conversation remains on its target
- **GIVEN** route membership, weights, eligibility, and priority tiers remain unchanged
- **WHEN** repeated requests for one logical alias carry the same non-empty session identity
- **THEN** native hashing selects the same first target

#### Scenario: Independent sessions can share the active tier
- **GIVEN** the active priority tier contains multiple weighted targets
- **WHEN** requests carry a set of distinct explicit session identities
- **THEN** the native hash ring distributes those identities within that tier according to its configured weights
- **AND** the contract does not require every pair of distinct identities to select different targets

#### Scenario: Preferred tier contains one target
- **GIVEN** an eligible highest-priority tier contains only one target
- **WHEN** requests from multiple sessions use the logical alias
- **THEN** that target remains the preferred target for all of them
- **AND** hashing does not send traffic to lower tiers merely to improve distribution

#### Scenario: Ordered failover remains ordered
- **GIVEN** a logical route expresses a preferred target followed by backup targets using ordered failover
- **WHEN** session affinity is applied to the dynamic same-tier routes
- **THEN** the ordered route retains its failover strategy and declaration-order preference
- **AND** its backup targets do not receive healthy traffic merely to distribute sessions

#### Scenario: Session identity is missing
- **WHEN** a hash-enabled route receives a request without a non-empty session identity
- **THEN** AISIX applies its configured caller-API-key fallback hash source
- **AND** operators are not told that separate sessions behind that shared key are independently distributed

### Requirement: Recovery returns traffic according to native priority and hashing

Session-aware routes SHALL retain AISIX's stateless per-request recovery policy. An eligible recovered preferred target SHALL participate in selection again, including for sessions that previously used a fallback. The gateway SHALL NOT retain a session-to-fallback binding after recovery. Existing pre-content retry limits and the prohibition on replaying generated stream output SHALL continue to apply.

#### Scenario: Preferred target recovers
- **GIVEN** a session normally selects target A and uses B while A is excluded
- **WHEN** A becomes eligible again and the unchanged native route ordering selects A
- **THEN** the session's next request selects A again
- **AND** no fallback-session lifetime must expire first

#### Scenario: Fallback remains bounded before generated content
- **WHEN** the selected target fails before generated content and another target is eligible
- **THEN** AISIX applies its existing within-tier and lower-tier fallback policy within configured budgets
- **AND** selection does not bypass per-target cooldown or introduce additional retry layers

### Requirement: Session identity reaches the credential selector

The CPA provider pool SHALL explicitly forward the agreed `session_id` header on affected standard-protocol inference endpoints, whether the selected logical route uses consistent hashing or ordered failover. AISIX SHALL NOT use an internal `x-aisix-*` header as the sole identity expected by CPA. This forwarding SHALL NOT widen credential forwarding or replace the pool's configured authentication.

#### Scenario: Hash input is available to CPA
- **WHEN** AISIX selects a concrete target for a request carrying a non-empty `session_id`
- **THEN** the upstream inference request carries the same session identity to CPA
- **AND** the configured CPA credential remains in effect

#### Scenario: Client credentials remain private
- **WHEN** the CPA provider pool forwards `session_id`
- **THEN** caller Authorization, API-key, cookie, and unrelated client headers are not forwarded by that configuration

### Requirement: AISIX memory admits one maximum Responses body

The AISIX container SHALL have a 512 MiB memory limit with no swap (`memswap_limit` equal to `mem_limit`). One 64 MiB Responses request SHALL complete under that limit. The tracked AISIX image pin SHALL equal the digest production runs. Concurrent maximum-size requests are not guaranteed: two concurrent 64 MiB requests can exceed the limit, and that residual risk SHALL be recorded in the deployment record.

#### Scenario: One 64 MiB request succeeds
- **WHEN** a single 64 MiB Responses request passes through AISIX `*` to CPA
- **THEN** it succeeds without an OOM kill, and the peak cgroup memory is recorded

#### Scenario: Pin drift is visible
- **WHEN** an operator compares tracked `compose.yaml`, `.env.example` and the running AISIX image
- **THEN** all three name `ghcr.io/api7/aisix:1.8.0-rc.1@sha256:ebcd2bbba9b7a3cc7cfb1318e4d3ba64cf9f93cbf2b19b8486ead6093da1ba5a` until a reviewed upgrade changes them together
