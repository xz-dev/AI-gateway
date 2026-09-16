# Session Affinity Propagation Specification

## Purpose

Carry a validated explicit client session identity across the gateway's conversational inference paths so native logical-target and credential affinity can cooperate across reconnects without introducing another session identity or history service.

## Requirements

### Requirement: Existing explicit identity is reused without a new namespace

For an authorized request carrying a valid explicit session identity recognized by the supported request path, the gateway SHALL reuse that identity for affinity on the Sub2API-to-AISIX-to-CPA path. Supported inputs SHALL include the existing OpenAI-compatible session inputs and Claude Code's explicit session input on Anthropic Messages. The gateway SHALL treat invalid UTF-8, control characters, an empty value, or a value longer than 255 bytes as absent for affinity. It SHALL NOT add an API-key/user namespace, replace a valid identity with a random per-request value, or derive a propagated explicit identity from changing prompt content. Existing request validation and protocol-required transformations outside this path SHALL remain intact.

#### Scenario: Conversation continues across turns
- **GIVEN** a client supplies the same explicit session identity on two requests for the same logical model
- **WHEN** the conversation input grows between those requests
- **THEN** AISIX and CPA receive the same explicit affinity identity on both requests
- **AND** the gateway does not replace it because the prompt changed

#### Scenario: Upstream account selection changes
- **GIVEN** two eligible Sub2API API-key accounts connect to the same AISIX routing service
- **WHEN** an authorized conversation continues through a different account
- **THEN** the client-supplied affinity identity reaching AISIX and CPA remains unchanged
- **AND** the selected account still supplies its own correct upstream authentication

#### Scenario: Explicit identity is absent
- **WHEN** an otherwise valid request has no recognized explicit session identity
- **THEN** this propagation feature neither rejects it solely for that absence nor generates an identity
- **AND** configured native fallback behavior may apply when the downstream session header is absent
- **AND** no new per-conversation affinity guarantee is made

#### Scenario: Explicit identity is invalid for affinity
- **WHEN** a request supplies an explicit session candidate containing invalid UTF-8, a control character, or more than 255 bytes
- **THEN** the gateway does not propagate that candidate as the affinity identity
- **AND** the otherwise valid request follows the same missing-identity behavior

### Requirement: Affected conversational paths propagate the same identity

The affected OpenAI API-key forwarding paths SHALL carry the resolved explicit identity in the downstream `session_id` header: raw Chat Completions, Chat-to-Responses conversion, Responses forwarding, WebSocket Responses through HTTP bridge, and Anthropic Messages conversion to Responses or Chat Completions. A conversation continuing over an already supported path SHALL NOT lose its identity merely because that path filters client headers, converts protocol shape, or builds a new upstream request. Non-generative operations such as token counting and unrelated media endpoints are outside this requirement.

#### Scenario: Equivalent OpenAI HTTP requests use different forwarding paths
- **GIVEN** each request contains the same recognized explicit session identity
- **WHEN** requests pass through raw Chat Completions, Chat-to-Responses conversion, or Responses forwarding to AISIX
- **THEN** each downstream request carries that identity in `session_id`
- **AND** request bodies retain their existing protocol-specific conversion behavior

#### Scenario: Anthropic Messages converts to an OpenAI protocol
- **GIVEN** an Anthropic Messages request routed through an affected OpenAI API-key account supplies the same Claude Code or other supported explicit session identity on successive turns
- **WHEN** Sub2API converts those turns to Responses or Chat Completions for AISIX
- **THEN** each converted inference request carries that identity in `session_id`
- **AND** existing Messages cache-control, metadata, model mapping, and response-conversion behavior remains intact

#### Scenario: WebSocket turn becomes an HTTP request
- **GIVEN** a WebSocket conversation supplies an explicit session identity through its supported session input
- **WHEN** Sub2API forwards successive turns through HTTP bridge
- **THEN** every resulting HTTP inference request carries the resolved conversation identity to AISIX
- **AND** an upstream retry does not generate a different identity for that turn

