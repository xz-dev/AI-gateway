# Go read-cache and catalog routing deployment — 2026-09-07

This is the earlier Go/APISIX rollout snapshot. A [later CPA-only v7.2.153 upgrade](cpa-v7.2.153-deployment-2026-09-07.md) restored all nine registered Codex models to the public and page-data catalogs; the missing-parent observations below describe the earlier capture, not the later Codex state.

## Result

**Deployed and verified in production.** Individual manual commands were used; no deployment or rollback orchestration script was run. The read-only verification helpers are retained separately.

Only `models-enricher` and `apisix-models` were recreated. All 48 project services remained running at final capture; other container identities, image identities and restart counts were unchanged. No new cgroup OOM events were observed. Resource limits were not increased.

## Exact deployed scope

- Go image: `localhost/models-enricher:readcache-20260907T102111Z`.
- Running Docker image ID: `sha256:78dd3a55d436e2efa1c9a04ae5b560e1e81da000e898e39662f299d1546d4e1d`.
- Executable SHA-256: `40f8344ff2cc91e46787d26f715abed95ff95431b4c74e134a8ecf5b6800410a`.
- Mounted Go configuration SHA-256: `e0bfd4d5dc14a7e8d67ec0a8735e9358ab4d5fc7718bfc257572885113ce45c4`.
- Go container: `df3b933282503d1387e1a3598c808db2473ac8ecfd05170a957efdad766f4d37`, started `2026-09-07T10:38:05.425377091Z`, restart count zero.
- Catalog APISIX container: `a8c87c6273dee938ed54002e7ec3ee5938fd0a8fa34fda74bce60b8f26c8b074`, started `2026-09-07T10:45:15.047046879Z`, healthy, restart count zero.
- APISIX retained its existing image, `docker.io/apache/apisix:3.18.0-debian`, runtime image ID `sha256:84e6b5e787e9f889ebff88161cb9a16599bafcffa236c6b54c7f779a0655940d`.

The candidate was derived from freshly captured production files, not copied wholesale from the dirty working tree. The unrelated local `alias-gpt-5-6-sol` route difference was explicitly excluded. Effective Compose comparison allowed only the Go image and 64MiB `/tmp` tmpfs addition. Credentials/environment values, overrides, inference routes and unrelated service definitions were preserved.

Go now caches successful upstream read responses, not final catalogs. Its actual `/tmp` mount is `rw,noexec,nosuid,nodev,size=64m,mode=1777`, within the unchanged 256MiB budget. Final inspection found 36 cache files totaling 21,179,550 bytes, with directory mode 0700 and file mode 0600.

The current channel configuration has no hidden pools or per-model overrides. Go has no `skip_channels` setting: absent channels retain CPA baseline; explicit `{}` entries fetch their own inventory without external metadata. GMI is an ordinary `{}` entry. Axis disables only its own inventory and uses its explicit subscription source. The approved source chains and five static IDs were installed unchanged.

APISIX's actual rendered configuration matched the reviewed production-derived template. The `codex-models` route requires nonempty `arg_client_version` and has no `proxy-cache`; the old 120-second nginx patch is absent. `apisix test` passed after recreation. A normal Compose `up` initially reported the existing APISIX container as running because bind-file contents do not change the container specification; an explicit target-only `--force-recreate` loaded the new mounts and startup configuration.

## Production checks

Using `client_version=v0.65.0`:

| Check | Result |
| --- | --- |
| Complete catalog | HTTP 200; 237 records; 10,643,181 bytes in the final captured pair |
| Same-version Sub2API authorization list | 124 model identities |
| Front catalog | HTTP 200; 64 records, exactly the authorized intersection |
| Record fidelity | Front records equal the selected full records, using arbitrary-precision integers and Decimal parsing |
| Conditional request | HTTP 304, empty body |
| Anonymous / deliberately invalid credential | HTTP 404, empty body |
| Missing / empty version at catalog APISIX | HTTP 200, matching direct CPA with the same query |
| Missing / empty version at front | HTTP 200, matching the corresponding Sub2API model identities |
| Final catalog cache | No HIT/STALE observed; rendered route has no final-cache plugin |

The private user credential came from the approved local Pi provider entry and was sent over stdin. No alternative user identity was tried and no inference request was replayed.

Two early verifier assertions were incorrect, not production failures:

1. CPA returns OpenAI `data` for an absent version, but Codex `models` for an explicitly empty version. Passthrough was checked against direct CPA with the same query instead of assuming one response shape.
2. Sub2API's plain request returned 15 identities, which is not the Codex authorization view. The real front module forwards the same query to its authorization leg; the matching versioned response contained 124 identities. Its intersection with the full catalog is exactly the observed 64 records, with no unauthorized or extra records.

Failed verifier logs and their captures were retained. Production routing was not changed to accommodate those assertions.

## Data gaps and limits

- GMI's actual inventory request returned 403 and the channel fallback branch ran. The captured full catalog had **zero `gmicloud/`-prefixed records**. Successful restoration or enrichment of GMI metadata was therefore **not** demonstrated. No credentials, egress permissions or inferred model ownership were changed.
- Exactly five bare static IDs were present. Only `codex/gpt-5.6-sol` was present among their designated parents. `codex/gpt-5.6-terra`, `codex/gpt-5.6-luna`, `codex/gpt-6-astra` and `supergrok/grok-4.7` remained absent. Their configured references were retained without substitution or fabricated metadata.
- CPA non-model configuration and Rust policy were unchanged. Core images remained CPA v7.2.152 and Sub2API 0.2.1; Rust remained v0.1.1. Sub2API's pre-existing restart count remained three.
- At final capture, Go used 115.50MiB and the new catalog APISIX used 64.20MiB. Their new-container lifetime peaks were 226.26MiB/256MiB and 83.78MiB/128MiB respectively. These are observations, not sustained-load or maximum-concurrency guarantees. No unrelated pressure test or budget increase was performed.

## Recovery evidence

Remote private backup and receipts:

`/root/AI-gateway/.go-catalog-manual-20260907T102111Z/`

Local evidence:

`/root/.cache/go-catalog-deploy-20260907T102111Z/`

Retained artifacts include `before/`, `candidate/`, `config-receipt.json`, `image-receipt.json`, the image archive, container snapshots, rendered runtime configuration, `catalog-result.json`, `runtime-result.json`, verifier logs and private response captures. Old images and fresh backups were preserved. Only the task's empty deployment lock is released at closeout; no backup cleanup or historical restore was performed.

The earlier [local verification report](enricher-read-cache-verification.md) remains historical fixture evidence; its resource run does not independently certify this later production configuration.
