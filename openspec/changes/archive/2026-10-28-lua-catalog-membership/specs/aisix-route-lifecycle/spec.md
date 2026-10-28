## MODIFIED Requirements

### Requirement: Direct models with unregistered targets are classified before pruning

When an audit finds AISIX direct models whose target ID is absent from CPA's available-model list and from its OAuth registrations, the operation SHALL classify each one before proposing to delete it. The classes are:
- **disabled channel**: the target belongs to a configured CPA channel that is disabled;
- **unregistered**: no enabled channel configuration and no registration names the target;
- **enabled but unavailable**: an enabled channel names the target, but CPA does not list it.

Only direct models in an explicitly approved deletion set SHALL be removed. Each removal SHALL take every logical-route target that names the model with it. A direct model in the enabled-but-unavailable class SHALL NOT be proposed for deletion until a read-only diagnosis of its cause is recorded. If the diagnosis does not show a transient cause, the decision SHALL go to the user, not be taken by the operator.

The removal SHALL follow the existing route-lifecycle rules:
- retained routes keep their order and unedited fields;
- a route that would be left without any target SHALL be rejected before deploy, not deleted implicitly;
- no CPA, Sub2API, provider-key, caller-key or Squid resource SHALL change.

After activation, verification SHALL show that the catalog lost only the members that the removed direct models alone contributed, and that no new direct model or route target names an ID that CPA does not register.

#### Scenario: Approved dangling entries are pruned

- **GIVEN** `zakk/gpt-6-astra` (disabled channel) and `xl/gpt-6-astra` (unregistered) are in the approved deletion set
- **AND** route `gpt-6-astra` lists `codex/gpt-6-astra`, `axis/gpt-6-astra`, `zakk/gpt-6-astra` and `aihub/gpt-6-astra`
- **WHEN** the cleanup is applied
- **THEN** both direct models are absent from the effective AISIX resources and from the catalog
- **AND** route `gpt-6-astra` keeps `codex/gpt-6-astra`, `axis/gpt-6-astra` and `aihub/gpt-6-astra` in that order

#### Scenario: Enabled-but-unavailable entry waits for diagnosis

- **GIVEN** `axis/gpt-6-sol` is configured on an enabled channel but absent from CPA's available list
- **WHEN** the dangling set is pruned
- **THEN** `axis/gpt-6-sol` and its route targets remain
- **AND** the record states its diagnosed cause, or that the user must decide

#### Scenario: Catalog loses only solely contributed members

- **GIVEN** a removed direct model's ID is not registered by CPA, not matched by the extra CPA list (`EXTRA_CPA`) of the profile's `catalog.lua`, and not a static model
- **WHEN** the cleanup is activated and the catalog refreshes
- **THEN** that ID leaves the catalog, and every other member and every passthrough row is unchanged apart from list order and Codex `priority`
