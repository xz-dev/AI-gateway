## ADDED Requirements

### Requirement: Model-sync policy is a first-class tagged operation
The Ansible tooling SHALL expose model-sync policy deployment from the existing single entry playbook through a dedicated tag. The operation SHALL use the configured private desired-state directory and the shared Compose invocation rather than requiring an operator to edit the remote host or duplicate Compose command construction.

#### Scenario: Operator discovers the operation
- **WHEN** an operator lists tags on the operations playbook
- **THEN** the model-sync deployment operation is visible with the other component-specific operations

#### Scenario: Operator plans a policy update
- **WHEN** an operator invokes the model-sync operation in plan mode
- **THEN** the playbook compares the local private policy with the remote policy and runs a read-only CPA preview without uploading, recreating, or PATCHing channel inventories

### Requirement: Model-sync desired policy is private and locally owned
The model-sync JSON policy SHALL live under the configured gitignored private configuration directory and SHALL be deployed to the existing remote bind-mount path. Repository examples SHALL NOT overwrite the adopted production policy.

#### Scenario: Existing production policy is adopted
- **WHEN** an operator prepares the first managed model-sync deployment
- **THEN** they can place the reviewed live policy at the documented private local path and establish it as the deployment baseline without committing it

#### Scenario: Broad Git staging is attempted
- **WHEN** an operator stages repository changes broadly
- **THEN** the private model-sync policy remains ignored and cannot be committed with tracked source

### Requirement: Model-sync configuration is validated and previewed by its own executable
Before upload, the operation SHALL use the executable in the currently deployed model-sync container to parse the complete candidate, validate all supported fields, limits, and regular expressions, and calculate each managed channel's current and desired model sets. Preview SHALL use the sidecar's normal read path but SHALL NOT PATCH CPA, alter model inventories, upload the candidate, or recreate any container. It SHALL produce a sanitized review artifact containing each channel's additions, removals, unchanged count, and a deterministic digest binding the candidate policy, fresh source inventory, and desired model sets.

#### Scenario: Candidate contains an invalid include expression
- **WHEN** plan or apply is requested with a policy containing an invalid `include` or `exclude` regular expression
- **THEN** preview fails before upload and the running sidecar and CPA inventories remain unchanged

#### Scenario: Plan is requested
- **WHEN** the model-sync operation runs with `ai_ops_mode=plan`
- **THEN** it executes only the existing container's read-only preview path, reports the complete model-set diff and approval digest, and performs no policy upload, PATCH, or container recreation

#### Scenario: Source inventory changes after approval
- **WHEN** apply recomputes the preview and its digest differs from the approved plan
- **THEN** apply stops before upload and requires a newly reviewed plan

### Requirement: Model-sync operation remains documented through existing surfaces
The Ansible README, role defaults, and operator variable examples SHALL document the model-sync policy path, tag, plan/apply usage, read-only model diff and approval digest, activation behavior, post-apply inventory verification, and recovery behavior using the existing `ai_ops_` namespace and documentation layout.

#### Scenario: Operator looks for model-sync controls
- **WHEN** an operator reads the documented operation and variable surfaces
- **THEN** they can identify the local policy path, preview and approval flow, remote effect, commands for plan/apply, and the fact that an approved changed policy recreates only `cpa-model-sync`
