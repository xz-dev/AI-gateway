## ADDED Requirements

### Requirement: A gateway-selected missing target is a routing failure

For a valid public aggregate, AISIX SHALL treat a structured upstream rejection identifying the selected internal model or its provider as missing as a gateway routing failure, not a caller error. Before response commitment, a terminal missing-target failure SHALL return 502; if the router determines that no candidate is available, it SHALL return the existing 503 unavailable response. This classification SHALL NOT change retry budgets, route ordering, direct unknown-model errors, or policy error status and body.

#### Scenario: Primary fails and the selected backup model is missing
- **GIVEN** a valid aggregate whose primary returns 503
- **WHEN** the selected backup returns 400 with a structured `model_not_found` error for its internal target
- **THEN** the final response is 502 for the routing failure or 503 for aggregate unavailability, not 400

#### Scenario: Primary is cooling and the selected backup provider is missing
- **GIVEN** the primary is already in cooldown and is skipped
- **WHEN** the selected backup returns a structured 400 identifying its internal provider as unknown
- **THEN** the final response is 502 for the routing failure or 503 for aggregate unavailability, not 400

#### Scenario: No candidate is available
- **WHEN** the aggregate has no available candidate and uses its fail-fast policy
- **THEN** AISIX returns 503 with the existing `all_candidates_unavailable` code and preserves any applicable retry hint

#### Scenario: Client names an unknown public model
- **WHEN** a client requests a model that does not resolve to a public route
- **THEN** the client receives the original 400 or 404 rather than a gateway failure

#### Scenario: Selected target rejects the request under policy
- **WHEN** an aggregate target returns a policy rejection with status 400 or 403
- **THEN** the status and error envelope remain unchanged, including the fields used by Sub2API for policy classification
