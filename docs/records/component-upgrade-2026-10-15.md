# Production component update — 2026-10-15

## Outcome: 6 of 6 candidates deployed (CPA and quota added in follow-ups; see end)

The operator approved six component updates after backup and validation, including a validated Sub2API database migration. All six are now deployed. CPA and the quota plugin were initially held, then upgraded on operator directive (see "CPA follow-up" and "Quota plugin follow-up"). The quota SQLite database remains integrity-failed and was not repaired. Sub2API was initially held, then upgraded after the operator explicitly requested completing its runtime and isolation fixes.

| Component | Before | Production after this run | Result |
| --- | --- | --- | --- |
| model-catalog-sidecar | nginx-unprivileged 1.31.6-alpine, previous build | Same version, new digest | Deployed |
| provider-sidecar | ZCode 4.8.0 | ZCode 4.8.1 | Deployed |
| AISIX | 1.5.0 | 1.6.0 | Deployed |
| Sub2API | 0.2.15-pr7157-pr7217-pr3172-2ddb824 | 0.2.16-pr7157-pr7217-pr3172-fc7aa1b | Deployed after runtime repair and independent isolation gate |
| CPA | 8.0.24-pr6189-pe8f773dd3e43 | 8.0.29-pr6189-pe8f773dd3e43 (official tag 5fc580b1) | Deployed in operator-directed follow-up |
| quota plugin | 0.19.6 | 0.21.1 | Deployed in operator-directed follow-up; SQLite still integrity-failed |

Deployed image references:

```text
MODEL_CATALOG_SIDECAR_IMAGE=docker.io/nginxinc/nginx-unprivileged:1.31.6-alpine@sha256:884e0b7f42f303641b0f4e78755aea647521dca0b89ca3be7992ad78b5c07434
PROVIDER_SIDECAR_IMAGE=ghcr.io/tridefender/zcode-proxy:4.8.1@sha256:e0af03445dfd68d50ddb71165847df51e29a3c2e3cc9545045faef12e6d3948b
AISIX_IMAGE=ghcr.io/api7/aisix:1.6.0@sha256:26eae08b5c94291050f179d3c0a56ca154f40751f5c755b5922b044efc523690
SUB2API_IMAGE=sha256:c5a4bbdda5a873ff39f7951bf2f341331e8394a6558493584078d731544f7127
```

## Independent production acceptance

Parent verification after deployment established:

- 48 managed containers running; all 11 configured healthchecks healthy.
- Only `model-catalog-sidecar`, `provider-sidecar`, `aisix`, and `sub2api` container identities changed from the original protected baseline. Relative to the later Sub2API-specific baseline, only Sub2API changed; its restart count was zero.
- CPA's container identity and image reference remained unchanged. Quota's configured version remained 0.19.6 and its library bytes matched the pre-update archive at the hold checkpoint.
- Only the four image variables above changed in the effective `.env` map over the full round. The Sub2API follow-up changed only `SUB2API_IMAGE` relative to its immutable pre-cutover snapshot.
- Final production `.env` SHA256: `8f32c136ea94b557672da54a82099ec5a13bc99c9192b5f16d754df674e4d5aa`.
- Sub2API's exact Docker image ID, binary SHA256, version, `/health` HTTP 200, complete runtime paths, and 292 → 296 migration history were independently verified after cutover. Exactly four reviewed migration filenames/checksums were added; all previous filename/checksum/applied_at tuples remained identical.
- Both Compose files and AISIX `config.yaml` / `resources.yaml` remained byte-identical to the protected archive.
- `nginx -t` passed; the production status page returned HTTP 200.

The deployment worker also recorded `/models-table` HTTP 200, ZCode offline-copy `/health` HTTP 200 and live healthy state, and AISIX live admin model/status reads. The provider-sidecar tunnel identity was preserved. AISIX's admin listener did not bind in the network-disabled fixture for either old or new version; that fixture limitation was not treated as proof of live admin compatibility.

No explicit paid-inference probe was issued by the rollout commands. The first Sub2API startup rehearsal did not establish blocked egress, so zero automatic upstream requests cannot be claimed for that attempt. The later parent-owned gate enforced a network-disabled namespace and superseded that deficient rehearsal. Full public SSE/WebSocket regression, long-duration soak, and production database restore were not performed.

