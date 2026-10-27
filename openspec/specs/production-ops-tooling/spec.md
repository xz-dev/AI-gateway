# Production Ops Tooling Specification

## Purpose

Defines the structure and conventions of the Ansible operations tooling: where operator configuration lives, how shared deployment logic is packaged, how operations are invoked, and how variables are declared. This capability covers the *tooling shape* only; deployment behavior (push model, drift gate, rollback, receipts) is specified by the existing `production-operations-control` capability and is not re-specified here.

## Requirements

### Requirement: Operator configuration lives in project inventory, not ini group vars

Operator-facing configuration SHALL be expressed as YAML inventories, one directory per environment (`hosts.yml`, `group_vars/`, `host_vars/`), not as ini `[group:vars]` key-value lines. The repository SHALL track only a documented example inventory (`ops/ansible/inventories/example/`) that contains no real host address, user or secret. Operators copy it to create a real inventory.

#### Scenario: New operator bootstraps configuration

- **WHEN** an operator sets up the tooling for the first time
- **THEN** they copy the example inventory into the private configuration directory and edit structured values (lists, dicts, multi-line strings), without touching ini sections

#### Scenario: Structured config value needed

- **WHEN** a configuration value is a list or mapping (e.g. component profiles, managed files)
- **THEN** it is expressible natively in the YAML inventory without encoding hacks

#### Scenario: Tracked files contain no production host

- **WHEN** the tracked tree is searched for the production host's address or name
- **THEN** no file under `ops/ansible/` outside the gitignored private configuration directory contains it

### Requirement: Private operator config lives inside the project, gitignored

The operator's private configuration directory SHALL live inside the repository at a gitignored path (`ops/ansible/private-config/`), not in the operator's home directory. It holds the adopted production compose files, `.env`, AISIX resources, Squid policy, model-sync policy, model-catalog configuration and the production inventory (`inventories/production/`). The default inventory in `ansible.cfg` SHALL point at the production inventory in that directory. The configuration directory and the inventory SHALL be overridable (`ai_ops_config_dir`, `-i`) for operators who genuinely need an external location.

#### Scenario: Fresh clone on a second operator machine

- **WHEN** an operator clones the repo on a new machine and re-adopts production state
- **THEN** the private config and the production inventory land at the documented in-repo paths, and `git status` shows them as ignored, never as committable content

#### Scenario: Secrets never committed

- **WHEN** the operator runs `git add -A` or equivalent broad staging
- **THEN** no file under the private config path can be staged (gitignore coverage is total)

#### Scenario: Production inventory is absent

- **WHEN** a playbook runs without the private production inventory and without `-i`
- **THEN** no remote host is targeted and nothing is changed

### Requirement: Shared deployment logic packaged as roles with declared defaults

Shared logic SHALL be packaged as Ansible roles, one per concern:
- shared preflight and Compose invocation;
- managed-file drift gate, upload and rollback;
- component transaction;
- lock recovery;
- host recovery guard;
- one role per configuration-file operation.

Every role entry point SHALL declare its inputs in `meta/argument_specs.yml`, so invalid or missing inputs fail before any remote read or write. Every tunable variable SHALL be declared exactly once in a role `defaults/main.yml` with a default value and a comment, and SHALL be overridable from inventory. No variable default SHALL be defined inline in a playbook or task via `set_fact` or `| default()` when it is operator-tunable.

#### Scenario: Operator discovers available knobs

- **WHEN** an operator wants to know every configurable variable and its default
- **THEN** reading the roles' `defaults/main.yml` and `meta/argument_specs.yml` files gives a complete, commented list without grepping playbooks

#### Scenario: Inventory override wins

- **WHEN** an operator sets a variable in the inventory's `group_vars/` or `host_vars/`
- **THEN** it takes precedence over the role default without editing role files

#### Scenario: Invalid role input

- **WHEN** a role entry point receives a missing required input or a value outside its declared choices
- **THEN** argument validation fails before preflight connects to the remote host

### Requirement: Variable namespace is uniform and documented

Every tooling-defined variable SHALL start with the name of the role that defines it (`ai_ops_` for the shared role, `ai_ops_<concern>_` for a concern role). Internal temporaries SHALL use the same prefix after a leading double underscore. The complete variable reference SHALL be documented in one place (role defaults, argument specs and README), including which variables are operator-facing and which are internal. Renamed operator variables SHALL be listed with their old names in the documentation.

