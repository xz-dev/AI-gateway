## ADDED Requirements

### Requirement: Models-enricher configuration is a first-class tagged operation
The Ansible tooling SHALL expose models-enricher configuration deployment and rollback from the existing single entry playbook through dedicated tags. The operations SHALL use the configured private desired-state directory and shared Compose invocation rather than requiring remote host edits or duplicated Compose command construction.

#### Scenario: Operator discovers the operations
- **WHEN** an operator lists tags on the operations playbook
- **THEN** models-enricher configuration deploy and rollback operations are visible with the other component-specific operations

#### Scenario: Operator plans a configuration update
- **WHEN** an operator invokes the models-enricher configuration operation in plan mode
- **THEN** the playbook validates and compares the local private YAML with the remote bind-mounted YAML without uploading files or recreating containers

### Requirement: Models-enricher desired configuration is private and locally owned
The models-enricher YAML configuration SHALL live under the configured gitignored private configuration directory and SHALL deploy to the existing remote bind-mount path. Repository examples and tracked defaults SHALL NOT overwrite the adopted production configuration.

#### Scenario: Existing production configuration is adopted
- **WHEN** an operator prepares the first managed models-enricher configuration deployment
- **THEN** they can place the reviewed live YAML at the documented private local path and establish it as the deployment baseline without recreating the service

#### Scenario: Broad Git staging is attempted
- **WHEN** an operator stages repository changes broadly
- **THEN** the private models-enricher YAML remains ignored and cannot be committed

### Requirement: Models-enricher operation is documented through existing surfaces
The Ansible README, role defaults, usage comments, and operator variable examples SHALL document the local and remote paths, tags, adoption, plan/apply, activation scope, verification, rollback, and recovery behavior using the existing `ai_ops_` namespace and documentation layout.

#### Scenario: Operator looks for models-enricher controls
- **WHEN** an operator reads the documented operation and variable surfaces
- **THEN** they can identify how to adopt, validate, plan, deploy, verify, and roll back the configuration without logging into the server to edit it