## Why CPA and quota were initially held (earlier checkpoint; CPA superseded by "CPA follow-up")

Before any component recreation, Python SQLite 3.46.1 `PRAGMA integrity_check` failed on the fresh online backup. A subsequent read-only check of the live quota database reported the same duplicate-page and out-of-order rowid errors. The cause was not established. This run neither repaired nor restored the database.

The quota backup is preserved evidence, **not an integrity-verified recovery copy**.

The CPA candidate was additionally rejected: although labeled `v8.0.29`, it was built from `a19a8ecde3eb83ced03be2fff625f3dd6128324a`. Independent GitHub API verification showed the actual `v8.0.29` tag points to `5fc580b1d72d87a7bcfe5cfb6f479c1aea5625b3`, eleven commits ahead. Tests on the older snapshot do not validate the requested release. The mislabeled image was not deployed.

Quota 0.21.1 was downloaded and its published archive checksum verified by the worker, but it was not installed in production. Its new calibration behavior has not passed a trusted production-copy acceptance gate.

## Sub2API: rejected prototype, accepted replacement, production cutover

The official `v0.2.16` tag resolves to commit `01842576b3d5c1e2545b5a99ad555d36e28ed306`. Production's downstream #7157 rerank, #7217 session affinity, and #3172 WebSocket keepalive changes were preserved. Binary SHA256: `264d54cee17d6cdcbce9449184623c33acefbb095b7d1867a597659f1a201f31`.

The first packaged candidate was rejected because it lacked the production entrypoint, resource directory, `pg_dump`, and `psql`. Its startup rehearsal used an ordinary bridge without enforced egress isolation. It was not deployed. These failures remain recorded, not retroactively treated as passing.

After explicit operator confirmation, the accepted replacement reused the exact production runtime base, replacing only the verified binary and upstream resources. Entrypoint, command, user, environment, working directory, volumes, healthcheck, PostgreSQL 18.6 tools, and libraries were preserved.

The parent independently restored the real PostgreSQL backup into a new fixture with `--network none`; Redis and the candidate shared that exact namespace. Assertions verified only `lo`, no non-loopback routes, and no published ports. `/health` returned OK. Migration history changed 292 → 296 with exactly the four reviewed additions; all old filename/checksum/applied_at tuples remained identical. Counts and ID fingerprints for accounts/users/groups/API keys/usage logs were unchanged, as were complete-record fingerprints for the first four tables, excluding the newly added `groups.i18n` field. Receipt: `/var/tmp/ai-gateway-update-20261015/sub2api-isolated-gate/gate-receipt.json`.

The same verified archive SHA256 `e886e4fc30694c4c4a22761363c98d8c1f0ff766ef1acacf2f833103c50a78cb` loaded with a different production Docker image ID than its local Podman ID `dc2fe5d5…`. The parent accepted the mapping only after independently comparing all 14 ordered RootFS layer diffIDs, nine Config fields including labels, OS, and architecture. Production is pinned to immutable Docker ID `c5a4bbdd…7127`, not a mutable tag. This establishes runtime equivalence; the precise serialization cause of the differing IDs was not independently diagnosed. Mapping evidence: `sub2api-deploy/docker-identity-equivalence.json`.

A fresh immutable pre-cutover snapshot was then taken. Only Sub2API was recreated through the existing Ansible drift/capacity gates with the preloaded-image profile. Final readiness, binary/version, full runtime files, fleet invariants, and migration history were verified by the deployment worker and independently by the parent.

The four new migrations now applied in production are `242_group_i18n.sql`, `243_gemini_cached_contents.sql`, `244_gemini_cached_contents_channel_usage.sql`, and `245_gemini_cached_contents_mutation_lease.sql`. Migration identity uses the complete filename, not just its numeric prefix; the two `242_*` files were not renumbered. Checksums follow the runner's trimmed-content semantics. These schema additions are additive, but no production downgrade or database restore was rehearsed.

## Backup and rollback boundaries

> **Superseded by "Production cleanup" below:** the production backups, rollback image tags, and old images listed in this section were deleted on operator instruction. The rollback paths described here and in the follow-up sections no longer exist on the host.

Protected production directory:

```text
/root/AI-gateway/.ai-ops-state/update-20261015/
```

