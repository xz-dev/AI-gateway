# Lua catalog membership production deployment — 2026-10-28

## Result

The reviewed coupled model-catalog window completed at 13:10:21 UTC. The standard and Codex catalog bodies were byte-identical before and after the successful attempt. The first attempt was rolled back on the strict Codex comparison; captured native availability and deterministic image A/B tests explained that difference before the supervisor approved a retry. The retry passed the original strict gate without an exception. This record does not archive the change.

Only model-catalog was recreated. No routing, CPA account, quota, plugin, AISIX resource or public-list ordering change was made.

## Image and profile identities

| Item | Value |
| --- | --- |
| Previous image | `ghcr.io/xz-dev/ai-gateway-model-catalog:v1.2.0-adfc8ee@sha256:07167e38627fc9e8c4681f8feb2906dceff1e03f6cac65f79e111cb51e9df18a` |
| Deployed image | `ghcr.io/xz-dev/ai-gateway-model-catalog:v1.3.0-4b2ff32@sha256:60b32f94f17f8ab420a2a6fc056585e3bebb19c7930bf364f84e9be6ca926ee0` |
| Old config SHA-256 | `601ff0d5f1a83050e76d72d111e4eecd98ef2d1316130260da23b7d56d02cfb9` |
| New config SHA-256 | `84e9159384c2377ef64eca3fffaeafe822699a80c13a09e79b716ae42c857b12` |
| Private script SHA-256 | `fa9a93cbe04726d15dabfe6224f4aac5de68a0546e2f9c2634ddd7dbbf7354db` |

The image was pulled by digest. Local Podman and host throwaway Docker containers both validated the candidate pair with `--rm --network none --read-only`; they used UID 0 only to read the protected scratch mount. The deployed service retains its existing unprivileged runtime user. In-container validation returned the same config and script digests.

The host backup is `.ai-ops-state/lua-catalog-20261028T1300/`: `compose.yaml`, `.env`, `config.yaml`, and any pre-existing `catalog.lua`. The backup was taken before either attempt and was retained. The uploaded Compose differs only in the model-catalog image default, `CATALOG_SCRIPT_PATH`, and the script mount. The environment file differs only in `MODEL_CATALOG_IMAGE`.

## Private profile migration

`membership.cpa_include: ["devin/*"]` became `EXTRA_CPA = { "devin/*" }`. The private config no longer contains `membership` or `metadata.include_models`. The script rejects the following live hides with `hidden by profile`:

| Previous hide | Matching live candidates | Action |
| --- | ---: | --- |
| `!codex/gpt-image-1.5` | 1 | Keep in script |
| `!codex/gpt-image-2` | 1 | Keep in script |
| `!codex/gpt-image-2.5` | 1 | Keep in script |
| `!codex/gpt-image-2.5-flare` | 1 | Keep in script |
| `!xl/gpt-image-2.5-flare` | 1 | Keep in script |
| `!codex/gpt-image-2.5-sunburst` | 1 | Keep in script |
| `!xl/gpt-image-2.5-sunburst` | 1 | Keep in script |
| `!supergrok/grok-imagine-image` | 1 | Keep in script |
| `!supergrok/grok-imagine-image-quality` | 1 | Keep in script |
| `!supergrok/grok-imagine-image-2.0` | 1 | Keep in script |
| `!xl/grok-imagine-image-2.0` | 0 | Drop dead hide after AISIX pruning |

The real `compileModelPattern` matcher reported zero conflicts with static slugs, `inherit` references or `metadata_from` targets. A scratch harness using the actual `AuthorityOwner` and membership worker, captured native manifest, management channel/registration responses, AISIX IDs and private profile reproduced all 344 live members in prefix mode. No private profile or capture was committed.

## First attempt and rollback

| Event | UTC |
| --- | --- |
| Window start | 13:00:25.739 |
| Recreate complete | 13:00:29.674 |
| Healthy, restarts 0 | 13:01:36.785 |
| Recovery timer restored | 13:01:51.947 |
| Standard body `cmp` passed | 13:02:13.508 |
| Codex `cmp` failed; rollback started | 13:02:13.532 |
| Coupled rollback complete | 13:02:16.286 |

