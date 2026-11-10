# Sub2API error-only release and staged cleanup — 2026-11-09

## Result
The release from PR22 merge `0141f54b6ff01bcb300bfc28b04e25d1c34cce92` is active on Sub2API image `ghcr.io/xz-dev/sub2api@sha256:56557012dc75e96feb22e26fdb36fce057ab1e660cba8f907bf4b3d85472472a`, revision `02918ea957f0639db932b23e7e5d00a6f2298308`. The earlier disk-gate stop record remains historical, not the current disposition.

Final container started18:22:47Z and was observed ready18:22:54Z, under120seconds. The fresh guarded image-only preview and apply passed. Independent old/new Docker renders changed only the selected image; all47other container identities,48namespace inodes and204security fingerprints remained unchanged. Memory512MiB, combined memory+swap832MiB, actual swap allowance320MiB, CPU1.5 and GOMEMLIMIT288MiB were preserved. Final health was healthy, restart0, noOOM; authorized catalog returned200/352entries.

## Cleanup and retention
The parent separately approved six exact obsolete entries: four original unreferenced CPA/AISIX/bridge/keeper image IDs, the old unmounted bridge spool volume and one redundant remote CPA backup. Each was rechecked immediately before exact non-force removal. The volume had unchanged allowlisted entries and no container/open-file/kernel/fuser references. The remote gzip matched the preserved controller archive member by size, compressed SHA256, full gzip CRC, uncompressed size and SHA256.

Filesystem available space increased from3424722944 to4042661888bytes: **617938944bytes measured**, not a sum of image logical sizes; concurrent writes can affect attribution. No prune or broader deletion occurred. Current images, prior Sub2API rollback images/aliases, mounted volumes, keeper store/anonymous spools/ledger, controller CPA/keeper backups, current512MiB rollback files and receipts remain protected. Further backup deletion awaits the parent's independent final check and disposition.

## First attempt and final retry
The first guarded activation succeeded, but an extra raw HostConfig equality check treated bind-list ordering as drift. Capture-first image-only recovery ran; nested failure handling unnecessarily repeated recovery after the first recovery's raw-equality check failed. The original first-new inspect/log capture was overwritten by a later old-recovery capture. Its exact historical HostConfig is unknown.

A bounded read-only comparison proved only bind-list order differed in the retained old-recovery inspect; all mount contents/options, resources and security settings were equal. The parent approved one fresh retry with only the full Binds string list sorted, retaining duplicates/options, and every other HostConfig field compared raw. Full new inspect was retained before assertions. The final new inspect showed only Binds ordering differed; exact runtime mounts, namespace, resources, environment and security checks passed. Controller failure handling now uses unique capture directories and one outer recovery; six local mocked paths passed. No application-source or production guard was weakened.

## Observations and limits
Existing traffic18:22:54–18:27:54Z: six completed Chat200s, one embedding200, one front Responses WebSocket101; no logged post-readiness5xx or499 through final18:28:20Z. No rerank completion or provider-terminal Responses event was established. The broader transaction interval included three Chat502s at18:08:15Z,18:10:01Z and18:22:49Z, around activation/recovery/startup. Actual client interruption durations were not measured.

Final three ten-second samples passed unchanged245000000-byte admission with empty tickets and unchanged200MiB memory/1GiB disk floors and32MiB margin. Available memory was932286464/933978112/960655360bytes; disk3859091456/3858636800/3858182144bytes. These are instantaneous admission observations, not full capacity acceptance.

No paid/synthetic inference, repeated overcap or8MiB probes, auth/account/route mutation, task completion ticks or archive. Realistic production8MiB remains unobserved; corrected overcap behavior remains production-unretested. The writer stopped; the parent owns independent verification and remaining retention decisions.

## Validation
OpenSpec strict validation passed20/20; local controller mocked recovery tests passed6/6. No staged files, source/CI changes, task ticks or archive.

## Final operational confirmation and separately authorized cleanup — 18:49Z
The parent independently confirmed operational update acceptance, then explicitly authorized final cleanup. Three original controller backups totaling268158912 logical bytes were deleted after fresh exact device/inode/size/SHA256 verification and stable regular non-symlink0600 identity checks. Two protected67-byte existing-key COPY files were removed; both paths absent. No existing API-key resource/source was revoked, rotated or changed. The obsolete prior Sub2API340273 image was removed by exact non-force Docker image rm after rechecking zero references across all running/stopped containers and rollout-only aliases/revision569aeca2c4. No force/prune/broad-directory cleanup occurred.
Final cleanup production filesystem available3870560256→4047486976B: **+176926720B measured**, separately from the earlier six-entry617938944B gain. Controller filesystem available293525258240→293791858688B: **+266600448B after one bounded filesystem sync** (immediate unsynced delta0). These are filesystem measurements, not logical image-size sums; concurrent writes/accounting/evidence writes affect attribution. The three controller backup bytes are not production reclaimed bytes.
Fresh pre/post read-only checks passed. Final18:49:52Z observation:48running/configured-health healthy,47unrelated identities/48namespace inodes/topology/bindings/204security fingerprints unchanged. Active565570/OCI02918ea and512MiB memory/832MiB combined/actualswap320MiB/CPU1.5/GOMEMLIMIT288MiB unchanged; noOOM/restart. Hard1GiB disk/200MiB memory floors+32MiB margin held, disk4047433728B/MemAvailable1083236352B,0tickets/idleledger.lock. Post-readiness front/Sub2API5xx0; Sub2API76chat200+59embedding200+2rerank200+24Responses200. No new inference or HTTP health probes. Runtime mount array-order variation is not configuration drift: unique destinations and all fields matched; only HostConfig.Binds full strings were sorted, every other field compared raw.
Old77f4715 and both unrelated20261102 rollback aliases remain, along with all other images/backups/volumes/currentDB/cache/hiddenowner/spools/ledger/tickets/auth/config/current512rollback snapshots and small receipts. Original failed logs/records retained; no replacement backup, restart or source/config change. Raw protected fixtures may contain credential copies; broader secret search/deletion was not authorized. Original user checkout untouched.
PR21 and PR22 are merged/deployed; operational acceptancePASS is not full capacity acceptance. Earlier2/3 instantaneous245MB admission refusals remain disclosed: admission is best-effort, not always granted. Real8MiB success remains unobserved and corrected413 production-unretested. No #46 completion, task ticks or archive. Writer stopped without observation/wait loops. Private exact hash/timestamp/byte-count receipts and concise user closeout retained on the controller.

## Supersession — 2026-11-09

This is a historical receipt, not the final deployed image or current admission
guarantee. Preserve its failed/stopped attempts and original evidence limits.
The [final deployment, cleanup and three residual capacity checks](maintenance-capacity-closeout-2026-11-09.md)
supersede its current-state/pending-deployment statements. No earlier unverified
rollout task is completed retroactively by this reconciliation.
