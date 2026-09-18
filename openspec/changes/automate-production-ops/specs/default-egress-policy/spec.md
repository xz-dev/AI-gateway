## MODIFIED Requirements

### Requirement: Existing runtime policy remains operator-owned

Initialization SHALL copy the tracked example policy only when the runtime policy does not exist. Repository updates SHALL NOT merge the new baseline into or overwrite an existing `data/egress-proxy/policy.json`. A ZCode version upgrade that needs additional destinations SHALL be applied as an explicit operator-reviewed edit to only the runtime ZCode service policy. After explicit adoption, the operator's private desired policy SHALL be an authorized source for such reviewed service-scoped deployment; repository examples SHALL NOT become its source of truth. Deployment SHALL compare the affected runtime policy with its reviewed baseline, reject unexplained drift, and preserve unrelated policies and credentials.

#### Scenario: Existing installation updates the repository

- **WHEN** an installation already has `data/egress-proxy/policy.json` and receives this repository change
- **THEN** its runtime policy remains byte-for-byte operator-controlled
- **AND** documentation tells the operator to review and explicitly select any desired baseline rules, through a manual edit or an adopted private desired-policy deployment

#### Scenario: Operator expands only ZCode egress

- **WHEN** ZCode Proxy is upgraded and its supported functions require additional egress paths
- **THEN** only the operator-owned ZCode service destinations are changed
- **AND** every non-ZCode service policy and credential remains unchanged

#### Scenario: Adopted private policy is explicitly deployed

- **GIVEN** the operator has reviewed and adopted the runtime baseline
- **WHEN** a reviewed private desired-policy change is approved for deployment
- **THEN** only the selected service changes are applied after drift and policy validation
- **AND** receiving a repository update alone never triggers that deployment
