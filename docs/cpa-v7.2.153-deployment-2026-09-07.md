# CPA v7.2.153 deployment — 2026-09-07

## Result

**CPA was manually upgraded; the models-table data source now contains all nine registered Codex models.** Recovery was observed after the version change and process recreation. This does not establish whether a code change or clearing transient runtime state caused recovery. No production rollback comparison or inference replay was performed.

The separate correction to “bare model name” semantics remains planning-only. `force-model-prefix` is still `false`; neither Go filtering nor actual model-call rules were changed.

## Deployed artifact and scope

- Image: `docker.io/eceasy/cli-proxy-api:v7.2.153`.
- Actual Docker image ID: `sha256:5f660aad0c318a38b9b86442062ff247ce5ddb5198c7c5daec33878873cc8693`.
- Runtime version: v7.2.153; commit `934fb79`; build `2026-09-07T11:32:18Z`.
- Container: `100cae07759eb16a88132d34fd67b982b354ee917c6011de2c6d1d22ed65114f`.
- Started: `2026-09-07T12:51:09.336633227Z`.
- Only the production `.env` setting `CLI_PROXY_IMAGE` changed, from v7.2.152 to v7.2.153. Effective Compose differed only in that image.
- Only `cli-proxy-api` was recreated, using `docker compose up -d --no-deps --no-build --pull never cli-proxy-api`.
- The existing shared network namespace, 1GiB memory/swap limit, 1.75 CPU limit, 256 PID limit and read-only root filesystem were retained.
- Go, Rust, APISIX, Sub2API, egress, routing, credentials and CPA non-model settings were not changed. Rust's independently synchronized model arrays were excluded from the non-model comparison.

[The upstream release](https://github.com/router-for-me/CLIProxyAPI/releases/tag/v7.2.153) includes authentication cooldown and scheduler changes, but does not explicitly identify this Codex catalog symptom as a fixed issue.

## Checks

Before deployment, the exact new image was started without network access using a minimal fixture and the existing plugin binaries. Both plugins loaded and registered. The expected offline Antigravity refresh warning did not trigger a network-policy change. This fixture checked startup/plugin loading, not complete production behavior. Its container was removed after verification; its logs were retained.

Production Management checks confirmed both original plugins remained registered and effectively enabled:

- `model-router` 0.4.2
- `cpa-quota-estimator` 0.5.2

At the post-upgrade service snapshot, CPA was healthy with restart count zero. All 48 project services were running; the other 47 container identities and restart counts were unchanged. No new OOM events were observed against the fresh pre-upgrade baseline. Control-file and plugin-binary hashes matched; no budgets were increased.

### Codex catalog

| Boundary | Before | After |
| --- | --- | --- |
| Registered Codex models, prefix-qualified subset | 9 | 9 |
| CPA public directory, `client_version=v0.65.0` | 1 | 9 |
| models-table's same-version Go directory | Previously 1 | 9 |

The targeted page-data check at `2026-09-07T12:55:44.618816Z` returned HTTP 200, 247 total records and 11,233,051 bytes. Its nine Codex IDs exactly matched CPA's registered prefix-qualified set:

- `codex/codex-auto-review`
- `codex/gpt-5.3-codex-spark`
- `codex/gpt-5.5`
- `codex/gpt-5.6-luna`
- `codex/gpt-5.6-sol`
- `codex/gpt-5.6-terra`
- `codex/gpt-6-astra`
- `codex/gpt-image-1.5`
- `codex/gpt-image-2`

The request used the page's existing `/v1/models?client_version=v0.65.0` endpoint. Go was not restarted and its five-minute successful-input cache policy was not changed. This verifies the page's data source, not the state of an already-open browser tab or inference access to every listed model.

Response SHA-256: `535a219eb762108d1ecb1f7f104abd5ac2e6974bf8e87de9c4fe24cadcca3cf5`.

## Backup and closeout

Private production evidence and recovery files:

`/root/AI-gateway/.cpa-core-v7.2.153-20260907T123746Z/`

Local evidence:

`/root/.cache/cpa-core-v7.2.153-20260907T123746Z/`

Retained backups include the previous environment, Compose/override, CPA configuration, auth files, plugin files and independent SQLite online backups. `PRAGMA quick_check` passed for both:

- `model-router.db`: 204,316,672 bytes.
- `cpa-quota-estimator.sqlite`: 38,514,688 bytes.

These are independent SQLite snapshots, not a cross-database atomic snapshot. No backup was restored and no database was replaced. The previous image and backups remain available; only this task's deployment lock is released at closeout.

Evidence includes `before-state.json`, `after-state.json`, `table-codex-result.json`, startup logs and the isolated plugin result. Read-only verification helpers wrote these receipts; deployment itself used individual manual commands.