#### Scenario: New variable added

- **WHEN** a contributor adds a tunable to a role
- **THEN** it carries that role's prefix, is declared in the role defaults with a comment and in its argument spec, and appears in the documented variable reference

#### Scenario: Lint checks naming

- **WHEN** the lint gate runs
- **THEN** a role variable without its role prefix fails it

### Requirement: Deliberate deviations are recorded as decisions

The tooling's remaining deliberate deviations from Ansible conventions SHALL be documented in the change's design.md with their rationale, so future contributors treat them as accepted decisions rather than defects to "fix":
- `command` Compose or Docker invocation where a `community.docker` module cannot keep a current guarantee (environment isolation of Compose interpolation, offline rendering, helper scripts), each listed with its reason;
- the custom drift-gate/baseline/receipt state machine;
- approval digests that bind an apply run to a reviewed check run.

#### Scenario: Contributor proposes switching to native check mode

- **WHEN** a contributor questions why the read-only preview is not native `--check`
- **THEN** the design document records that it now is, and lists the approval digest as the only remaining addition to it

#### Scenario: Contributor proposes switching to the compose module

- **WHEN** a contributor questions why a Compose call is a `command` instead of a `community.docker` module, or why native file baselines are not used
- **THEN** the design document lists that call with the guarantee the module cannot keep

### Requirement: Behavioral parity with pre-restructure tooling

A restructure of the tooling SHALL NOT change deployment guarantees. The following SHALL remain equivalent to the pre-restructure implementation:
- drift gate semantics;
- pre-deploy recovery copies;
- rollback guarantees;
- approval-digest binding;
- receipt format and location;
- activation and recreation scope;
- remote state directory layout and keys;
- the push model itself.

Mechanism may change where a guarantee is preserved. For example, Squid policy deploy uses a tarball + manifest checksum gate instead of per-file drift checks, because the per-file mechanism does not scale to 177 files on the real host.

#### Scenario: Existing rehearsal evidence stays valid

- **WHEN** the restructure is complete
- **THEN** every fixture proof and the production rehearsal observations still describe actual behavior: drift gate blocking, a no-op deploy that does not recreate containers, receipt contents, state dir permissions

#### Scenario: Existing remote state is reused

- **WHEN** the restructured tooling runs against a host deployed by the previous tooling
- **THEN** it reads the existing baselines, rollback snapshots and transition state under their existing names, and a check run against an unchanged host reports no change

### Requirement: Model-sync policy is a first-class operation
The Ansible tooling SHALL expose model-sync policy deployment and rollback through the configuration operation playbooks with `component=cpa-model-sync`. The operation SHALL use the configured private desired-state directory and the shared Compose invocation rather than requiring an operator to edit the remote host or duplicate Compose command construction.

#### Scenario: Operator discovers the operation
- **WHEN** an operator reads the configuration playbooks' accepted components
- **THEN** `cpa-model-sync` is listed with the other configuration components

#### Scenario: Operator plans a policy update
- **WHEN** an operator runs the model-sync operation with `--check --diff`
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
- **WHEN** a check or apply run is requested with a policy containing an invalid `include` or `exclude` regular expression
- **THEN** preview fails before upload and the running sidecar and CPA inventories remain unchanged

#### Scenario: Plan is requested
- **WHEN** the model-sync operation runs in check mode
- **THEN** it executes only the existing container's read-only preview path, reports the complete model-set diff and approval digest, and performs no policy upload, PATCH, or container recreation

#### Scenario: Source inventory changes after approval
- **WHEN** apply recomputes the preview and its digest differs from the approved plan
- **THEN** apply stops before upload and requires a newly reviewed plan

### Requirement: Model-sync operation remains documented through existing surfaces
The Ansible README, role defaults, argument specs and the example inventory SHALL document:
- the model-sync policy path and the playbooks with their `component` value;
- check/apply usage, the read-only model diff and the approval digest;
- activation behavior and post-apply inventory verification;
- recovery behavior.

They SHALL use the documented variable namespace and documentation layout.

