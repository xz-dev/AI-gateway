## 1. Refresh Semantics and Tests

- [x] 1.1 Add failing handler tests for read-only `GET /models-table`, POST-only `/models-table/refresh`, fresh status/timestamp rendering, and no-JavaScript form submission; verify targeted Go tests fail for missing behavior.
- [x] 1.2 Add failing collection tests proving a forced request bypasses CPA/AISIX, channel-inventory, metadata-source, and projection caches while normal GET requests retain existing cache/cadence behavior; verify request counters distinguish forced from normal reads.
- [x] 1.3 Add failing concurrency and fallback tests proving simultaneous POSTs share one refresh, a failed refresh renders the exact last-good table with warning/time, and failure without last-good data returns non-success; verify tests cover bounded request cancellation and no `cpa-model-sync` call.

## 2. Enricher Implementation

- [x] 2.1 Add request-scoped cache-bypass support to existing read paths without globally clearing caches; verify ordinary cache tests remain green and forced-read counter tests pass.
- [x] 2.2 Add bounded singleflight force-refresh orchestration around the existing immutable snapshot publication and catalog build paths; verify concurrent refresh and generation-pinning tests pass under `go test -race`.
- [x] 2.3 Add in-memory last-good table storage plus fresh/failed status metadata and render the POST response directly; verify successful and failed refresh HTML tests pass with escaped, server-rendered output and no scripts.
- [x] 2.4 Add the refresh form to normal models-table HTML and register exact method-aware handlers so GET refresh is rejected; verify existing table representation, columns, sorting, and JSON catalog tests remain unchanged.

## 3. Diagnostic Routing and Documentation

- [x] 3.1 Add an exact `POST /models-table/refresh` route to `apisix-models` with the same source-address restriction and enricher upstream as the existing table route; verify route fixtures show port 9083 access while public-front fixtures do not expose it.
- [x] 3.2 Update README/operator documentation with POST semantics, full-chain scope, five/ten-minute cadence distinction, last-good warning behavior, and explicit exclusion of `cpa-model-sync`; verify referenced paths and commands against source.

## 4. Integrated Verification

- [x] 4.1 Run targeted Go tests, `go test -race ./...` for `models-enricher`, APISIX/catalog HTTP fixtures, and `git diff --check`; verify all pass with no new dependency.
- [x] 4.2 Run an isolated HTTP acceptance check proving GET does not force collection, POST refresh returns a completion timestamp and newly introduced provider, concurrent POSTs coalesce, failed POST preserves last-good HTML, and public entrance rejects the path; record bounded request counts and statuses.
- [x] 4.3 Validate the OpenSpec change with strict validation and confirm implementation remains limited to planning scope until a separate `/opsx-apply` request.
