## Why

The models table can remain stale for up to the combined CPA synchronization and enricher cache cadences after an operator adds or changes a provider. Operators need a bounded, explicit way to refresh the diagnostic catalog immediately and see whether fresh data was obtained without restarting services.

## What Changes

- Add a force-refresh action to the models-table diagnostic surface.
- Keep `GET /models-table` read-only and add `POST /models-table/refresh` for the state-changing operation.
- Add a server-rendered “强制刷新” button that submits the POST and displays the resulting table.
- Force a singleflight refresh of the full enricher catalog chain: CPA/AISIX raw snapshot, channel inventories, metadata sources, and rendered catalog projection.
- On refresh failure, preserve and display the last usable table with a prominent warning and refresh timestamp instead of replacing it with an empty/error-only page.
- Keep the action within the existing localhost/Tailscale-only models-table exposure; do not expose it through the public catalog entrance.
- Do not trigger or add a control API for the independent `cpa-model-sync` service.

## Capabilities

### New Capabilities

- `models-table-force-refresh`: Operator-triggered, bounded refresh and last-good fallback behavior for the server-rendered models table.

### Modified Capabilities

- `codex-models-enricher`: Extend catalog snapshot/cache behavior with an explicit operator refresh path while preserving periodic refresh and normal catalog request semantics.

## Impact

- Affected components: `models-enricher`, `apisix-models`, models-table HTML rendering/tests, and operator documentation.
- New internal API: `POST /models-table/refresh`, reachable only through the existing `:9083` localhost/Tailscale diagnostic path.
- No new dependency, database, persistent store, public route, or `cpa-model-sync` control interface.
