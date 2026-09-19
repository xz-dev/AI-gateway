## ADDED Requirements

### Requirement: Routine adopted operations preserve the completed cutover boundary

After the serving baseline has been explicitly adopted, routine AISIX image and route operations SHALL follow the production operations control and AISIX route lifecycle contracts. Routine operation approval SHALL NOT authorize initial cutover, account-placement migration, Headroom configuration, removal of recovery evidence, or reintroduction of New API or CPA logical-model routing. Existing management isolation, pairwise network boundaries, bounded resource use, and explicit image selection SHALL remain in force. Runtime API observation or maintenance SHALL use the dedicated internal management path with the admin key; any temporary operator tunnel or relay SHALL require explicit approval and closure, never a standing external binding.

#### Scenario: Apply a routine route edit

- **GIVEN** the existing serving baseline and AISIX resources have been adopted
- **WHEN** the operator approves a route membership change
- **THEN** only the reviewed AISIX changes and approved direct-model cleanup are applied
- **AND** Sub2API account placement, Headroom settings, network isolation, and retained recovery evidence remain unchanged

#### Scenario: Routine operation fails

- **WHEN** a routine AISIX operation fails verification
- **THEN** recovery follows the approved prior AISIX configuration or artifact path
- **AND** it does not restore a retired logical-model router or expose the Admin API externally
