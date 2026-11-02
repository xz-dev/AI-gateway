## MODIFIED Requirements

### Requirement: Model-catalog configuration is a first-class operation
The Ansible tooling SHALL expose model-catalog configuration deployment and rollback through the configuration operation playbooks with `component=model-catalog`. The operations SHALL use the configured private desired-state directory and shared Compose invocation rather than requiring remote host edits or duplicated Compose command construction.

The model-catalog profile SHALL consist of `config.yaml` and an optional `catalog.lua`. Each file SHALL be a managed file with its own drift gate and baseline. One operation SHALL validate both candidates with the deployed binary, deploy them, recreate `model-catalog` at most once, verify both remote files and the service, and on a failed verification roll back both files together.

A private `catalog.lua` SHALL be custom when the file exists in the private model-catalog directory, whatever its content. The operation SHALL fail before any upload, in check mode too, when the private profile has a custom `catalog.lua` and the private `config.yaml` sets `metadata.include_models`. The message SHALL name both files and say to move the hides into `catalog.lua`. The gateway itself SHALL NOT reject this combination.

#### Scenario: Operator discovers the operations
- **WHEN** an operator reads the configuration playbooks' accepted components
- **THEN** `model-catalog` is listed for both configuration deploy and configuration rollback

#### Scenario: Operator plans a configuration update
- **WHEN** an operator runs the model-catalog configuration operation with `--check --diff`
- **THEN** the playbook validates and compares both local private files with the remote bind-mounted files without uploading files or recreating containers

#### Scenario: Custom script combined with include_models rejected
- **WHEN** the private directory has `catalog.lua` and the private `config.yaml` sets `metadata.include_models`
- **THEN** the operation fails before upload, also under `--check`, and names both files

#### Scenario: Profile without a custom script
- **WHEN** the private directory has no `catalog.lua`
- **THEN** the operation manages only `config.yaml`, does not touch the remote `catalog.lua`, and allows `metadata.include_models`

#### Scenario: Script change alone recreates once
- **WHEN** only the private `catalog.lua` differs from the remote file
- **THEN** the operation validates it with `validate-config /app/config.yaml --script -`, uploads it, recreates `model-catalog` once and verifies the remote checksum and readiness

#### Scenario: Failed verification restores both files
- **WHEN** both files changed and post-activation verification fails
- **THEN** both remote files are restored from their baselines and `model-catalog` is recreated on the restored pair

### Requirement: Model-catalog desired configuration is private and locally owned
The model-catalog `config.yaml` and the optional `catalog.lua` SHALL live under the configured gitignored private configuration directory and SHALL deploy to the existing remote bind-mount paths `services/routing/model-catalog/config.yaml` and `services/routing/model-catalog/catalog.lua`. Repository examples and tracked defaults SHALL NOT overwrite the adopted production configuration.

#### Scenario: Existing production configuration is adopted
- **WHEN** an operator prepares the first managed model-catalog configuration deployment
- **THEN** they can place the reviewed live YAML at the documented private local path and establish it as the deployment baseline without recreating the service

#### Scenario: Broad Git staging is attempted
- **WHEN** an operator stages repository changes broadly
- **THEN** the private model-catalog YAML and `catalog.lua` remain ignored and cannot be committed

### Requirement: Model-catalog operation is documented through existing surfaces
The Ansible README, role defaults, argument specs and the example inventory SHALL document:
- the local and remote paths of both profile files and the playbooks with their `component` value;
- what makes `catalog.lua` custom and the `include_models` combination check;
- adoption and check/apply;
- activation scope and verification;
- rollback and recovery behavior.

They SHALL use the documented variable namespace and documentation layout.

#### Scenario: Operator looks for models-enricher controls
- **WHEN** an operator reads the documented operation and variable surfaces
- **THEN** they can identify how to adopt, validate, check, deploy, verify, and roll back the configuration without logging into the server to edit it

#### Scenario: Operator looks for the membership script
- **WHEN** an operator wants to change catalog membership
- **THEN** the documentation names the private `catalog.lua` path, the combination check, and links the script reference in `docs/model-catalog.md`
