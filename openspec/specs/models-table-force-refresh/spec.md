# Models Table Force Refresh Specification

## Purpose

Provide operators with a bounded, explicitly triggered way to refresh the diagnostic models table immediately while retaining a trustworthy last-good view when fresh collection fails.

## Requirements

### Requirement: Models table exposes an explicit force-refresh action
The diagnostic models-table surface SHALL expose `POST /models-table/refresh` as the explicit force-refresh action. The server-rendered table SHALL include a form control that submits this action without requiring JavaScript. `GET /models-table` SHALL render the most recently published table without waiting for upstream collection. When no usable table has been published, it SHALL immediately render a loading/no-data page with the same refresh form.

#### Scenario: Entry inside the successful-refresh window
- **GIVEN** a successful refresh completed within the configured entry window
- **WHEN** an operator requests `GET /models-table`
- **THEN** the system renders the current table without initiating another forced refresh

#### Scenario: Entry outside the successful-refresh window
- **GIVEN** the last successful refresh is older than the entry window or no successful refresh exists
- **WHEN** an operator requests `GET /models-table`
- **THEN** the system starts or joins the shared refresh job in the background
- **AND** immediately renders existing data or a loading/no-data page without waiting for that job

#### Scenario: Operator submits refresh
- **WHEN** an operator submits the server-rendered refresh form
- **THEN** the browser sends `POST /models-table/refresh`
- **AND** the response renders the result of that refresh attempt as a models table

#### Scenario: Refresh path rejects GET
- **WHEN** a client requests `GET /models-table/refresh`
- **THEN** the system rejects the request without initiating refresh work

### Requirement: Force refresh covers the full catalog collection chain
A force-refresh request SHALL bypass the enricher's eligible read caches for:
- the CPA and AISIX raw snapshots;
- management discovery and identity reads;
- `@cpa-upstream` responses needed by the compile set;
- the catalog projection.

Metadata bundled into the running image SHALL be reused, because it involves no network read. A force refresh SHALL perform at most one active refresh collection at a time and SHALL NOT trigger the independent `cpa-model-sync` service.

#### Scenario: Newly loaded CPA provider becomes visible
- **WHEN** CPA has loaded a newly configured provider and an operator submits a force refresh
- **THEN** the refresh re-reads the current CPA inventory and the needed `@cpa-upstream` data instead of waiting for the normal cache cadence
- **AND** the resulting table includes the provider's eligible prefixed models when collection succeeds

#### Scenario: Concurrent refresh requests share work
- **WHEN** multiple operators submit force refresh while one refresh is active
- **THEN** the system coalesces them onto one bounded upstream collection
- **AND** each request receives the result associated with that collection

#### Scenario: CPA model sync remains independent
- **WHEN** an operator submits a force refresh
- **THEN** the system does not invoke, restart, or add a control request to `cpa-model-sync`

### Requirement: Refresh result reports freshness
The rendered response to a force-refresh request SHALL state whether fresh collection succeeded and SHALL display the completion time. Table pages SHALL show the last successful refresh completion and the next scheduled refresh, or explicitly state that no success/schedule exists. Failed attempts and attempts using retained metadata inputs SHALL NOT advance the successful-refresh window. The page SHALL remain server-rendered and SHALL NOT require browser-side JavaScript or a secondary catalog request.

#### Scenario: Successful refresh status
- **WHEN** the full forced collection and table build succeed
- **THEN** the rendered page identifies the result as freshly collected
- **AND** displays the refresh completion time

### Requirement: Failed refresh does not fabricate current authority
A metadata failure SHALL reuse eligible last-successful input responses and recompose the table from the current authoritative CPA snapshot, with a stale-input warning. A manual refresh without usable CPA authority SHALL return a non-success error page instead of replaying a saved table as a fresh result. A failed background refresh SHALL leave the last published diagnostic table available to later GET requests with a failure warning; this retained display SHALL NOT grant membership or routing readiness to JSON catalog requests.

#### Scenario: Background refresh fails after a usable table exists
- **WHEN** background collection fails and a published table exists
- **THEN** subsequent GET requests display that table with the failed attempt time and a warning
- **AND** the last-successful-refresh time remains unchanged