#### Scenario: Operator looks for model-sync controls
- **WHEN** an operator reads the documented operation and variable surfaces
- **THEN** they can identify the local policy path, preview and approval flow, remote effect, commands for check/apply, and the fact that an approved changed policy recreates only `cpa-model-sync`

### Requirement: Model-catalog configuration is a first-class operation
The Ansible tooling SHALL expose model-catalog configuration deployment and rollback through the configuration operation playbooks with `component=model-catalog`. The operations SHALL use the configured private desired-state directory and shared Compose invocation rather than requiring remote host edits or duplicated Compose command construction.

#### Scenario: Operator discovers the operations
- **WHEN** an operator reads the configuration playbooks' accepted components
- **THEN** `model-catalog` is listed for both configuration deploy and configuration rollback

#### Scenario: Operator plans a configuration update
- **WHEN** an operator runs the model-catalog configuration operation with `--check --diff`
- **THEN** the playbook validates and compares the local private YAML with the remote bind-mounted YAML without uploading files or recreating containers

### Requirement: Model-catalog desired configuration is private and locally owned
The model-catalog YAML configuration SHALL live under the configured gitignored private configuration directory and SHALL deploy to the existing remote bind-mount path. Repository examples and tracked defaults SHALL NOT overwrite the adopted production configuration.

#### Scenario: Existing production configuration is adopted
- **WHEN** an operator prepares the first managed model-catalog configuration deployment
- **THEN** they can place the reviewed live YAML at the documented private local path and establish it as the deployment baseline without recreating the service

#### Scenario: Broad Git staging is attempted
- **WHEN** an operator stages repository changes broadly
- **THEN** the private model-catalog YAML remains ignored and cannot be committed

### Requirement: Model-catalog operation is documented through existing surfaces
The Ansible README, role defaults, argument specs and the example inventory SHALL document:
- the local and remote paths and the playbooks with their `component` value;
- adoption and check/apply;
- activation scope and verification;
- rollback and recovery behavior.

They SHALL use the documented variable namespace and documentation layout.

#### Scenario: Operator looks for models-enricher controls
- **WHEN** an operator reads the documented operation and variable surfaces
- **THEN** they can identify how to adopt, validate, check, deploy, verify, and roll back the configuration without logging into the server to edit it

### Requirement: Front keeper and bridge use existing component profiles
The Ansible tooling SHALL expose `openai-api-cache-keeper` and `responses-ws-sse-bridge` through the profile-selected component deploy and rollback playbooks. It SHALL NOT reinstate the retired inner-keeper or scoped standard-cache operations. Role defaults, the example inventory and the README SHALL document:
- the private source directory and Docker Compose v2 rendering;
- the fresh approval digest;
- target-only interruption and verification;
- explicit rollback.

#### Scenario: Operator plans a front component update
- **WHEN** the operator runs the component deploy playbook for either front component with `--check --diff`
- **THEN** it reports a sanitized candidate/live hash and identity plan without uploading, pulling, recreating, building, or acquiring a production writer lock

#### Scenario: Retired scoped operation is requested
- **WHEN** an operator requests a retired inner-keeper, standard-cache or response-keeper operation
- **THEN** no playbook or accepted component provides it, and the request fails argument validation before any remote access

### Requirement: One playbook per operation with a validated component selector

Each operation SHALL have its own playbook:
- component deploy;
- component rollback;
- configuration deploy;
- configuration rollback;
- host recovery guard install;
- stale lock recovery.

Operations that act on one component SHALL take it as `-e component=<name>`, validated against the operation's declared choices before any remote access. Tags SHALL only filter tasks within an operation and SHALL NOT select or combine operations. Compose-invocation boilerplate SHALL be defined once and shared by all playbooks.

#### Scenario: Operator runs any operation

- **WHEN** an operator performs a deploy or rollback
- **THEN** they run the playbook named after the operation and pass the component, and the accepted components are discoverable from the role argument specs (`ansible-doc --type role`)

#### Scenario: Unknown component

- **WHEN** an operator passes a component the operation does not support, or omits it
- **THEN** the playbook fails before connecting to the remote host and lists the accepted components

#### Scenario: Tag filter cannot start a second operation

