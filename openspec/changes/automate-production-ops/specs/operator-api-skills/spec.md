## Purpose

Provide reusable guidance for occasional AI-assisted Sub2API administration through supported APIs, without duplicating the application's management logic in deployment automation.

## ADDED Requirements

### Requirement: Skills use version-matched supported management APIs

The repository SHALL provide an operator API skill beginning with Sub2API. Each supported operation SHALL document its purpose, exact target identification, authentication prerequisites, version-matched API contract, verification, and recovery limits. Account routing/mapping and account pool/retry edits SHALL use this API workflow rather than Ansible business-state reconciliation. The skill SHALL NOT guess API fields, manipulate application tables directly, or replace the existing panel. Unsupported operations SHALL stop with the missing contract identified.

#### Scenario: Request an account retry-setting change

- **WHEN** the operator asks the AI to adjust a Sub2API account's retry setting
- **THEN** the skill identifies the account and deployed version and uses the supported field semantics for that version
- **AND** no Ansible-managed account snapshot or SQL mutation is introduced

### Requirement: Inspection is distinct from authorized mutation

The skill SHALL default to reading and proposing. Before a write it SHALL present exact account or resource identities, owned field changes, effects, and recovery limits, and require explicit approval. A request to inspect or answer a question SHALL NOT authorize mutation. Infrastructure changes, new management exposure, restarts, and billable inference tests SHALL remain separate from approval of a management-field edit.

#### Scenario: Operator asks why a model is missing

- **WHEN** the AI inspects relevant account mappings, group entitlements, and served catalog
- **THEN** it reports observed differences without changing mappings, restarting services, or creating an inference request

### Requirement: Updates preserve unrelated and concurrent state

Immediately before an approved update, the skill SHALL read the latest resource and confirm the owned fields still match the reviewed baseline. It SHALL preserve unrelated fields, including credentials and peer pool/retry settings. Where the API replaces a nested object, the skill SHALL construct that object from the latest complete representation rather than assume partial merging. It SHALL use supported concurrency controls where available; without them, it SHALL disclose the limitation and require a non-overlapping edit window. Unknown replacement semantics or incomplete required state SHALL block the write.

#### Scenario: Change routing without resetting peer edits

- **GIVEN** an account's approved change affects only its routing destination and a peer has changed its pool settings
- **WHEN** the skill prepares the update from the latest complete resource
- **THEN** the peer settings and credentials are preserved
- **AND** unexpected drift in the owned routing fields stops the update for renewed review

### Requirement: Verification and recovery are field-scoped and secret-safe

The skill SHALL read back the edited resource and verify the intended values, reporting relevant served-catalog observations separately from inference success. On a timed-out write it SHALL observe the outcome before retrying; it SHALL NOT blindly repeat a possibly completed mutation. Recovery SHALL restore only approved owned fields against fresh state or stop on conflict. Credentials, tokens, raw private objects, and secret-bearing commands SHALL NOT enter public examples, reports, or durable AI context. Authentication failure SHALL stop without bypassing the required login or authorization.

#### Scenario: Update response is lost

- **WHEN** the connection times out after an approved field update
- **THEN** the skill reads the resource to determine whether the update took effect before proposing another write

#### Scenario: Authentication is unavailable

- **WHEN** the required admin login or token is unavailable or rejected
- **THEN** the skill requests operator authentication without switching to database access or exposing credentials
