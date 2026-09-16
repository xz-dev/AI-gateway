## Purpose

Provide operators with a bounded, explicitly triggered way to refresh the diagnostic models table immediately while retaining a trustworthy last-good view when fresh collection fails.

## ADDED Requirements

### Requirement: Models table exposes an explicit force-refresh action
The diagnostic models-table surface SHALL keep `GET /models-table` read-only and SHALL expose `POST /models-table/refresh` as the force-refresh action. The server-rendered table SHALL include a form control that submits this action without requiring JavaScript.

#### Scenario: Normal table view stays read-only
- **WHEN** an operator requests `GET /models-table`
- **THEN** the system renders the current models table without initiating a forced upstream refresh

#### Scenario: Operator submits refresh
- **WHEN** an operator submits the server-rendered refresh form
- **THEN** the browser sends `POST /models-table/refresh`
- **AND** the response renders the result of that refresh attempt as a models table

#### Scenario: Refresh path rejects GET
- **WHEN** a client requests `GET /models-table/refresh`
- **THEN** the system rejects the request without initiating refresh work

### Requirement: Force refresh covers the full catalog collection chain
A force-refresh request SHALL bypass eligible enricher read caches for the CPA and AISIX raw snapshots, enabled channel inventories, configured metadata sources, and catalog projection. It SHALL perform at most one active refresh collection at a time and SHALL NOT trigger the independent `cpa-model-sync` service.

#### Scenario: Newly loaded CPA provider becomes visible
- **WHEN** CPA has loaded a newly configured provider and an operator submits a force refresh
- **THEN** the refresh re-reads the current CPA inventory and channel data instead of waiting for the normal cache cadence
- **AND** the resulting table includes the provider's eligible prefixed models when collection succeeds

#### Scenario: Concurrent refresh requests share work
- **WHEN** multiple operators submit force refresh while one refresh is active
- **THEN** the system coalesces them onto one bounded upstream collection
- **AND** each request receives the result associated with that collection

#### Scenario: CPA model sync remains independent
- **WHEN** an operator submits a force refresh
- **THEN** the system does not invoke, restart, or add a control request to `cpa-model-sync`

### Requirement: Refresh result reports freshness
The rendered response to a force-refresh request SHALL state whether fresh collection succeeded and SHALL display the completion time. The page SHALL remain server-rendered and SHALL NOT require browser-side JavaScript or a secondary catalog request.

#### Scenario: Successful refresh status
- **WHEN** the full forced collection and table build succeed
- **THEN** the rendered page identifies the result as freshly collected
- **AND** displays the refresh completion time

### Requirement: Failed refresh preserves the last usable table
If any required force-refresh phase fails, the system SHALL render the last usable models table with a prominent failure warning and the failed attempt time. It SHALL NOT label the retained table as freshly collected. If no last usable table exists, the system SHALL return an error page with a non-success HTTP status.

#### Scenario: Refresh fails after a usable table exists
- **WHEN** a force-refresh phase fails and a last usable table is available
- **THEN** the response displays that last usable table
- **AND** prominently identifies that forced refresh failed and the displayed data is retained data
- **AND** displays the failed attempt time

#### Scenario: Refresh fails without retained data
- **WHEN** a force-refresh phase fails before any usable table has been produced
- **THEN** the system returns a non-success HTTP status and an error page
- **AND** does not display an empty table as a successful refresh

### Requirement: Force-refresh action remains diagnostic-only
The force-refresh route SHALL be reachable only through the existing localhost/Tailscale models-table diagnostic exposure and SHALL NOT be exposed by the public model-catalog entrance.

#### Scenario: Diagnostic route is available on port 9083
- **WHEN** an authorized network client accesses the existing `:9083` diagnostic surface
- **THEN** it can view the refresh form and submit the refresh action

#### Scenario: Public catalog surface does not expose refresh
- **WHEN** a caller accesses the public model-catalog entrance
- **THEN** `/models-table/refresh` is not routed to the force-refresh handler
