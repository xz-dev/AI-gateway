# Default Egress Policy Specification

## Purpose

Define the outbound requests a fresh AI-gateway installation may make before operator customization, preserving a fail-closed boundary while keeping fixed built-in control-plane maintenance functional.

## Requirements

### Requirement: Fresh-install policy remains fail-closed

The fresh-install egress policy SHALL allow only explicitly listed control-plane destinations. Every default destination SHALL use an exact domain, TLS bump, the `GET` method, and an anchored path expression; requests with another destination, method, or path SHALL remain denied.

#### Scenario: Unlisted request remains denied

- **WHEN** CPA or Sub2API requests an unlisted host, an unlisted path on an allowed host, or a method other than `GET`
- **THEN** the egress proxy denies the request without contacting the upstream destination

#### Scenario: GitHub repository wildcard is absent

- **WHEN** an operator inspects the fresh-install policy
- **THEN** no rule matches arbitrary GitHub owners or repositories
- **AND** no rule permits every release path on `github.com`, `api.github.com`, or `release-assets.githubusercontent.com`

### Requirement: CPA Management Center update chain is available

The default CPA policy SHALL allow the complete verified Management Center update chain with these JSON path expressions:

- `api.github.com`: `^/repos/router-for-me/Cli-Proxy-API-Management-Center/releases/latest$`
- `github.com`: `^/router-for-me/Cli-Proxy-API-Management-Center/releases/download/[^/]+/management\\.html$`
- `release-assets.githubusercontent.com`: `^/github-production-release-asset/1051566067/`

The release-assets rule SHALL remain scoped to repository ID `1051566067`; it SHALL NOT use a repository-ID wildcard.

#### Scenario: Management Center updater follows the release redirect

- **WHEN** CPA checks the latest Management Center release and downloads its `management.html` asset
- **THEN** the release metadata request, repository-specific browser download, and repository-ID-scoped CDN request are allowed
- **AND** release assets belonging to another repository ID remain denied

### Requirement: CPA model catalog refresh is available

The default CPA policy SHALL allow these exact JSON path expressions on `raw.githubusercontent.com`:

- `^/router-for-me/models/refs/heads/main/models\\.json$`
- `^/router-for-me/models/refs/heads/main/codex_client_models\\.json$`

#### Scenario: CPA refreshes built-in catalogs

- **WHEN** CPA performs its startup or periodic model catalog refresh
- **THEN** both fixed catalog requests are allowed
- **AND** other branches, files, repositories, and raw GitHub paths remain denied

### Requirement: Sub2API pricing refresh is available

The default Sub2API policy SHALL allow these exact JSON path expressions on `raw.githubusercontent.com`:

- `^/Wei-Shaw/model-price-repo/main/model_prices_and_context_window\\.json$`
- `^/Wei-Shaw/model-price-repo/main/model_prices_and_context_window\\.sha256$`

#### Scenario: Pricing hash reports changed data

- **WHEN** Sub2API checks the remote pricing hash and determines that pricing data changed
- **THEN** the exact hash and pricing JSON requests are allowed
- **AND** unrelated files from the pricing repository remain denied

### Requirement: Sub2API Codex version synchronization is available

The default Sub2API policy SHALL allow these exact JSON path expressions on `api.github.com`:

- `^/repos/openai/codex/releases/latest$`
- `^/repos/openai/codex/releases\\?per_page=30$`

#### Scenario: Latest release is not a usable stable Codex release

- **WHEN** the automatic Codex version synchronizer cannot derive a usable stable version from the latest-release response
- **THEN** its exact `per_page=30` fallback request is allowed
- **AND** other OpenAI repository API paths remain denied

### Requirement: Read-only service version awareness is available

The default policy SHALL allow these exact `api.github.com` JSON path expressions:

- CPA: `^/repos/router-for-me/CLIProxyAPI/releases/latest$`
- Sub2API: `^/repos/Wei-Shaw/sub2api/releases/latest$`

These rules SHALL provide release awareness only; the default policy SHALL NOT allow Sub2API binary update, checksum, or rollback asset downloads.

#### Scenario: Administrator checks service versions

- **WHEN** an administrator opens a built-in version-check view for CPA or Sub2API
- **THEN** the corresponding exact latest-release request is allowed
- **AND** no generic GitHub release asset permission is granted

### Requirement: Optional and dynamic destinations stay outside the default

The fresh-install policy SHALL exclude provider inference and OAuth endpoints, user-defined destinations, GitHub user-profile endpoints, CPA plugin registry and dynamic plugin repositories, CPA metadata plugins, unverified upstream mirror or fallback domains, Sub2API rollback release listing, backup and payment integrations, private overrides, and local ZCode destinations.

#### Scenario: Optional management function is not configured

- **WHEN** a fresh installation requests `^/repos/Wei-Shaw/sub2api/releases\\?per_page=15$`, a plugin release API, or an unverified fallback mirror
- **THEN** the request remains denied until an operator adds a feature-specific exact rule

#### Scenario: Provider is configured later

- **WHEN** an operator enables a provider, OAuth flow, plugin, backup target, or other deployment-specific integration
- **THEN** the operator must add its exact destination policy separately
- **AND** enabling that integration does not broaden the tracked default policy

### Requirement: Existing runtime policy remains operator-owned

Initialization SHALL copy the tracked example policy only when the runtime policy does not exist. Repository updates SHALL NOT merge the new baseline into or overwrite an existing `data/egress-proxy/policy.json`.

#### Scenario: Existing installation updates the repository

- **WHEN** an installation already has `data/egress-proxy/policy.json` and receives this repository change
- **THEN** its runtime policy remains byte-for-byte operator-controlled
- **AND** documentation tells the operator to review and manually merge any desired baseline rules

### Requirement: Default policy contract is validated offline

Repository validation SHALL render the tracked default policy and SHALL verify its required exact control-plane rules and fail-closed boundaries without depending on live GitHub or provider network access.

#### Scenario: A future change broadens a GitHub path

- **WHEN** a contributor replaces a required exact rule with an owner, repository, release-path, or repository-ID wildcard
- **THEN** repository validation fails before the change is accepted
