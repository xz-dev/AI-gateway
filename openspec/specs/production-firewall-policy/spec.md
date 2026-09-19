# Production Firewall Policy Specification

## Purpose

Let operators repeatedly deploy a reviewed service's Squid outbound policy without broadening unrelated access, disturbing existing trust, or taking over host and cloud firewalls.

## Requirements
### Requirement: Firewall deploys push the locally edited policy

The operator edits the local private Squid policy JSON (one service's entries/fields); other rules, custom settings, and order in that same service stay in the local file and are therefore preserved by construction. The deploy SHALL render locally, validate, upload the rendered generated directory, and activate via the deployed version's tested path. It SHALL preserve existing CA material and the fail-closed network boundary, and SHALL NOT modify host nftables/iptables/UFW rules, Docker firewall rules, Tailscale policy, cloud security groups, or network topology. An operation requiring such changes SHALL stop and identify the separate work. It SHALL NOT modify host nftables/iptables/UFW rules, Docker firewall rules, Tailscale policy, cloud security groups, or network topology. An operation requiring such changes SHALL stop and identify the separate work instead of expanding its scope.

#### Scenario: Expand one provider's required paths

- **GIVEN** the operator has adopted the current Squid policy
- **WHEN** an approved ZCode update adds a required exact endpoint
- **THEN** only the reviewed ZCode change is rendered and deployed
- **AND** other ZCode settings, other services' rules, the CA, and non-Squid firewalls remain unchanged

#### Scenario: A service retains its private extra rule

- **GIVEN** the local policy for a service carries a custom ACL entry adopted from production
- **WHEN** an operator adds one approved endpoint to that service locally
- **THEN** the custom entry and the relative order of untouched rules are present in the rendered candidate
- **AND** the deployed remote policy contains both

#### Scenario: Remote policy drifted

- **WHEN** the remote generated policy differs from the last-deployed baseline
- **THEN** the deploy stops without overwriting, and the diff is shown for adoption into the local file

### Requirement: Candidate policy preserves least privilege before activation

The operation SHALL reject invalid policy and unintended broadening before activating it. The review SHALL distinguish service, domain, TLS mode, method, and path changes. Where TLS bump is used, declared method and path restrictions SHALL remain enforced; an existing approved splice exception SHALL NOT be presented as method/path enforcement or broadened implicitly. Repository defaults SHALL NOT be merged into an existing private policy as part of an ordinary update.

#### Scenario: A narrow endpoint becomes a wildcard

- **WHEN** a proposed change intended to permit one endpoint instead allows every path on its domain
- **THEN** validation rejects the mismatch with the approved scope
- **AND** the serving policy remains unchanged

#### Scenario: Invalid candidate policy

- **WHEN** the candidate cannot be rendered or accepted by the selected Squid version
- **THEN** activation is blocked without replacing the serving policy

### Requirement: Activation is verified and reversible within the policy scope

The operation SHALL activate only a validated, approved candidate, using the least disruptive supported method for the deployed version and mount layout. It SHALL verify a representative allowed request and denied destination, method, or path boundaries relevant to the change, with the authorized traffic defined in the plan. Activation failure or a failed mandatory boundary check SHALL trigger the approved recovery: upload the protected pre-deploy rendered directory and reactivate, then verify restoration. Failure to verify recovery SHALL be reported as recovery-required, never bypassed by disabling the proxy or opening access. An unchanged effective policy SHALL NOT trigger activation.

#### Scenario: A valid update is applied twice

- **WHEN** the approved policy has been activated and verified, and the same desired policy is applied again
- **THEN** the second operation reports no effective change without reloading Squid

#### Scenario: A denied path becomes reachable

- **WHEN** post-activation verification detects access outside the approved policy
- **THEN** the operation reports failure and uploads the pre-deploy rendered policy under the approved recovery action
- **AND** it reports separately whether the prior deny boundary was successfully restored