The fresh window pre-capture, unlike the earlier preflight capture, lacked native `codex/gpt-6.1-sol`. Native rows changed 555→556 and computed passthrough changed 46→47. AISIX stayed at 76; catalog membership stayed at 344. The only Codex row difference was `codex/gpt-6.1-sol`: its minimal compiled row became a full native passthrough row after native recovery. This was the only added native ID; no AISIX ID changed.

The worker restored `compose.yaml`, `.env` and `config.yaml` together and recreated model-catalog on v1.2.0. Readback confirmed the old config hash, old image, healthy state, restarts 0 and active recovery timer. Temporary key 37 was deleted: DELETE 200, subsequent GET 404, key file absent.

### Deterministic availability proof

After read-only diagnosis was authorized, three raw native samples approximately 30 seconds apart each contained 556 rows and the recovered model (55 fields). An off-path localhost stub supplied identical captured native, registration, channel and AISIX responses to throwaway containers of both exact images. The old container used the old private profile; the new container used the migrated profile and script. Mounts were read-only and containers had independent ephemeral caches. Metadata API-call responses were fixed 404 for both; no live upstream reads occurred. This isolates image/profile behavior, rather than claiming to replay all production metadata sources.

Both images produced byte-identical standard and Codex bodies for each of two identical-input cases:

| Native availability snapshot | Standard SHA-256, both images | Codex SHA-256, both images |
| --- | --- | --- |
| Target absent | `8409f5b2302f9f86c7a3117aeb9414d7811a9192c032a34340a2dd63213e60c6` | `c2610eb7f19e3b5f80485b6e305ebd3a8b561224867d7517f49c0346f8b23eea` |
| Target present | `8409f5b2302f9f86c7a3117aeb9414d7811a9192c032a34340a2dd63213e60c6` | `1af306efba80a72213c9db19773fce3727d2739f1a6ce2a72b9c31c82ccb9a7e` |

The supervisor approved one retry with the strict comparison first. A recurrence exception was permitted only for individually proved native availability changes. The successful retry did not need that exception.

## Successful retry

The model was present in a fresh native pre-capture before retrying.

| Event | UTC |
| --- | --- |
| Window start | 13:09:24.255 |
| One recreate complete | 13:09:27.640 |
| Healthy, restarts 0 | 13:09:40.947 |
| Recovery timer restored after forced refresh | 13:09:57.790 |
| Post-capture complete | 13:10:20.080 |
| Standard body `cmp` passed | 13:10:20.496 |
| Codex body `cmp` passed | 13:10:20.520 |
| All comparison gates passed | 13:10:21.170 |

Docker inspection of all containers before/after showed exactly one changed ID: model-catalog. The final ID still matched after baseline adoption. The service logged `membership script loaded` with the private script digest, and no WARN appeared through the final log readback. In-container validation succeeded. The recovery timer was active.

| Capture | Pre | Post |
| --- | ---: | ---: |
| CPA native IDs | 556 | 556 |
| AISIX IDs | 76 | 76 |
| Standard catalog / member IDs | 344 | 344 |
| Internal Codex rows, client version `0.101.0` | 344 | 344 |
| Public standard rows | 344 | 344 |
| Public Codex rows, client version `0.101.0` | 335 | 335 |
| Sub2API standard rows | 344 | 344 |
| Native passthrough IDs | 47 | 47 |

Native and AISIX ID sets were unchanged. Member and passthrough sets were equal. All five catalog body captures were byte-identical, not only the two required internal bodies:

| Body | Bytes | SHA-256, pre and post |
| --- | ---: | --- |
| Internal standard | 18,070 | `8409f5b2302f9f86c7a3117aeb9414d7811a9192c032a34340a2dd63213e60c6` |
| Internal Codex | 2,876,162 | `e361fdeecdb76671cfeaed5c394e507b922082cab5f5de40e53badc568f3435d` |
| Public standard | 29,078 | `3de725e1edb72834fdf53564dfbbc68d70b866165157d16b5f6a734fbbbaddb1` |
| Public Codex | 2,871,805 | `58d52042201725397a5e4625ba5c4906541c2f066cba4687ec7dcf7c244261d3` |
| Sub2API standard | 29,078 | `3de725e1edb72834fdf53564dfbbc68d70b866165157d16b5f6a734fbbbaddb1` |

