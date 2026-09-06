# Layered metadata production deployment

Verified 2026-09-06, following explicit deployment confirmation.

## Released artifact

- Host: `100.94.238.35`, `~/AI-gateway`.
- Enricher: `models-enricher:layered-eac910bbfb2b`.
- Image: `sha256:d11ea70e28acadea41de7bdf1c158c7aee0c82688b3098e1864b5ad5ac5462ff`.
- Binary: `sha256:eac910bbfb2b92d5b2b70ec220fa1b3315ef00ac51978d29c2d77de2ce0ab0e0`.
- Image selector persisted. Front Lua installed and APISIX gracefully reloaded; mounted HTML updated. Routing, topology, limits and other deployment configuration unchanged.

## Actual acceptance

- Isolated candidate: 265 models, 4,534,595 bytes, no membership change against the prior catalog; identities and output aliases aligned; existing CPA credential values absent from the response.
- Production full catalog: 265 models. Caller entitlement set: 92 IDs; its intersection with the full catalog: **71 visible models**, with 194 excluded.
- Every visible record equals its corresponding complete original record, using precision-preserving JSON comparison. Both fresh and normal `v0.65.0` requests passed.
- Actual cache HIT, final ETag/304, authorization variance, aligned routing IDs and output aliases passed.
- Missing and invalid credentials return **empty 404**, preserving production's existing opaque rejection behavior. Sidecar restricted paths remain closed and missing client version remains 400.
- Production browser at `http://100.94.238.35:9083/models-table`: 265 rows, nine columns, 1,142 explicitly unknown cells, no page error, one same-origin catalog request.
- Other 46 services retain their container IDs, images and start times. Services running, no reported container OOM. Snapshot memory: enricher 39.21/256 MiB; front APISIX 104.1/128 MiB; sidecar 1.621/32 MiB. This is not load-test evidence.

## Rollback event and boundaries

The first acceptance attempt incorrectly expected public rejection status 401. Automatic rollback restored v0.7.5 and the original Lua/page. Read-only checks established that direct Sub2API returns 401 while the existing production front returns empty 404 for both missing and invalid credentials. This was an acceptance assumption error, not a demonstrated business regression. The offline rejection fixtures were corrected and rerun; the **same frozen binary/image and business code** were then deployed successfully.

No inference requests, account/permission changes, Core/plugin changes, commits, pushes or additional production configuration repairs were performed. APISIX reported its existing file-descriptor-limit warning during reload; no limit setting was changed.

Sanitized detailed receipt: `/tmp/cpa-layered-apply.m0RRXw/production-acceptance.json` on the workstation. Temporary production staging/rollback assets at `~/AI-gateway/.layered-eac910bbfb2b` were deleted after successful acceptance and explicit cleanup confirmation. Runtime images, volumes, live files and other deployment assets were not deleted. Local sanitized receipts remain available.