- **WHEN** an operator passes `--tags` or `--skip-tags` to an operation playbook
- **THEN** only tasks of that operation are filtered, and safety gates (argument validation, preflight, drift and approval checks) still run

#### Scenario: Compose invocation changed once

- **WHEN** the compose command prefix (files, env-file, project name) needs to change
- **THEN** exactly one location is edited and all playbooks pick it up

### Requirement: Check mode is the read-only preview

`--check --diff` SHALL be the only read-only preview. A check run SHALL:
- perform every read-only comparison, validation, preview and approval-digest calculation;
- perform no remote write, lock acquisition, pull, recreation, restart or API mutation;
- report `changed` only for production changes it would make, never for controller-local scratch work.

A diff SHALL NOT print secret-bearing file contents. An apply run SHALL still require the approval digest printed by a fresh check run wherever the operation is approval-bound.

#### Scenario: Unchanged production is checked

- **WHEN** every deploy playbook runs with `--check --diff` against a host that matches the private desired state
- **THEN** each reports `changed=0` and `failed=0`, including the Squid policy, whose local render and archive do not count as changes

#### Scenario: Secret file differs

- **WHEN** `.env` or another secret-bearing file differs in a check run
- **THEN** the output reports that it would change without showing its contents

#### Scenario: Apply without a fresh approval

- **WHEN** an approval-bound operation runs without `--check` and without a matching approval digest
- **THEN** it stops before any mutation and prints the digest to review

### Requirement: Container operations use pinned community.docker modules where they keep the guarantees

Container inspection, image inspection, image pull, Compose recreate and restart, and read-only exec SHALL use `community.docker` modules, unless a module cannot keep a guarantee of the current implementation. The collection version SHALL be pinned in `ops/ansible/requirements.yml` and installed into a project-local collections path, so a different user-level copy is never used. Module calls SHALL pass the Compose project name, files and env files explicitly and SHALL never include dependencies, build images or pull outside the approved step. Mutating module calls SHALL run only in apply mode. Compose calls that run with a cleared environment (`env -i`) SHALL keep that isolation.

Preflight SHALL verify read-only that the host provides what the modules need, and SHALL stop and name the missing item instead of installing anything.

#### Scenario: A component is recreated through the module

- **WHEN** an approved deploy recreates one service with `docker_compose_v2`
- **THEN** only that service is recreated, no image is pulled or built, every other project container keeps its ID and start time, and the project name is the configured one

#### Scenario: Host lacks a module prerequisite

- **WHEN** the host has no Compose plugin of a supported version, or no Python `requests` for the remote interpreter
- **THEN** preflight fails before any change and names the missing prerequisite

#### Scenario: A variable in the remote login environment shadows an `.env` key

- **WHEN** the component transaction renders or recreates a service
- **THEN** Compose interpolates only from the deployed `.env`, as before the restructure

### Requirement: Controller Compose renderer is pinned to the host version

The candidate render used by the drift gate and scope guard SHALL run on the controller with a Compose binary whose version and per-architecture checksum are recorded in the repository. Preflight SHALL obtain that binary into a task-scoped directory, verify its checksum, and stop before any render if its version differs from the host's Compose version. The render SHALL need no container daemon and no system-wide install.

#### Scenario: Check run from a Podman workstation

- **WHEN** an operator without Docker or a Compose binary on `PATH` runs any playbook with `--check --diff`
- **THEN** preflight fetches and verifies the pinned renderer and the run completes without a hand-installed binary

#### Scenario: Host Compose is upgraded

- **WHEN** the host's Compose version no longer equals the pinned renderer version
- **THEN** every run stops in preflight and names both versions, before any render or comparison

#### Scenario: Renderer binary is tampered

- **WHEN** the cached renderer does not match the recorded checksum
- **THEN** preflight refuses to use it

### Requirement: Tooling passes a production lint gate

All playbooks and roles SHALL pass `ansible-lint` at the `production` profile with zero violations and `ansible-playbook --syntax-check`, in CI and locally. A lint exception SHALL be a per-line `noqa` that names the rule and gives a reason; blanket rule skips SHALL NOT be used. The controller toolchain versions SHALL be pinned in the repository.

#### Scenario: A change introduces a violation

- **WHEN** a pull request adds a task that breaks a production-profile rule
- **THEN** CI fails and names the rule and location