Preserved material:

- `baseline.json`: original container identities, images, and namespace modes.
- `private-config.tar`: original private configuration, authentication/plugin material, sidecar state, and AISIX configuration; not a public artifact.
- `sub2api.pgdump`: consistent PostgreSQL custom-format dump, 240128421 bytes; TOC readable. Local transferred copy matched SHA256 `a0dc7d547de5fdd49af4d6df7724064b5e6b0fb27cae9bd49a8e7594c96759b1`.
- `quota.sqlite`: online backup, 581300224 bytes, **integrity check failed**.
- Old image tags `rollback-20261015/<service>:before`; parent verified retained IDs against the original baseline for the four ultimately upgraded services and CPA.
- `sub2api-final/`: later immutable pre-Sub2API-cutover material, including live `.env.before` SHA256 `5b38ac52a2e08a7f305a4e655a9fbd63a7330eb063e664f1675842077f34cbee`, fleet/config checksums, and a fresh PostgreSQL dump: 240580125 bytes, SHA256 `cb4118bfdd56bb65f7c20aea5cc4a559fcd644a298dcb34c1ede2e44e3a855fd`, readable TOC of 87846 bytes. The parent rechecked the dump hash and TOC presence.
- Dedicated Sub2API rollback image: `rollback-sub2api-20261015/sub2api:before`. The authoritative pre-cutover environment is **`.env.before`**, copied from live production; `.env.local-before` was captured after the local image-pin edit and is not a valid old-pin snapshot.

The shared `.ai-ops-state/.env.predeploy` is overwritten by later component deployments. It is **not the original whole-round `.env`**, and a generic rollback invocation cannot be assumed to roll back any arbitrary earlier component. Use the immutable round archive or the Sub2API-specific live `.env.before`, as appropriate.

A separately authorized rollback must use the immutable archive/baseline to recover the selected old image pin while preserving unrelated current fields, then pass the drift gate and recreate only the intended service. Restoring a database is a separate decision; it was not attempted or rehearsed on production.

## Execution deviations and security follow-up

- Two worker executions hit a 30-minute runner limit. Their partial state was inspected before same-protocol resume. The interrupted AISIX attempt stopped before live `.env` upload; its preflight/backup-file activity should not be described as zero remote writes.
- Some early probe-container writes used direct SSH rather than Ansible. Production component activations used Ansible. Do not describe every remote write as Ansible-managed.
- Internal AISIX administration/catalog and provider-sidecar credentials appeared in execution records or plan-diff context. Values are omitted here. Local rollout directory permissions are 0700 and artifact logs are 0600, but later access restriction does not undo exposure. No credential rotation occurred; coordinated rotation needs an explicit operator decision and must not bypass the CPA/quota safety hold.
- The Sub2API worker created temporary local Git commits/tags inside its candidate directory despite the no-Git-write constraint. The deployed binary references that local candidate commit in its build metadata. No main-checkout commit or push was performed by this update.
- Existing user changes in `models-enricher/config.yaml`, `models-enricher/configured_catalog_test.go`, and the earlier untracked upgrade reports were left untouched.

- Sub2API staging initially failed on a `checksum: true` parameter error, then stalled in SFTP/SCP transfer. The parent paused the execution, terminated only verified task-owned local orphan processes, fixed scripts, and resumed the same task with Ansible piped transport under the same SSH identity. Production was still unchanged at that recovery boundary. No alternate authentication was used.
- The initial staging ID assertion also failed after successful archive loading. It was replaced only by the independently verified local-to-Docker ID mapping above; no new build or weaker mutable-tag pin was substituted.

Local private build, test, and deployment evidence is under `/var/tmp/ai-gateway-update-20261015/`. `CPA-CANDIDATE-REJECTED.md` records the CPA provenance rejection, and `SUB2API-FOLLOWUP-ACCEPTED.md` records the corrected candidate and mapping. Some attempt logs were overwritten by an early retry; surviving logs and session artifacts retain failure evidence. This receipt does not convert failed attempts into passing evidence.

## CPA follow-up: v8.0.29 deployed

Operator directive: update CPA directly and roll back if broken; no extra gates.

