# Production disk cleanup — 2026-10-29

## Outcome

Approved safe-category cleanup ran at 13:50:01–13:50:37 UTC without restarting services. Initial inventory at 13:44 UTC showed **96% used, 1.38 GiB available** on the approximately 30 GiB root filesystem. Final verification showed **91% used, 2.80 GiB available** (3,004,760,064 bytes).

Measured cleanup accounts for approximately **1.27 GiB**; available space increased by **1.42 GiB** during the full inventory/cleanup interval. Live writes, rotation and database checkpoints affect `df`, so its entire change is not attributed to cleanup. The immediate pre-cleanup reading was already 95% used.

| Category | Before → after | Reclaimed |
| --- | --- | ---: |
| Dangling images | No candidates; guarded `docker image prune -f` | 0 |
| Build cache | Docker accounting 126.5 MB → 95.85 MB; `docker builder prune -f` | 30.7 MB reported; 29.37 MiB physical |
| Unused prototype images | Three unreferenced pre-cutover TypeScript experiments removed; image count 54 → 51 | 31.02 MiB physical, shared layers retained |
| Archived journals | `journalctl --vacuum-size=200M`; journal directory 1,413.72 → 170.23 MiB | 1,243.48 MiB |
| APT download cache | 1.52 MiB → 0.07 MiB; `apt-get clean` | 1.45 MiB |
| Old health-diagnostic scratch | Two completed 2026-10-23 directories removed | 0.25 MiB |

The removed image tags were `ts-shadow-ba4a4721b93b`, `ts-shadow-c67fb9cee6c6` and `ts-shadow-9143d1a5b4b0`. None appeared in container references, deployed Compose/environment pins or repository pins. The historical released `ts-shadow-b4439caba423` image was retained. No broad image/system/volume prune was used.

## Top-15 directory inventory

Initial `du -xh --max-depth=3 /` ranking, excluding `/` itself. Sizes are rounded; ancestors include their children and must not be added together. `$HOME` denotes the deployment account's home; `$DEPLOY` denotes its checkout. Purpose/owner classifications are inferred from paths, not permission changes.

| Path | Before | After | Purpose / owner |
| --- | ---: | ---: | --- |
| `/var` | 11 GiB | 8.8 GiB | Runtime storage, logs and scratch; system |
| `$HOME` | 9.6 GiB | 9.6 GiB | Deployment account |
| `$DEPLOY` | 9.5 GiB | 9.5 GiB | Application deployment |
| `$DEPLOY/data` | 6.4 GiB | 6.4 GiB | Live databases and application state |
| `/var/lib` | 6.1 GiB | 6.1 GiB | Container/runtime state |
| `/var/lib/containerd` | 5.2 GiB | 5.1 GiB | Shared image content and snapshots |
| `/usr` | 3.4 GiB | 3.4 GiB | Installed system files |
| `$DEPLOY/.ai-ops-state` | 2.5 GiB | 2.5 GiB | Operational baselines and database snapshots |
| `/usr/lib` | 2.1 GiB | 2.1 GiB | System libraries, modules and firmware |
| `/var/tmp` | 2.0 GiB | 2.0 GiB | Operational scratch and retained rollback artifacts |
| `/var/log` | 2.0 GiB | 751 MiB | System logs |
| `/var/tmp/ai-aisix-window` | 1.8 GiB | 1.8 GiB | Current routing-window evidence and rollback snapshots |
| `/var/log/journal` | About 1.4 GiB | 170 MiB | System journal; archived entries vacuumed |
| `/usr/lib/firmware` | 732 MiB | 732 MiB | Installed firmware |
| `/var/lib/docker` | 687 MiB | 688 MiB | Docker metadata, container logs and writable state |

## Safety and verification

- Normalized `docker ps -a`/inspect snapshots matched for **all 50 containers**: IDs, image IDs, running/health states, start timestamps and restart counts were unchanged.
- Router-container local `GET /healthz` returned **HTTP 200** before and after cleanup. The probe used Bash `/dev/tcp`; `curl` is not installed in that image.
- All 51 non-selected images remained present, including AISIX **1.6.0**, all container/Compose/repository pins and explicit rollback tags. Uncertain historical and utility images were left alone.
- No volumes, databases, SQLite files, backups, plugins, CPA configuration or running containers were modified by cleanup. Services were not restarted and `/etc` was not changed.
- The current window's config archive and both fresh SQLite snapshots remained present. Older union/prune/Lua/keeper scratch was retained because it contains rollback/config evidence or backup references; current-day warm-worker and routing scratch was retained entirely.
- Container JSON logs were **not truncated**. They totaled approximately 234 MiB after cleanup: Sub2API 89.8 MiB and the egress proxy 80.7 MiB were largest. All 49 stack containers already had rotation limits (mostly `20m` × 5; a few `10m` × 3). One legacy host-access relay had no explicit limit, but its log was only 1,168 bytes.

## Remaining risks and follow-ups

- **91% remains high.** The largest application consumer is PostgreSQL (about 4.7 GiB), followed by CPA state and retained snapshots. No database maintenance was attempted.
- Operational snapshots occupy about 2.5 GiB under `.ai-ops-state`, another 617 MiB under `.rollback`, and most of the 1.8 GiB current-window scratch. Define a reviewed retention/off-host backup policy with restore verification before deleting or compressing any of them.
- Journal vacuum was one-time. Consider a persistent journal size/retention limit in a separate approved configuration change; no limit was installed here.
- Consider rotation for the legacy relay when it next has an approved recreation window. Existing stack rotation is bounded and needs no emergency truncation.

Private raw inventory, image keep/remove classifications and before/after evidence remain on the controller under `/var/tmp/ai-disk-cleanup/`. No credentials, addresses, hostnames or private configuration contents are included in this record.
