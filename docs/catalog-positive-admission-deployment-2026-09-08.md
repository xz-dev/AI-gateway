# Positive catalog admission deployment — 2026-09-08

**Deployed and verified.** Explicit user authorization covered this Go directory change. Individual manual commands changed only the `models-enricher` image reference and recreated that service. CPA, Rust, Sub2API, APISIX, routing, credentials, Go configuration and resource limits were not changed. No inference replay or Git commit was performed.

## Deployed artifacts

- Image: `localhost/models-enricher:identity-20260908T004700Z`.
- Docker image ID: `sha256:9ddf0f2b6603a32eb06cb1c60d5cfa3a4c565950aa78a1d896f194cbbea4f659`.
- Executable SHA-256: `b8ec1b5485048caa6f6dbee3f3639064c7003ea5d8184fe9f166dcc6c4b28796`.
- Image archive SHA-256: `6beea96e5fdbc7a6aecd6eeadb7ec8c8f124d8e0be391c47f5af2b18e18ba092`.
- Unchanged mounted configuration SHA-256: `e0bfd4d5dc14a7e8d67ec0a8735e9358ab4d5fc7718bfc257572885113ce45c4`.
- Container: `fe1c064d5994e142e0803688eaa3153a6dda01666f6cf4597a0589bda28e47d7`, started `2026-09-08T01:10:34.720792172Z`.

The candidate used pinned build/runtime images and a task-scoped source snapshot. Podman/Docker filesystem layers and runtime image settings matched; the running executable matched the tested candidate. The candidate environment file was derived from the fresh production backup. Effective Compose differed only in the Go image. The switch used `docker compose up -d --no-deps --no-build --pull never models-enricher`.

## Acceptance

Same-version captures used `client_version=v0.65.0`:

| Check | Result |
| --- | --- |
| Native CPA membership | 319 records in both captured baselines |
| Full directory | 242 → **188** records; 54 successful Management nonmatches removed |
| Positive admission | Exactly the expected current matched set plus five explicit statics; zero unmatched retained records |
| OAuth | 33 matched public IDs; native fields preserved exactly |
| Explicit statics | All five retained |
| Full response | 8,579,921 bytes; SHA-256 `5f8f66862f979639c8f0e41c8d02219c4d89755213590c25c8c724ed85c6b65f` |
| Same-user authorization | 124 allowed IDs; front returned 66 records, the exact ordered full-record intersection |
| Conditional / denial | 304 empty; anonymous and deliberately invalid credentials both 404 empty |
| Missing / empty version | Directory APISIX matches CPA using the same query |
| Runtime | All 48 services running; only Go recreated; other 47 identities, images and restart counts unchanged |
| Configuration / resources | HostConfig unchanged, CPA non-model projection and non-target files unchanged; no new OOM events |
| Private cache | 44 files, 21,521,081 bytes; directory 0700, files 0600; existing 64MiB tmpfs and 32MiB/256-entry cache limits unchanged |

The approved user credential was supplied through stdin, not logged or saved. No alternate user identity was tried. The verification read only existing Management metadata, registrations, definitions and aliases; it did not download OAuth credential files. No identity-read failure was observed in the captured Go log.

## Local checks and limits

The latest race suite passed 125 test/subtest events; vet passed. The normally skipped `TestCatalogHTTPFixture` was separately executed with the exact candidate image and freshly copied production APISIX/sidecar files in a network-isolated fixture. Its explicit Management declarations were updated without relaxing its size, numeric-fidelity or concurrency assertions. An initial fixture edit used a nonexistent test-helper field and failed compilation; that failure was corrected and is not behavioral red evidence.

The offline chain passed 251 exact records (11,076,924 bytes), two concurrent admitted requests, large integer/decimal fidelity, authorization-first behavior, native failure, the 16MiB boundary, page delivery and sidecar path restrictions. Component peaks: Go 121.81/256MiB, sidecar 20.93/32MiB, catalog APISIX 71.68/128MiB, front APISIX **127.61/128MiB**; no max/OOM events. The front has very little measured headroom: this short run is not a sustained-load guarantee, and no budget was raised.

Production Go final current/peak usage was 53.38/192.02MiB within its unchanged 256MiB limit. These are bounded observations, not maximum-concurrency, inference or metadata-completeness certification.

An initial final-state assertion hashed the ordered environment-variable list. HostConfig matched, and the actual key/value map matched the old image defaults plus unchanged Compose environment. Only the verifier comparison was corrected; remaining cross-service, OOM and cache checks then passed. The failure receipt was preserved. No catalog requests were repeated for that correction.

## Evidence and recovery

- Local: `/root/.cache/catalog-positive-20260908T004700Z/`.
- Remote: `/root/AI-gateway/.catalog-positive-20260908T004700Z/`.
- Retained: fresh environment/Compose/config backup, source snapshot, build/race/vet logs, image receipts/archive, offline resource result, before/preswitch/startup/final states, private catalog captures and read-only verifier results.
- Previous image retained: `localhost/models-enricher:readcache-20260907T102111Z`.

Rollback, if separately needed, is limited to restoring the backed-up environment image reference and manually recreating only Go. Do not restore historical CPA/DB state or restart unrelated services. Only this task's `.go-catalog-release.lock` is released at closeout; backups and images remain.
