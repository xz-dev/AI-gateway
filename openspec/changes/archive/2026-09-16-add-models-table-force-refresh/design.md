## Context

See `proposal.md` for motivation. Current models-table delivery is a server-rendered `GET /models-table` response from `models-enricher`, routed by the sidecar-only `apisix-models` instance and published through the existing localhost/Tailscale `:9083` relay. The enricher owns a five-minute raw routing snapshot cadence, five-minute projection cache, and read-through caches for upstream inputs. `cpa-model-sync` is a separate ten-minute service and has no control seam intended for this page.

The current handler combines table and JSON catalog builds around an immutable routing generation. Reads intentionally do not trigger collection, so the new operator action must be explicit and must not weaken inference hot-path or ordinary catalog behavior.

## Goals / Non-Goals

**Goals:**

- Give operators one no-JavaScript action that refreshes current CPA/AISIX membership, enabled channel inventories, metadata sources, and table projection.
- Reuse existing collection, publication, size limits, timeouts, and singleflight behavior.
- Preserve a last-good table for diagnosis when forced collection fails.
- Keep exposure identical to the existing diagnostic page.

**Non-Goals:**

- Triggering, restarting, or controlling `cpa-model-sync`.
- Changing periodic refresh intervals.
- Adding a public refresh API, background job system, persistent refresh history, or JavaScript polling.
- Guaranteeing that upstream provider configuration not yet loaded by CPA appears.

## Decisions

### 1. Use `POST /models-table/refresh`, not a refreshing GET

The existing page gains a plain HTML form with `method="post"` and `action="/models-table/refresh"`. `GET /models-table` remains observational, and GET on the refresh path is rejected.

Alternative considered: `GET /models-table/refresh` or `?refresh=1`. Rejected because browser prefetch, crawlers, bookmarks, and reloads could produce upstream work from a nominally safe request.

### 2. Put refresh orchestration in `models-enricher`

`models-enricher` already owns raw CPA/AISIX collection, channel discovery, metadata fetches, projection caching, and HTML rendering. The refresh handler calls one new bounded orchestration seam there rather than teaching APISIX, nginx, or the relay about cache internals.

`apisix-models` receives one exact POST route for `/models-table/refresh`, with the same real source-address restriction and enricher upstream as the existing GET route. No wildcard or public-front route is added.

Alternative considered: restart the enricher or call multiple internal services from APISIX. Rejected because restart is disruptive and multi-service orchestration duplicates ownership.

### 3. Introduce request-scoped cache bypass instead of globally clearing caches

A force-refresh context/option propagates through the existing collection path. Eligible read-cache lookups are skipped for that attempt, while successful responses may replace their normal cache entries. Projection lookup is skipped and the resulting table is keyed to the resulting raw generation.

This avoids a global cache flush racing ordinary readers. Existing immutable generations remain valid for in-flight requests.

Alternative considered: delete all cache entries before building. Rejected because failures would destroy usable fallbacks and concurrent ordinary requests would observe avoidable cache misses.

### 4. Coalesce refresh requests using existing synchronization boundaries

The refresh operation shares one active upstream attempt. Existing snapshot refresh serialization and catalog build serialization are reused or minimally extended; no second worker pool or queue is added. Waiting requests use their request context, while the shared operation remains bounded by existing overall and per-channel deadlines.

Alternative considered: reject concurrent refreshes with 409. Rejected because sharing one result is simpler for operators repeatedly clicking or multiple open tabs.

### 5. Keep an in-memory last-good rendered table

After any successful table build, the enricher stores the bounded HTML result and its collection timestamp in memory. A failed forced refresh renders that last-good table with a server-generated warning banner and failed-attempt timestamp. No disk or database persistence is introduced; process restart may remove the fallback.

The warning must distinguish:

- fresh forced result;
- retained result after failed force refresh;
- no retained result, which returns a non-success error page.

Alternative considered: rebuild from stale raw inputs after failure. Rejected because it can combine generations and make provenance unclear; retaining the exact last successful rendered artifact is easier to label truthfully.

### 6. Preserve normal-read semantics

Only POST refresh carries the bypass option. `GET /models-table`, `GET /v1/models`, `/routing-index`, and inference remain unchanged. The refresh response itself returns the HTML table directly, avoiding redirects that could lose refresh status or require a second catalog build.

## Risks / Trade-offs

- **Force refresh increases upstream load** → retain existing concurrency bounds, deadlines, response limits, and singleflight; expose one exact diagnostic route only.
- **A slow operator request may disconnect** → use request cancellation where safe while keeping shared work bounded; no unbounded detached job.
- **Last-good table is lost on enricher restart** → accepted; persistence would add state and stale-data lifecycle complexity. The page returns an explicit error until a successful table exists.
- **CPA configuration may not yet be loaded** → refresh can only observe current CPA state; page wording must not claim it controls CPA or `cpa-model-sync`.
- **Bypass path could accidentally affect public catalogs** → make bypass request-scoped and cover normal GET/non-public routing with regression tests.

## Migration Plan

1. Add tests for method routing, full-chain bypass, concurrent coalescing, success status, and last-good failure rendering.
2. Add the enricher refresh orchestration and server-rendered form/status output.
3. Add exact `apisix-models` POST routing and update route fixtures.
4. Update operator documentation for `:9083/models-table` and its refresh semantics.
5. Deploy only the updated enricher and `apisix-models` configuration through the existing cautious rollout; verify GET remains read-only, POST refresh returns a fresh timestamp, public routes reject the action, and existing catalogs/inference remain healthy.
6. Roll back by restoring the prior enricher image and APISIX route file; no data migration is required.
