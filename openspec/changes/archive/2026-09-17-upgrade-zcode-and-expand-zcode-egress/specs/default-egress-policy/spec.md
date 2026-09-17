## MODIFIED Requirements

### Requirement: Optional and dynamic destinations stay outside the default

The fresh-install policy SHALL exclude provider inference and OAuth endpoints, user-defined destinations, GitHub user-profile endpoints, CPA plugin registry and dynamic plugin repositories, CPA metadata plugins, unverified upstream mirror or fallback domains, Sub2API rollback release listing, backup and payment integrations, private overrides, and local ZCode destinations. An operator-owned production policy MAY enable ZCode only through service-scoped exact domain, HTTP method, and path rules for the deployed ZCode version; it SHALL NOT grant a domain-wide or path-wide ZCode bypass.

#### Scenario: Optional management function is not configured

- **WHEN** a fresh installation requests `^/repos/Wei-Shaw/sub2api/releases\\?per_page=15$`, a plugin release API, an unverified fallback mirror, or a ZCode provider endpoint
- **THEN** the request remains denied until an operator adds a feature-specific exact rule

#### Scenario: Provider is configured later

- **WHEN** an operator enables a provider, OAuth flow, plugin, backup target, or other deployment-specific integration
- **THEN** the operator must add its exact destination policy separately
- **AND** enabling that integration does not broaden the tracked default policy

#### Scenario: Production ZCode policy covers the deployed feature set

- **WHEN** an operator deploys ZCode Proxy 4.6.5 with OAuth, quota, claim, off-peak, endpoint routing, client signing, captcha, inference, and MCP functions
- **THEN** the operator-owned ZCode policy allows only the corresponding documented domains, HTTP methods, and paths
- **AND** an undeclared path on an otherwise allowed ZCode domain remains denied

### Requirement: Existing runtime policy remains operator-owned

Initialization SHALL copy the tracked example policy only when the runtime policy does not exist. Repository updates SHALL NOT merge the new baseline into or overwrite an existing `data/egress-proxy/policy.json`. A ZCode version upgrade that needs additional destinations SHALL be applied as an explicit operator-reviewed edit to only the runtime ZCode service policy.

#### Scenario: Existing installation updates the repository

- **WHEN** an installation already has `data/egress-proxy/policy.json` and receives this repository change
- **THEN** its runtime policy remains byte-for-byte operator-controlled
- **AND** documentation tells the operator to review and manually merge any desired baseline rules

#### Scenario: Operator expands only ZCode egress

- **WHEN** ZCode Proxy is upgraded and its supported functions require additional egress paths
- **THEN** only the operator-owned ZCode service destinations are changed
- **AND** every non-ZCode service policy and credential remains unchanged

### Requirement: Default policy contract is validated offline

Repository validation SHALL render the tracked default policy and SHALL verify its required exact control-plane rules and fail-closed boundaries without depending on live GitHub or provider network access. Deployment-specific ZCode policy validation SHALL additionally prove that declared ZCode methods and paths render successfully and that representative undeclared paths remain denied.

#### Scenario: A future change broadens a GitHub path

- **WHEN** a contributor replaces a required exact rule with an owner, repository, release-path, or repository-ID wildcard
- **THEN** repository validation fails before the change is accepted

#### Scenario: A ZCode rule is broadened beyond the required path

- **WHEN** a deployment-specific ZCode policy replaces an exact OAuth, billing, off-peak, MCP, routing, signing, captcha, or inference path with a domain-wide or path-wide permission
- **THEN** validation fails before rollout
