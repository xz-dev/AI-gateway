## ADDED Requirements

### Requirement: Compose provider selection preserves the canonical engine
The shared preflight SHALL distinguish the Compose provider command from the container engine. Automatic selection SHALL prefer usable Docker with Compose, then usable Podman with its Compose plugin or standalone provider. Explicit provider and runtime overrides SHALL retain precedence. An effective provider containing no command token SHALL fail with a named prerequisite error before argv construction. Selection SHALL NOT expand the set of supported component transactions.

#### Scenario: Docker is preferred when both engines are usable
- **WHEN** automatic detection finds usable Docker Compose and Podman
- **THEN** the provider is `docker compose` and the engine is `docker`

#### Scenario: Podman plugin is available
- **WHEN** Docker is unusable and Podman supplies its Compose plugin
- **THEN** the provider is `podman compose` and the engine is `podman`

#### Scenario: Standalone Podman Compose is available
- **WHEN** Docker is unusable and Podman uses standalone `podman-compose`
- **THEN** the provider remains `podman-compose` and the engine is `podman`

#### Scenario: Empty extra-vars request automatic selection
- **WHEN** the operator supplies empty provider and runtime values as extra-vars
- **THEN** automatic detection supplies the effective provider to shared argv and prerequisite checks without treating the empty input as an executable

#### Scenario: Explicit provider and runtime override detection
- **WHEN** the operator supplies a nonempty provider command and a declared runtime
- **THEN** those values are used without probing unrelated providers

#### Scenario: No usable provider exists
- **WHEN** neither supported engine has a usable provider
- **THEN** preflight stops with a named missing-prerequisite error and no deployment mutation

#### Scenario: Effective provider is whitespace only
- **WHEN** an explicit provider contains no command token
- **THEN** preflight stops with a named Compose-provider prerequisite error rather than an empty-sequence template failure

#### Scenario: Detection runs in check mode
- **WHEN** automatic preflight runs with `--check`
- **THEN** read-only selection produces the same provider and engine while creating no target state directory, uploading no deployment files and activating no service

### Requirement: Optional Admin probe outcomes are truthful and sanitized
The AISIX optional Admin command probe SHALL distinguish command success, unavailable execution and other command failure without exposing credentials or response content. Nonzero outcomes SHALL remain nonfatal and SHALL leave `admin_api_checked` false. A zero exit SHALL retain the existing command-probe receipt meaning and SHALL NOT be reported as skipped/failed or as proof of effective route/model equivalence. Check mode SHALL skip the probe and its apply receipt.

#### Scenario: Command probe succeeds
- **WHEN** the optional command returns zero
- **THEN** diagnostics report command-probe success, the receipt records `admin_api_checked=true`, and no route-equivalence claim is made

#### Scenario: Command is unavailable
- **WHEN** the optional command returns 127
- **THEN** diagnostics report the optional probe unavailable, the operation remains nonfatal and the receipt records `admin_api_checked=false`

#### Scenario: Command fails
- **WHEN** the optional command returns another nonzero status
- **THEN** diagnostics report a failed optional command without credential fallback or secret output and the receipt records `admin_api_checked=false`

#### Scenario: Check mode omits Admin access
- **WHEN** the operation runs in check mode
- **THEN** no Admin command, apply receipt or activation is performed
