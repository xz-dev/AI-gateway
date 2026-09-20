# Production Ops Tooling Specification

## Purpose

Defines the structure and conventions of the Ansible operations tooling: where operator configuration lives, how shared deployment logic is packaged, how operations are invoked, and how variables are declared. This capability covers the *tooling shape* only; deployment behavior (push model, drift gate, rollback, receipts) is specified by the existing `production-operations-control` capability and is not re-specified here.

## Requirements
### Requirement: Operator configuration lives in project inventory, not ini group vars

Operator-facing configuration SHALL be expressed as YAML in `ansible/inventory/` (`group_vars` and/or `host_vars`), not as ini `[group:vars]` key-value lines. A documented template (`examples/vars.yml` or equivalent) SHALL exist for operators to copy.

#### Scenario: New operator bootstraps configuration

- **WHEN** an operator sets up the tooling for the first time
- **THEN** they copy a documented YAML template into `inventory/` and edit structured values (lists, dicts, multi-line strings possible), without touching ini sections

#### Scenario: Structured config value needed

- **WHEN** a configuration value is a list or mapping (e.g. component profiles, managed files)
- **THEN** it is expressible natively in the YAML inventory without encoding hacks

### Requirement: Private operator config lives inside the project, gitignored

The operator's private configuration directory (adopted production compose files, `.env`, AISIX resources, Squid policy) SHALL live inside the repository at a gitignored path (e.g. `ansible/private-config/`), not in the operator's home directory. The path SHALL be overridable via inventory for operators who genuinely need an external location.

#### Scenario: Fresh clone on a second operator machine

- **WHEN** an operator clones the repo on a new machine and re-adopts production state
- **THEN** the private config lands at the documented in-repo path and `git status` shows it as ignored, never as committable content

#### Scenario: Secrets never committed

- **WHEN** the operator runs `git add -A` or equivalent broad staging
- **THEN** no file under the private config path can be staged (gitignore coverage is total)

### Requirement: Shared deployment logic packaged as roles with declared defaults

Shared logic (preflight, deploy-file, rollback-file) SHALL be packaged as Ansible roles. Every tunable variable SHALL be declared exactly once in a role `defaults/main.yml` with a default value and a comment, and SHALL be overridable from inventory. No variable default SHALL be defined inline in a playbook or task via `set_fact` or `| default()` when it is operator-tunable.

#### Scenario: Operator discovers available knobs

- **WHEN** an operator wants to know every configurable variable and its default
- **THEN** reading the roles' `defaults/main.yml` files gives a complete, commented list without grepping playbooks

#### Scenario: Inventory override wins

- **WHEN** an operator sets a variable in `inventory/group_vars/` or `host_vars/`
- **THEN** it takes precedence over the role default without editing role files

### Requirement: Single entry playbook with tag-selected operations

All operations (component deploy, Squid deploy, AISIX deploy, rollback) SHALL be reachable from a single entry playbook using `--tags` to select the operation. Compose-invocation boilerplate SHALL NOT be duplicated across operation paths.

#### Scenario: Operator runs any operation

- **WHEN** an operator performs a deploy or rollback
- **THEN** they invoke the same playbook file and select the operation via `--tags <operation>`, with the operation surface discoverable via `--list-tags`

#### Scenario: Compose invocation changed once

- **WHEN** the compose command prefix (files, env-file, project name) needs to change
- **THEN** exactly one location is edited and all operation paths pick it up

### Requirement: Variable namespace is uniform and documented

All tooling-defined variables SHALL use the `ai_ops_` prefix. The complete variable reference SHALL be documented in one place (role defaults + README), including which variables are operator-facing versus internal.

#### Scenario: New variable added

- **WHEN** a contributor adds a tunable to the tooling
- **THEN** it follows the `ai_ops_` prefix, is declared in role defaults with a comment, and appears in the documented variable reference

### Requirement: Deliberate deviations are recorded as decisions

The tooling's deliberate deviations from Ansible conventions — the hand-rolled `ai_ops_mode=plan|apply` instead of native `--check`, `command`/`shell` compose invocation instead of the docker compose module, and the custom drift-gate/baseline/receipt state machine — SHALL be documented in the change's design.md with their rationale, so future contributors treat them as accepted decisions rather than defects to "fix".

#### Scenario: Contributor proposes switching to native check mode

- **WHEN** a contributor questions why the tooling doesn't use `--check` or the compose module
- **THEN** the design document contains the recorded rationale explaining the accepted deviation

### Requirement: Behavioral parity with pre-restructure tooling

The restructure SHALL NOT change deployment guarantees: drift gate semantics, pre-deploy recovery copies, rollback guarantees, receipt format and location, activation commands, remote state directory layout, and the push model itself SHALL remain equivalent to the pre-restructure implementation. Mechanism may change where a guarantee is preserved — notably, Squid policy deploy uses a tarball + manifest checksum gate instead of per-file drift checks (the per-file mechanism does not scale to 177 files on the real host).

#### Scenario: Existing rehearsal evidence stays valid

- **WHEN** the restructure is complete
- **THEN** every fixture proof and the rainyun-la rehearsal observations (drift gate blocking, no-op deploy not recreating containers, receipt contents, state dir permissions) still describe actual behavior, with no re-verification of *behavior* required beyond a smoke check

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
