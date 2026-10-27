# CPA plugin maintenance — 2026-10-27

## Scope and root cause

Updated `cpa-quota-estimator` 0.21.2 → 0.21.3 and installed `codex-auto-reset` 0.1.13. CPA remains on its existing custom v8.0.38 image; the official-image/catalog-membership change is planning-only and was not applied.

The plugin host copies shared objects into `/tmp/cliproxy-pluginhost` before loading them. Docker's previous tmpfs default was `noexec`, causing `failed to map segment`. Only CPA's `/tmp` now uses `rw,exec,nosuid,nodev,size=64m`; its root filesystem and plugin mount remain read-only. Both tracked and private Compose files received the same one-line change.

The new Ansible component transaction deliberately permits only image/environment changes; a tmpfs topology change fails that gate. This maintenance therefore used a reviewed manual structural-change window rather than weakening the gate. The new playbooks supplied production check verification and the separate config activations described in [the Ansible acceptance record](ansible-operations-production-2026-10-27.md). No dependency, network namespace, image, or resource limit changed.

## Artifacts

Release ZIP SHA-256 values matched GitHub's published asset digests. Controller `gh release download` and direct HTTPS attempts failed TLS; the same public release assets were downloaded with Python requests on the production host and independently hash-checked before extraction.

| Plugin | ZIP SHA-256 | Installed shared-object SHA-256 |
|---|---|---|
| quota 0.21.3 | `f526cad9f068202934a01b4dca0463ff0a9ea88b6667b2eb67d26027a9246639` | `76ae87037dd0bbd3d714a0381c25f988874e96fe5d1552f3dd17df7734bece5d` |
| auto-reset 0.1.13 | `63c10e5b820484e07b070442d93b9dd726e07c4afa688494313154043951adf1` | `16a1d91d8e466a44f1da7f68535cc68102d8f68a10c333ff32536b0a955fa137` |

Installed under `data/cpa/plugins/linux/amd64/` as `<plugin-id>-v<version>.so`. CPA source `internal/pluginhost/platform.go:pluginFileFromPath` and `config.go:pluginConfigDesiredVersion` select this filename/version convention and `store.version` (falling back to `store.release-tag`). Live store metadata now identifies versions 0.21.3/v0.21.3 and 0.1.13/v0.1.13, using official-registry identity metadata and the exact selected GitHub release versions.

## Backups and safe window

Private backup directory: `/root/AI-gateway/.ai-ops-state/cpa-plugins-20261027T182453Z/` (0700). It retains:

- `quota-before.sqlite`: consistent **online** backup using Python's SQLite `Connection.backup`, taken while CPA ran; `integrity_check=ok` before maintenance. No live WAL database was copied as an ordinary file.
- `quota-after.sqlite`: another online backup after successful startup; `integrity_check=ok`.
- `config-before.yaml`, fresh `config-window-before.yaml`, `compose-before.yaml`, `compose.override-before.yaml`, `env-before`, and the previous quota shared objects outside the load directory.
- Private startup/command logs, account-status capture and UTC timeline. Secret-bearing config/environment snapshots are 0600.

The config edit was limited by YAML-node offsets to the quota store version/release tag and a new auto-reset entry; parsing proved all other values unchanged. Auto-reset is enabled with priority 1, **no enabled accounts** and no redemption requests. No existing auto-reset state file was present.

| UTC | Event |
|---|---|
| 18:24:53 | Online backup started; backup integrity verified before the window |
| 18:41:11.867 | Maintenance began; stopped recovery timer and paused cpa-model-sync |
| 18:41:14.293 | CPA application stopped; cpa-netns remained running |
| 18:41:16.351 | CPA started on the same image with new tmpfs/config/plugins |
| 18:42:38.178 | Healthy, restarts=0; both plugins loaded |
| 18:42:38.382 | Model-sync unpaused and recovery timer active; window complete |

The reviewed Compose diff was exactly the CPA tmpfs line, validated with `compose config --quiet`. Activation used the tooling's `env -i PATH=… docker compose -p ai-gateway -f … --env-file …` style with `up -d --no-deps --no-build --pull never cli-proxy-api`. New binaries/config were placed only while CPA was stopped; all old quota binaries were removed from the load directory but preserved in backups. No hot reload occurred. Window duration was 86.5 seconds; stop-to-healthy was 83.9 seconds. Only CPA's application container ID changed; all 51 other running container IDs stayed unchanged.

## Verification

- Startup logged `pluginhost: plugin loaded` for **quota 0.21.3** and **auto-reset 0.1.13**. Zero `failed to map segment` or `plugin hot reloaded` events in the new container logs.
- Quota migration evidence: pre-backup metadata had only `missed_advanced_early_resets_v1=complete`; post-start online backup additionally had `missed_advanced_early_resets_v2=complete`. Version 0.21.3 emits **no migration-success log line**. Source `repair.go:repairMissedAdvancedEarlyResets` writes the v2 marker in the same SQL transaction as the repair and commits them together. `store.go:openStore` returns an error unless `store.migrate` succeeds; `app.go:app.configure` must complete `openStore` before initial configuration succeeds. The host's `Host.startPluginLoad` waits for registration before the load-result path. This committed marker plus successful plugin configuration and `integrity_check=ok` establishes migration completion without inventing a log message.
- CPA healthy, restarts=0. Observed memory 121.7 MiB shortly after startup and 181.6 MiB at subsequent verification, within unchanged 768 MiB limit. Root read-only; plugin bind mount `RW=false`; only `/tmp` has `exec`.
- Public Responses probes: `zcode/glm-5.3-flash` (CPA) and `glm-5.3-flash` (AISIX), minimal output budgets, both HTTP 200.
- Public `/v1/models`: HTTP 200, **367** models; Codex query: HTTP 200, **358** models. Counts match pre-capture. Trusted `127.0.0.1:9083/models-table`: HTTP 200.
- Auto-reset management status: **401 unauthenticated**, **200 authenticated**. Returned config had `enabled_accounts=[]`; reset history empty. Saved CPA config contains no enabled-account list. No reset-bank credit was redeemed.
- Recovery timer active and model-sync unpaused. Following `playbooks/deploy.yml -e component=cli-proxy-api --check --diff`: **changed=0 failed=0**. No baseline rewriting or stale-snapshot adoption was needed.
- Temporary normal-client verification key **id=32**, group 15: admin API DELETE returned 200; subsequent GET returned 404. Temporary `/var/tmp/ai-plugin-*.json` files (key, admin session and API captures) were removed; remaining count **0**. No key value or public hostname is recorded here.
- `validate.sh .env.example` passed with the local Compose override temporarily moved and restored by an EXIT trap. Compose commit `822a537` passed all Validate jobs (run **40558062889**). Full OpenSpec strict validation passed.

## Rollback

Stop the recovery timer, pause cpa-model-sync, then stop only `cli-proxy-api`. Restore `compose-before.yaml`, `config-window-before.yaml` and the backed-up old quota binaries; remove both newly installed versioned binaries from the load directory. Validate Compose and start only CPA with the same isolated argv and `--no-deps --no-build --pull never`. Prove health, original plugin selection, model endpoints and integrity, then unpause sync and restart the timer. Reconcile controller desired Compose with the restored host before planning another deploy.

Do **not** copy an old SQLite snapshot over a live database or newer requests. A database restore would need a separate stopped-writer recovery decision and explicit loss accounting. Backups remain protected on-host; restore was not rehearsed, no paid reset was tested, and long-term soak is outside this maintenance.
