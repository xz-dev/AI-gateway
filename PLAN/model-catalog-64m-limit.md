# Raise model-catalog limits to 64 MiB

## Scope

Raise only existing model-catalog read, intersection, final-response, and completed-result cache limits from 16/32 MiB to 64 MiB. Keep inference-body and unrelated metadata-source limits unchanged. Do not add streaming or memory optimizations.

## Progress

- [x] Raise Sub2API catalog-read environment default to 64 MiB.
- [x] Raise public APISIX basic/original/final catalog limits to 64 MiB.
- [x] Raise models-enricher CPA native/API-call reads and completed-result cache to 64 MiB.
- [x] Update existing limit assertions and current README values.
- [x] Build local amd64 models-enricher candidate without running tests.
- [ ] Publish candidate under immutable GHCR digest.
- [ ] Back up and patch only production `.env`, Compose limit default, and APISIX Lua file.
- [ ] Recreate models-enricher and Sub2API; gracefully reload public APISIX.
- [ ] Verify direct origin and `pi --list-models --refresh`; send no inference request.

## Accepted risk

Owner explicitly selected a limit-only change and no additional capacity tests. This does not establish that a near-64-MiB catalog is safe under current concurrency or memory ceilings. Historical evidence records APISIX OOM in a different 128-MiB fixture after a 32-MiB limit experiment; current production public APISIX has a 256-MiB limit and current catalog is about 31 MiB. Stop and roll back on any OOM, restart, unhealthy state, or failed catalog refresh.
