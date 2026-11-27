# Gateway component candidate verification — 2026-11-27 UTC

Candidate evidence only. This record does not report a production change or authorize an apply.

| Component | Selection | Candidate evidence |
| --- | --- | --- |
| APISIX front and model planes | 3.21.0-debian | Exact image; both plane config tests, custom Lua and catalog route checks; full topology ladder |
| Cloudflared | 2026.11.2 | Exact image, offline version check and guarded preview |
| CLIProxyAPI | 8.1.7 capacity and SSE fork | [Publish job 45916658806](https://github.com/xz-dev/CLIProxyAPI/actions/runs/45916658806); exact image loaded both configured plugin versions in isolation |
| AISIX | 1.12.0 capacity fork | [Publish job 45916642321](https://github.com/xz-dev/aisix/actions/runs/45916642321); old/v1 → new/v2 → old/v1 replay preserved model visibility, key restrictions and ordered failover |
| Sub2API | 0.2.25 capacity fork, r3 | [Publish job 45924292136](https://github.com/xz-dev/sub2api/actions/runs/45924292136), [full CI 45924292142](https://github.com/xz-dev/sub2api/actions/runs/45924292142), [security scan 45924292135](https://github.com/xz-dev/sub2api/actions/runs/45924292135); exact amd64 image retained pricing data and returned healthy across schema 304 → 305 → 305 |
| PostgreSQL | 18.7-alpine3.24, refreshed build | Synthetic upgrade and old image reopening the new image's writes |
| Redis | 8.12.0-alpine3.23 | Same-version restart, empty cache reconstruction, and old image restore from a pre-switch snapshot. The old image cannot open the new image's RDB. |
| ZCode provider sidecar | 4.9.1 | Exact amd64 image booted with synthetic credentials and no network. No-key health rejected; authenticated health and existing model inventory returned 200. This is not a provider-inference check. |

No compatible newer image was identified in this round for Alpine namespace guards, Socat, Squid, tun2proxy, AISIX status, model catalog, CPA model sync, SSE keepalive proxy, keeper or the Responses bridge. Keep their current images; the keeper and bridge default branches are not substitutes for the deployed builds.

## Integration

- Standard ladder `20261127T015913Z-450380`: 10/10 passed through 256 MiB. The proposed standard lock is generated from this run's manifest and verdict. The Podman wrapper verified `memory.swap.max=0` before tests for all seven bounded containers. The input and runtime-reported mock base digests resolve to the same amd64 image ID; the lock records the runtime-reported digest.
- Full topology `20261127T020504Z-514022`: 12/12 passed, including 1 GiB HTTP and WebSocket cases through both APISIX planes, with the new AISIX v2 fixture and exact candidate digests. No production upstreams were used.
- Legacy nightly lock, unchanged: `20261127T021031Z-700270` passed both HTTP and WebSocket smoke rungs with the original v1 resources. The manifest hashes the selected resource fixture in both modes.

## Promotion boundary

Use only a fresh preview for each activation. The existing production AISIX resources match the last managed baseline and the reviewed November 20 rollout; migrate only that input's format. The older controller copy differs and is not an input to the migration. Cut over and restore AISIX image and resource file together. Keep PostgreSQL durable data and the pre-switch Redis cache backup separate. Approve the scopes, interruption window, verification traffic and recovery before the single promotion. Do not reuse preview digests after a prior step changes live inputs.