#### Scenario: Client reconnects with the same identity
- **GIVEN** an affected HTTP or WebSocket client disconnects and later creates a new request or connection
- **WHEN** it resends the same valid explicit session identity for the same logical model
- **THEN** the new downstream inference request carries the same `session_id`
- **AND** no connection-local identity is substituted for it

### Requirement: Cache keys and history retain their own semantics

The gateway SHALL NOT overwrite or introduce `prompt_cache_key`, cache-retention fields, or provider conversation fields solely to make them equal to the affinity header. Existing HTTP-bridge replay and `previous_response_id` handling SHALL remain effective. Reusing a value already recognized by the existing explicit-session extractor SHALL NOT authorize modifying the corresponding body field.

#### Scenario: Routing identity and prompt cache key differ
- **GIVEN** a request supplies an explicit session header and a different supported `prompt_cache_key`
- **WHEN** the request traverses the affected path
- **THEN** affinity propagation does not replace either value with the other
- **AND** existing provider-specific cache-field handling remains unchanged

#### Scenario: Bridge replays a continuation
- **GIVEN** HTTP bridge has sufficient history to reconstruct a continuation using `previous_response_id`
- **WHEN** it creates the replay request
- **THEN** existing replay and continuation-field removal rules still apply
- **AND** replay preserves the resolved affinity identity and the existing tool-call relationships

### Requirement: Authentication and provider custody remain independent of affinity

Session identity SHALL remain a routing hint, not authorization. Sub2API SHALL continue authenticating and authorizing each request and enforcing quotas. AISIX and CPA SHALL use their configured upstream credentials rather than a forwarded client credential. This change SHALL NOT remove existing credential isolation or provider-required identity handling outside the affected path.

#### Scenario: Session identifier does not grant access
- **WHEN** a request presents a known session identity without valid authorization for its requested model
- **THEN** existing authentication and authorization checks deny access
- **AND** the identity does not bypass quota or model-access controls

#### Scenario: Forwarding preserves credential boundaries
- **WHEN** an authorized session request reaches CPA through AISIX
- **THEN** CPA receives the configured AISIX-to-CPA credential and the agreed session identity
- **AND** affinity forwarding does not expose or substitute the caller's Authorization, API key, or cookies

### Requirement: CPA native credential affinity is explicitly enabled

CPA SHALL enable its native session-affinity selector with a one-hour inactivity lifetime before this change is deployed. CPA SHALL continue owning credential selection, binding refresh, eligibility, invalidation, and fallback identity behavior. With an unchanged provider/model and an eligible existing binding, successive requests carrying the same resolved session identity SHALL reuse that binding. The deployment SHALL NOT proceed when the effective CPA configuration does not confirm the enabled selector and lifetime. This change SHALL NOT introduce a second credential-affinity store or treat a credential-affinity hit as proof of upstream prompt caching.

#### Scenario: Effective configuration is incomplete
- **WHEN** the candidate environment reports CPA session affinity disabled or an effective lifetime other than one hour
- **THEN** composed-path acceptance and production deployment remain blocked

#### Scenario: Bound credential remains eligible
- **GIVEN** CPA has an eligible credential bound to a session for a provider and model
- **WHEN** another request with that identity reaches the same provider and model within one hour of activity
- **THEN** CPA uses and refreshes its existing binding under the native affinity policy
- **AND** any reported credential-affinity hit is distinct from provider-reported cached-token usage

#### Scenario: Binding can no longer be used
- **WHEN** a bound credential becomes unavailable or its binding expires
- **THEN** CPA applies its existing eligibility and reselection behavior
- **AND** the gateway adds no separate credential recovery state

#### Scenario: CPA restarts
- **GIVEN** a session was bound to a credential before CPA stopped
- **WHEN** CPA restarts and receives the session's next request
- **THEN** it creates a binding using the eligible credentials available after restart
- **AND** no guarantee is made that the pre-restart credential is recovered

#### Scenario: First requests race before a binding exists
- **GIVEN** no CPA binding exists for a valid session, provider, and model
- **WHEN** multiple first requests for that tuple execute concurrently
- **THEN** the native selector may temporarily choose different eligible credentials
- **AND** this change adds no per-session serialization or distributed lock
- **AND** a later request uses the current successful binding while it remains eligible