The table used its pinned `v0.65.0` view. Pre: 354 rows, kept 344, hidden 10. Post: 564 rows, kept 344, include-hidden 0, script-rejected 220. Row-level comparison found only 210 newly displayed default-script rejects and the 10 migrated hide reasons. No row disappeared and no kept row changed. Generation and capture timestamps advanced normally.

## Baselines, resources and cleanup

Both `deploy-config.yml -e component=model-catalog --check --diff` and `deploy.yml -e component=model-catalog --check --diff` initially reported changed 0, failed 0 after local private Compose/environment readback adoption. Config apply then wrote only the two managed-file baselines (changed 2); it did not upload a profile or recreate a container. Rerunning both checks reported changed 0, failed 0. The component check needed no transaction because desired and live Compose/environment/image matched.

| Resource evidence | Value |
| --- | --- |
| Production cgroup peak after retry | 241,070,080 bytes (229.90 MiB) |
| Existing cgroup limit | 402,653,184 bytes (384 MiB) |
| cgroup OOM / OOM kill events | 0 / 0 |
| Settled process RSS at 13:25 UTC | 9,948 KiB |
| Settled Docker working-set display at 13:25 UTC | 5.312 MiB / 384 MiB |
| Same-time cgroup current, including cache | 120,778,752 bytes |
| Service health / restarts | healthy / 0 |

Docker working set, process RSS and cgroup current have different accounting; they are not interchangeable peak measurements. The retry was monitored with 36 cgroup/RSS samples, and `memory.peak` supplies the peak independent of sample cadence.

Task 1.6's separate local overlap test remains: 50 forced refreshes on the production-shaped fixture, every round overlapping a build and a collection (median overlap 98 ms); peak process RSS 216.5 MiB, last ten settled RSS 159.1–176.9 MiB, slope −1.4 MiB per round. Production observations above do not claim to repeat that synthetic stress test.

Temporary key 38 was deleted after validation: DELETE 200, subsequent GET 404, scratch key file absent. No temporary key remains from either attempt. Private captures, backup copies and commands remain under `/var/tmp/ai-lua-window/` on the controller and host; they contain private data and are not tracked.

## Rollback commands

Run in the deployment root on the production host. This restores the coupled pre-window files; a remaining script is ignored by v1.2.0.

```sh
set -eu
backup=.ai-ops-state/lua-catalog-20261028T1300
timeout 30 systemctl stop ai-gateway-recovery.timer
cp -a "$backup/compose.yaml" compose.yaml
cp -a "$backup/.env" .env
cp -a "$backup/config.yaml" services/routing/model-catalog/config.yaml
if test -f "$backup/catalog.lua"; then
  cp -a "$backup/catalog.lua" services/routing/model-catalog/catalog.lua
fi
timeout 30 docker compose config -q
timeout 180 docker compose up -d --no-deps --no-build model-catalog
timeout 30 systemctl start ai-gateway-recovery.timer
timeout 30 docker inspect ai-gateway-model-catalog-1 \
  --format '{{.Config.Image}} {{.State.Health.Status}} {{.RestartCount}}'
```

Wait for healthy, verify the old image/config, and repeat catalog checks. Re-adopt the restored private Compose/environment/profile before later managed operations. The v1.2.0 image does not support `validate-config --script`; do not use the new script validation command against it.

## Verification scope

The successful window passed the original strict parity requirement; there is no accepted catalog delta. No production inference, plugin reload, broad component deployment or archive was needed. Upstream native availability can still change independently; the first rollback and the identical-input A/B demonstrate why that source must be captured with future byte comparisons. Commit/CI identities are recorded in the change task evidence after the corresponding runs complete.
