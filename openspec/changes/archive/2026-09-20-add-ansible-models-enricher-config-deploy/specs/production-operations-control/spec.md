## ADDED Requirements

### Requirement: Models-enricher configuration deployment preserves drift and recovery guarantees
The models-enricher configuration operation SHALL apply the existing per-file drift gate, protected pre-deploy copy, baseline update, guarded rollback, and sanitized receipt guarantees to the remote YAML. Independent remote edits SHALL stop deployment before upload unless the operator reviews and adopts or reconciles them.

#### Scenario: Remote configuration changed independently
- **WHEN** the remote models-enricher YAML differs from both its recorded baseline and the local desired YAML
- **THEN** deployment stops before upload or container recreation and reports that reconciliation is required

#### Scenario: First adoption matches production
- **WHEN** reviewed local and remote YAML bytes match but no current baseline exists
- **THEN** apply establishes the baseline without recreating models-enricher

### Requirement: Candidate configuration is validated before mutation
The operation SHALL validate the complete local YAML with the models-enricher executable before upload. Validation SHALL exercise startup-loadable configuration rules without overwriting the remote file, starting a replacement service, or issuing production write requests. A candidate that cannot be validated by the deployed executable SHALL stop before mutation.

#### Scenario: Candidate contains malformed YAML or an invalid regular expression
- **WHEN** plan or apply is requested with invalid local configuration
- **THEN** validation fails before upload and the running service remains unchanged

#### Scenario: Plan is requested for a valid changed candidate
- **WHEN** the operator runs plan mode with a valid local configuration that differs from the remote file
- **THEN** the operation shows the effective file diff and planned recreation and verification scope while leaving production unchanged

### Requirement: Changed configuration activates only models-enricher
A changed models-enricher YAML SHALL be activated by force-recreating only `models-enricher`, without rebuilding images, restarting its namespace owner or relays, restarting AISIX or CPA, or recreating unrelated services. Because the process reads configuration only at startup, a content change SHALL force recreation even when Compose service configuration and image identity are unchanged. An unchanged desired file with a current baseline SHALL remain a no-op.

#### Scenario: Configuration content changes
- **WHEN** a validated local YAML differs from the drift-free remote YAML
- **THEN** the file is uploaded and only `models-enricher` is force-recreated without dependencies or image builds

#### Scenario: Desired configuration already runs
- **WHEN** local, remote, and baseline YAML checksums match
- **THEN** the operation does not recreate or probe the service

### Requirement: Deployment verifies the expected file and fresh service readiness
After activation, success SHALL require the replacement container to use the expected deployed configuration bytes, remain running, and become healthy within a bounded interval using the existing readiness contract. Verification SHALL also confirm that explicitly protected coupled and unrelated container identities were not recreated. Container presence alone SHALL NOT prove success.

#### Scenario: Replacement loads the expected configuration and becomes ready
- **WHEN** the changed configuration is valid for current live channel identities and upstream availability permits initialization
- **THEN** the replacement models-enricher container becomes healthy with the expected mounted-file digest and the operation records success

#### Scenario: Static validation passes but runtime validation or readiness fails
- **WHEN** the replacement exits, loads different bytes, or does not become healthy before the bounded timeout
- **THEN** deployment fails and begins recovery instead of recording the candidate as the new baseline

### Requirement: Failed activation restores and verifies the previous configuration
If candidate activation or verification fails, the operation SHALL restore the protected pre-deploy YAML, force-recreate only `models-enricher`, and verify the restored file digest and bounded readiness before reporting recovery complete. If restoration or readiness cannot be proven, the outcome SHALL be recovery-required and the previous baseline SHALL remain authoritative.

#### Scenario: Candidate activation fails and recovery succeeds
- **WHEN** the candidate replacement fails but the restored configuration becomes healthy with the expected previous digest
- **THEN** the operation reports candidate failure with completed recovery and does not advance the baseline

#### Scenario: Restored service cannot be proven healthy
- **WHEN** restoration, recreation, digest verification, or readiness verification fails
- **THEN** the operation reports recovery-required and does not claim deployment or rollback success

### Requirement: Models-enricher receipts are bounded and sanitized
Deploy and rollback receipts SHALL record candidate and observed configuration digests, container and image identities, mutation scope, readiness result, protected-container identity checks, recovery result, and final outcome. Receipts SHALL NOT contain credentials, environment dumps, full YAML content, raw model inventories, or unbounded logs.

#### Scenario: Configuration deployment succeeds
- **WHEN** the expected file is loaded and readiness verification passes
- **THEN** the receipt records bounded identities and checks without private configuration content or secrets

#### Scenario: Recovery is incomplete
- **WHEN** the previous configuration cannot be restored and verified
- **THEN** the receipt reports recovery-required rather than successful deployment
