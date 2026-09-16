# Models-table force refresh production deployment

Deployed and verified on 2026-09-16 through the existing localhost/Tailscale `:9083` diagnostic surface.

## Delivered behavior

- `GET /models-table` remains read-only and renders a no-JavaScript refresh form.
- `POST /models-table/refresh` bypasses the enricher's CPA/AISIX, channel inventory, metadata-source, and projection caches.
- Concurrent refresh submissions share one bounded collection.
- A failed forced collection returns the in-memory last-good table with a warning; it does not label stale data as fresh.
- `cpa-model-sync` is not invoked or controlled by this endpoint.
- The public Sub2API/front entrance does not route requests to the diagnostic refresh handler.

## Production artifacts

- Enricher image: `local/models-enricher:force-refresh-20260916T102741Z`
- Enricher image ID: `sha256:1dcb2c4876d9caa38a0ba9cf70daf4bffe5fd7d1419a02f33e495d55aec97467`
- Enricher binary SHA256: `26ebb4a3148bcdb63f0f3aa067564ac92f7b03e5aadecae4b1eedc90c5f08313`
- APISIX models config SHA256: `2ddd08688212088cc234d79c501d162beff11352d43b929af205277b35a41862`
- Sidecar template SHA256: `0763844dda86c273b953c03b533a05666c1f5bf692dc4d6301c9b42fccbaaf1f`
- Production evidence and file backups: `/root/rollout-models-table-force-refresh-20260916T103016Z`
- Rollback image: `local/models-enricher:rollback-before-force-refresh-20260916T102741Z`

The candidate image was derived from the running production image and replaced only the verified enricher executable, avoiding production source-tree drift.

## Acceptance evidence

- Candidate smoke test: readiness, server-rendered GET, and forced POST passed against isolated mock CPA/AISIX endpoints.
- Production `GET /models-table`: HTTP 200.
- Production forced POST: HTTP 200 with a fresh-success marker and `claudeye/glm-5.3` present.
- Two simultaneous forced POST requests both completed successfully.
- `GET /models-table/refresh`: HTTP 403 at the diagnostic sidecar boundary.
- A request to the public entrance returned the normal Sub2API frontend and did not contain the refresh handler marker.
- Controlled failure injected only into the enricher network namespace by rejecting its CPA catalog TCP connection. The forced POST returned HTTP 200 with a warning and retained the last-good table containing `claudeye/glm-5.3`.
- After removing the injected rule, a new forced POST returned fresh success.
- `models-enricher` and `apisix-models` remained healthy; `model-catalog-sidecar` remained running; all three had zero restarts.

No CPA, Sub2API, AISIX routing, account, database, or inference configuration was changed.
