> Open change: requirements below are intended acceptance, not certification
> of full production capacity. See the [2026-11-09 disposition](../../../../../docs/records/maintenance-capacity-closeout-2026-11-09.md);
> residual size checks and public-template enablement remain pending.

## MODIFIED Requirements

### Requirement: CPA runs an official image with default model-prefix behavior

CPA SHALL run an official upstream release image, pinned by digest, without gateway-specific source patches, with one exception. CPA SHALL run a digest-pinned fork build of an official release tag, based on the production tree (`v8.0.51` plus the PR #7134 backport). The fork SHALL carry only reviewed backports of upstream PRs (currently PR #7134) plus the switchable body-spool patch. It SHALL carry no commits from third-party forks. The fork digest SHALL appear in the published compatibility lock. CPA SHALL return to the official image at the next reviewed upgrade, but only once every carried patch is in an official release. If upstream declines the PR, the fork MAY remain long-lived, rebased periodically onto official releases, with each rebased digest admitted to the lock only after the CI gate passes. Its `routing.force-model-prefix` setting SHALL be `false`, the upstream default.

Gateway routing and catalog membership SHALL NOT depend on that setting:
- AISIX exact rows and routes take priority, and every other ID reaches CPA unchanged through `*`;
- AISIX targets that use the CPA pool SHALL name CPA IDs that carry a CPA prefix or a CPA-registered ID containing `/`.

#### Scenario: Prefixed ID reaches only its prefix's credentials

- **WHEN** a client requests `xl/gpt-6.1-sol`
- **THEN** AISIX forwards it to CPA unchanged, and CPA serves it only with the credential whose prefix is `xl`

#### Scenario: Bare ID still routes to AISIX

- **WHEN** a client requests `gpt-6.1-sol` and CPA also registers a bare `gpt-6.1-sol`
- **THEN** the AISIX logical route serves it, not the CPA bare copy

#### Scenario: Unprefixed slash ID served by its registering credential

- **WHEN** a client requests `devin/claude-opus-5-5`, which only the Devin account registers and AISIX does not define
- **THEN** AISIX forwards it through `*` and CPA serves it with that account

#### Scenario: AISIX target no longer registered by CPA

- **WHEN** a CPA image change stops registering an unprefixed slash ID that an AISIX direct model names, such as `devin/deepseek-v4-1-flash` in v8.0.45
- **THEN** that direct model and every logical-route target naming it are removed from the AISIX resources in the same deployment window
- **AND** every AISIX target that CPA registered at pre-capture remains registered, except accepted upstream removals removed from AISIX in that window; no new unregistered target appears
- **AND** pre-existing targets absent from CPA registration are recorded separately, not deleted as part of this change

#### Scenario: Unreleased backport keeps the fork

- **WHEN** the body-spool PR is released upstream but PR #7134 is not
- **THEN** CPA keeps running the fork that carries PR #7134

#### Scenario: Setting drift is visible

- **WHEN** an operator inspects CPA's `force-model-prefix` setting after deployment
- **THEN** it reads `false` through the Management API and in the persisted CPA configuration