#### Scenario: Manual refresh lacks CPA authority
- **WHEN** an operator submits refresh and the required CPA snapshot is unavailable
- **THEN** the system returns a non-success HTTP status and an error page
- **AND** does not display an empty or retained table as a successful refresh

#### Scenario: Metadata source fails after a successful response
- **WHEN** the current CPA snapshot admits a newly added model but a metadata source fails
- **THEN** the table is recomposed with the newly admitted model and eligible retained metadata
- **AND** it warns that stale inputs were used instead of labeling collection fully fresh

### Requirement: Startup and scheduled refresh share the same bounded job
The service SHALL initiate one full-chain refresh in the background at startup and at every configured scheduled interval (default `30m`). Entry-triggered refresh SHALL use an independently configurable success window (default `10m`). Both durations SHALL be positive; neither SHALL impose an ordering constraint on the other. Startup, scheduled, entry and manual triggers SHALL coalesce onto the same active job. Manual requests SHALL wait for that job unless the client disconnects; disconnect SHALL NOT cancel shared work. Shutdown SHALL stop the ticker and cancel/join active work before closing its cache. Existing JSON catalog request semantics and raw routing-snapshot ownership SHALL remain unchanged.

#### Scenario: Manual request joins startup work
- **GIVEN** the startup collection is still running
- **WHEN** an operator submits the refresh form
- **THEN** no second collection starts
- **AND** the manual request waits for the startup collection and renders its result

#### Scenario: Scheduled collection keeps data warm
- **GIVEN** no operator visits the table
- **WHEN** the scheduled interval elapses
- **THEN** the same full-chain refresh job runs and publishes its result

#### Scenario: Durations are independent
- **WHEN** configuration sets the entry window to `2h` and the scheduled interval to `1m`
- **THEN** both positive durations are accepted
- **AND** an explicit zero, negative or malformed duration is rejected

#### Scenario: Shutdown stops refresh work
- **WHEN** the service shuts down with a refresh active
- **THEN** the schedule stops and active refresh work is cancelled and joined
- **AND** later triggers cannot start another job

### Requirement: Force-refresh action remains diagnostic-only
The models table and its force-refresh route SHALL be served by a dedicated front-door listener. That listener SHALL be bound only to localhost and the Tailscale address on port `9083`, and it SHALL forward these paths directly to `model-catalog`. Localhost and Tailscale are a trusted domain, so the listener SHALL NOT add source-address restrictions. The public front-door listener SHALL NOT route `/models-table` or `/models-table/refresh`.

#### Scenario: Diagnostic route is available on port 9083
- **WHEN** a client on localhost or the tailnet accesses `:9083/models-table`
- **THEN** it can view the table and the refresh form and submit the refresh action

#### Scenario: Public catalog surface does not expose refresh
- **WHEN** a caller accesses `/models-table` or `/models-table/refresh` through the public front-door listener
- **THEN** the request is not routed to `model-catalog`

### Requirement: Models table shows hidden and referenced-only models
The models table SHALL list every admitted model, including models that `include_models` hides.

Each hidden model SHALL be marked hidden and SHALL show what decided it:
- the deciding rule path and pattern, for example `include_models[1]` and `!**/gpt-image-*`; or
- "no include rule matched".

A hidden model that the compile set needs through `inherit` or `metadata_from` SHALL additionally be marked "referenced by" with the referencing slugs, and SHALL show its compiled metadata. Other hidden models SHALL show no compiled metadata, chain or source fields, because they were not processed.

The table SHALL summarize kept, hidden and referenced-only counts. These markings SHALL NOT affect JSON catalog contents.

#### Scenario: Excluded model shown with its rule
- **WHEN** `include_models: ["**", "!**/gpt-image-*"]` and CPA admits `codex/gpt-image-1`
- **THEN** the table row for `codex/gpt-image-1` is marked hidden by `include_models[1]` and shows no compiled metadata

#### Scenario: Referenced hidden model shows metadata
- **WHEN** hidden model `vendor/y` is the `metadata_from` target of kept model `pool/x`
- **THEN** its row is marked hidden, referenced by `pool/x`, and shows its compiled metadata

#### Scenario: Unmatched model shown as hidden
- **WHEN** `include_models: ["codex/**"]` and CPA admits `other/m`
- **THEN** the row for `other/m` is marked hidden with "no include rule matched"
