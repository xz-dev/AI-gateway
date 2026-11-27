## ADDED Requirements

### Requirement: Single-push full-set version updates use a complete verified selection

When the operator requests a single push for a full-set image update, the release SHALL verify all selected candidates and their rollback prerequisites before pushing any of those selections to main. A scope with no newer compatible artifact SHALL retain its current selection and be reported. A complete release SHALL still apply production changes in isolated component or explicitly approved coupled windows with fresh previews and verification.

#### Scenario: A fork or production preflight is not ready

- **GIVEN** the operator chose one main-branch push for a full-set update
- **WHEN** a selected fork lacks a verified artifact or a required registry check cannot establish the planned artifact
- **THEN** the operator receives the specific blocker and no partial selection is pushed or applied

#### Scenario: The complete candidate selection passes

- **GIVEN** all selected artifacts and rollback prerequisites pass, and no unexplained drift is present
- **WHEN** the operator approves the exact rollout plan and interruption window
- **THEN** the completed selections are published together and the approved rollout proceeds serially with fresh verification after each activation