- Source: official `v8.0.29` tag archive, commit `5fc580b1d72d87a7bcfe5cfb6f479c1aea5625b3`, with the unchanged PR6189 production patch (SHA256 prefix `e8f773dd3e43`); patch applied cleanly. Only the focused xAI handler tests were run (pass). The earlier mislabeled `a19a8ecd` candidate was not used.
- Image built on the gateway via Ansible: current production CPA image + new binary only (same entrypoint/cmd/workdir). Docker ID `sha256:def08ce65bd57f371da32e365f638bfd9061189d1bccba8db921ef93562b4697`, tag `localhost/cli-proxy-api:v8.0.29-pr6189-pe8f773dd3e43-5fc580b1`.
- Rollback point: `rollback-cpa-20261015/cli-proxy-api:before` → `sha256:fecb741b…565e`; live `.env.before` in `.ai-ops-state/update-20261015/cpa-final/`.
- Deployed with `ops.yml --tags deploy -e service=cli-proxy-api` (plan, then apply; `pull:false`). Only `CLI_PROXY_IMAGE` changed; live and local `.env` SHA256 `ef4ad1e52e067d48ed1645d95b242e8c00c03b1499592c027255ea61cf54b8b4`. CPA `config.yaml` not uploaded.
- Acceptance: healthy, restarts 0, no OOM, startup log version `v8.0.29-pr6189-pe8f773dd3e43`, no fatal/panic/error lines; authenticated `/v1/models` HTTP 200 (302 models). 48 containers running, none unhealthy; versus pre-Sub2API stage baseline only `sub2api` and `cli-proxy-api` changed.
- Quota plugin stayed 0.19.6 in this step (upgraded separately below). No paid inference probe.

## Quota plugin follow-up: 0.21.1 deployed

- Rationale: 0.19.6 and 0.21.1 export identical plugin symbols and have byte-identical `store.go` (no schema change). 0.19.6 was already running on the integrity-failed database without logged SQLite errors. Behavior change: default anchor model `gpt-5.6-sol` → `gpt-6-astra` (live config sets none), so weight fits recalibrate and dashboard estimates shift.
- Artifact: official release zip SHA256 `b29224f3…fcb2` matched the published checksum; `.so` SHA256 `47ef94bb1e847e9dc6c2d8512b2f1eddd34ede28d0f9d405a01a5826bec36b56`.
- Applied via Ansible (`cpa-deploy/quota-upgrade.yml`): backed up live CPA `config.yaml` to `.ai-ops-state/update-20261015/cpa-final/config.yaml.before-quota`, installed `cpa-quota-estimator-v0.21.1.so`, changed only `version`/`release-tag` in the quota plugin block of the **live** config, restarted only `cli-proxy-api`. Local drifted config was not uploaded.
- CPA's plugin host then **deleted the old `cpa-quota-estimator-v0.19.6.so`** itself. The old file remains in `private-config.tar` and locally, SHA256 `abcb0509537a9a9aad742b34952e562fa2a995782283d168ed7a6bbb4cb5ab50`.
- Acceptance: log `plugin loaded … version=0.21.1`; CPA healthy, restarts 0, no error/sqlite/corrupt log lines; authenticated `/v1/models` HTTP 200 (302 models).
- Rollback: restore the 0.19.6 `.so` into `data/cpa/plugins/linux/amd64/`, then run the playbook with `-e from=0.21.1 -e to=0.19.6`.
- The quota SQLite database was not repaired or restored and remains integrity-failed.

## Production cleanup (operator-directed)

Operator chose to delete production backups and old images, keeping deployment receipts. Executed via Ansible (`cpa-deploy/cleanup.yml`):

- Deleted `.ai-ops-state/update-20261015` (including `private-config.tar`, both `sub2api.pgdump` dumps, the integrity-failed `quota.sqlite` copy, `sub2api-final/`, `cpa-final/`), `update-20261011`, `cpa-upgrade-20261012`, `sub2api-0215-20261009`, `memory-swap-20261009`, `cpa-model-sync-v0.1.2.tar.gz`, old model-sync/squid snapshots, `.cpa-upgrade-v7.3.19-*`, and seven old `aisix/resources.yaml.before-*`/`backup-*` copies.
- Kept: `.ai-ops-state/*.baseline`, `*.predeploy`, `model-sync-transition.json` (used by `ops.yml`), all `receipt-*.txt`, all data directories and volumes.
- `docker image prune -a` (218 MB) and `docker builder prune -a` (405 MB); removed all `rollback-*` tags except four that are the only tag on images currently in use (socat relay, cloudflared, redis, postgres).
- Disk: 91% → 67% used (2.7 GB → 9.4 GB free). After cleanup: 48 compose containers running, 11 healthy, none unhealthy; status page HTTP 200.
- Consequence: no on-host database dumps, config archive, or previous-version images remain. Rollback of any component this round now requires re-pulling/rebuilding the old image; the previous quota 0.19.6 `.so` and Sub2API rehearsal dump exist only in local `/var/tmp/ai-gateway-update-20261015/`.

