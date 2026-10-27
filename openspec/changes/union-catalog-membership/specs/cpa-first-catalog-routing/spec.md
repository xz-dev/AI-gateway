## ADDED Requirements

### Requirement: CPA runs an official image with default model-prefix behavior

CPA SHALL run an official upstream release image, pinned by digest, without gateway-specific source patches. Its `routing.force-model-prefix` setting SHALL be `false`, the upstream default.

Gateway routing and catalog membership SHALL NOT depend on that setting:
- the inner selector keeps sending IDs without `/` to AISIX and IDs with `/` to CPA unchanged;
- AISIX targets that use the CPA pool SHALL name CPA IDs that carry a CPA prefix or a CPA-registered ID containing `/`.

#### Scenario: Prefixed ID reaches only its prefix's credentials

- **WHEN** a client requests `xl/gpt-6.1-sol`
- **THEN** the selector forwards it to CPA unchanged, and CPA serves it only with the credential whose prefix is `xl`

#### Scenario: Bare ID still routes to AISIX

- **WHEN** a client requests `gpt-6.1-sol` and CPA also registers a bare `gpt-6.1-sol`
- **THEN** the selector forwards it to AISIX, not CPA

#### Scenario: Unprefixed slash ID served by its registering credential

- **WHEN** a client requests `devin/claude-opus-5-5`, which only the Devin account registers
- **THEN** CPA serves it with that account, as before the setting change

#### Scenario: Setting drift is visible

- **WHEN** an operator inspects CPA's `force-model-prefix` setting after deployment
- **THEN** it reads `false` through the Management API and in the persisted CPA configuration
