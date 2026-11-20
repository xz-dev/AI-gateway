## ADDED Requirements

### Requirement: CPA-backed fallbacks have native capability evidence

An AISIX resource rollout MUST check every explicit secondary CPA-backed target against a fresh native CPA inventory, using the upstream model ID after resolving the AISIX target. A merged or supplemented gateway catalog MUST NOT count as registration evidence. Preview and apply MUST reject missing evidence or an unregistered fallback before uploading resources. Keep the existing primary and wildcard semantics.

#### Scenario: Missing fallback is rejected before upload
- **WHEN** a candidate declares a CPA-backed secondary target absent from the native inventory
- **THEN** validation rejects the candidate and identifies the aggregate and missing target

#### Scenario: XL inventory is limited to Claude
- **WHEN** AISIX loads the corrected resources
- **THEN** its only explicit XL declarations are Claude models, neither XL GPT nor XL Grok remains a route target, and the public `gpt-6.2-sol` retains its Codex primary

#### Scenario: Fable 5.2 replaces the explicit AISIX 5.1 route
- **WHEN** an operator reads back the effective AISIX inventory and routing
- **THEN** bare `claude-fable-5-2` targets the registered `xl/claude-fable-5-2` and no explicit Fable 5.1 declaration or target remains in AISIX

#### Scenario: Silent fallback removal does not mask overload
- **WHEN** the Codex primary returns 503 or is already cooling, and the corrected aggregate has no alternate
- **THEN** the client receives a failover-eligible 5xx (502 or 503), never 400, and AISIX does not call the removed target even if the mock upstream would return `model_not_found` with HTTP 400 for it