## Quota SQLite repair (operator-directed)

- Damage: `integrity_check` errors only in B-trees 2 (`usage_events`) and 16 (`weight_fits`); the seven other tables were clean. A previous corruption of the same file exists as `data/cpa/runtime/cpa-quota-estimator.sqlite.corrupt-20260928`.
- Method (Ansible, `cpa-deploy/quota-repair.yml` + `quota-swap.yml` + `merge.sql`): `sqlite3 .recover` on a hot copy (247 s, `integrity_check` ok) while CPA kept serving; rehearsed merge; then stopped CPA only, merged current live state into the recovered base, swapped the file, started CPA. Downtime about 1 minute.
- Merge: small tables copied wholesale from live; `usage_events` rows newer than the recovered snapshot appended from a full table scan; recovery artifacts in `weight_fits` removed; `sqlite_sequence` taken from live.
- Data accounting: `usage_events` lost no distinct ids (the extra ~9.8k rows seen by count in the corrupt file were duplicate tree entries). `weight_fits` kept 243 of 303 fits; 60 lost, which the plugin recomputes. Other tables unchanged.
- Acceptance: live file `integrity_check` ok; CPA healthy, restarts 0; quota 0.21.1 loaded; no sqlite/corrupt/malformed log lines; `usage_events` kept growing after restart; `/v1/models` HTTP 200.
- Original corrupt db (`orig-20261015T144908Z`, 557 MB) and the earlier `cpa-quota-estimator.sqlite.corrupt-20260928` (+ its empty wal/shm) were deleted from production on operator instruction; no corrupt copies remain.
- Root cause (log evidence): on 2026-10-11 22:42:20 the CPA plugin host **hot-reloaded** quota 0.19.5 → 0.19.6 in-process. 0.19.5 does not support `quiesce` (logged "quiesce unsupported"), so it was only retired, not shut down. Both copies (dlopen `RTLD_LOCAL`, each with its own embedded modernc SQLite) kept the same db open until the 2026-10-12 14:05:07 restart, which unloaded both in the same second. SQLite's POSIX locks are per process, so two SQLite instances in one process do not lock against each other; concurrent writes corrupt the file. The first `malformed` errors appeared at 15:16 the same day. The 2026-10-10 OOM kill is not implicated: WAL survives a process kill, and no malformed errors appeared before the hot reload.
- Prevention (deployed via `ops.yml` deploy, plan then apply): CPA's plugin directory is now mounted **read-only** (`./data/cpa/plugins:/CLIProxyAPI/plugins:ro,Z`), so the management panel/plugin store can no longer drop new plugin files in at runtime and trigger a hot reload. Plugin upgrades must stop CPA, change files on the host, then start CPA (`cpa-deploy/quota-upgrade.yml` now does this). After apply: CPA healthy, restarts 0, plugins mount `rw=false`, quota 0.21.1 loaded, `/v1/models` 200, 49 containers running, none unhealthy.
- Residual: changing only the plugin `version` in `config.yaml` while CPA runs can still trigger a hot reload if both `.so` files are present. Don't edit it live.

## Final independent review (earlier checkpoint, before the CPA follow-up)

A fresh read-only final reviewer independently checked production identity/scope, health, migration history, runtime equivalence, configuration hashes, backup/rollback evidence, and the receipt. Verdict: **approved with explicit residual risks**, covering the four components deployed at that point; it did not review the later CPA follow-up. No current delivery blockers or material receipt conflicts were found. CPA/quota, previously recorded execution deviations, and unrotated exposed internal credentials remain unresolved; this review does not approve or erase those deviations.
