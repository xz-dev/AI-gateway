# AISIX-first priority routing — deployed, 2026-10-29

## Outcome

**Deployed in attempt 4 with the official AISIX 1.8.0-rc.1 schema-memory fix.** The outcome record and task evidence were published in `7c16201`; Validate run 40867850213 succeeded. After supervisor review, the delta specs were synced and the change was [archived](../../openspec/changes/archive/2026-10-29-aisix-first-routing-fallback/tasks.md); post-archive strict validation passed. Attempts 1–3 remain below as history, including the real attempt-3 OOM. The user authorized realistic approximately 8 MiB history validation instead of another destructive synthetic maximum-size probe, while retaining the 64 MiB admission contract.

Production now uses the constant apisix-models pipe, AISIX's single `*` fallback to CPA, 512 MiB without swap, no direct router-to-CPA network path, the updated recovery script and the all-protocol CPA retention filter. The obsolete host/CPA relays and four networks are removed. The earlier 120-second AISIX stream timeout and model-catalog v1.4.0 remain unchanged.

The user approved immediate execution, the all-protocol retention filter and a CPA namespace recreation rather than a live network disconnect. One operator owned CPA throughout. The last-30-minute access-log sample contained 700 requests including 166 healthchecks (23.3/minute), below the approved 30/minute postponement threshold.

## Attempt 4 activation

Repository pin/planning/docs commit `6caf82b` passed foreground `validate.sh`, OpenSpec strict validation and CI Validate 40854049870 before service writes. The isolated worktree had no override; the shared worktree's override was untouched. Its submodules were initialized before validation. The exact official tag/index digest was pre-pulled on the host:

`ghcr.io/api7/aisix:1.8.0-rc.1@sha256:ebcd2bbba9b7a3cc7cfb1318e4d3ba64cf9f93cbf2b19b8486ead6093da1ba5a`

The amd64 manifest is `sha256:080a77bbd74cf89671cf0da72cc8485ef726d44f64dd5637a7d0626d93dcf531`. The private environment retains the former 1.6.0 pin as a `# rollback:` comment. No body cap changed.

| UTC | Action / result |
|---|---|
| 10:02:48–10:32:48 | Preserve the actual pre-window router and AISIX access logs: 442 router requests, 14.7/minute, below the approved postponement threshold. |
| 10:33:30 | Window starts. Recovery timer stopped, model sync paused, fresh file/online SQLite backups and baseline captured; temporary key 44 created. |
| 10:36–10:37 | CPA filter rewritten in place; Responses and Chat stream/non-stream checks plus non-Kimi check succeed. The Kimi Chat probe needs `reasoning_effort: low`: upstream rejects its default `medium`. This is a probe parameter finding, not a rollback trigger. Plugin subtree unchanged; no hot reload. |
| 10:38–10:43 | C1, new private image pin and `*` resources activated after check/diff. Correct image at 512 MiB/no swap, restart count 0, 76 listed IDs and no `*`; direct CPA-only bare request 200. |
| 10:45:04 | C2 active: slim router healthy, CPA addresses blocked, known and CPA-only bare/slash inference 200. |
| 10:47:14 | C3 active: CPA namespace recreated and healthy; both existing plugins load normally. |
| 10:52:13 | Updated recovery script active; manual service run succeeds, timer enabled, sync unpaused. |
| 10:52:16–10:52:46 | One realistic public history request: 8,389,995 bytes, 376 source-reading tool-call pairs, 754 input items built from real public Rust source. Transport completes with a structured provider `400 context_too_large`, not a 413. AISIX peak 131,010,560 bytes (~125 MiB), baseline 69,689,344 bytes; zero OOM/restarts. No successful large-context generation is claimed. |
| 10:53–10:56:52 | All 53 slash IDs exercised; all 53 exact-row counters increase. Account-specific failures are recorded below. |
| 10:57–11:01:16 | Public known/bare/slash requests, image generation/edit, SSE and keeper continuation pass. Catalog/manifest captures and network/health checks pass. Key 44 DELETE 200 / GET 404 / file absent. Active changes finish. |

### Attempt 4 functional results

| Check | Result |
|---|---|
| Known public Responses / Chat | 200. |
| CPA-only bare / slash | Public `claude-sonnet-5` and `xl/gemini-3.5-flash-lite`: 200. |
| All 53 slash IDs | 23×200, 18×502, nine×400, two×429, one client timeout. All exact-row counters increase; the background `*` counter also increases, not attributable to those exact-row probes. Unavailable accounts, image IDs tested on the Responses endpoint and intentionally excluded axis IDs were not repaired. |
| Public Responses SSE | 200, 47 events; first event 1.554 s, completion 2.278 s. |
| Public Chat SSE | 200, 42 events; first event 1.814 s, completion 2.426 s. |
| Keeper continuation after target-affinity upgrade | Both turns 200; keeper-owned parent and correct marker recall. No forced failover was injected. |
| Unknown everywhere | Both public protocols: 400 `upstream_error`, code `model_not_found`. |
| Public images | `gpt-image-2` generation 200 (12.65 s), Grok generation 200 (6.97 s), multipart `gpt-image-2` edit 200 (13.99 s). |
| Realistic 8 MiB history | Accepted through the public chain; provider context-limit error returned normally; no 413/OOM/restart. Peak ~125 MiB. No synthetic 64 MiB production probe. |
| Standard/Codex catalogs | Ordered canonical IDs unchanged: internal 344, public standard 344, public Codex 335. Standard/public/Sub2API bodies are byte-identical; seven Codex metadata rows change with source/availability churn. Native inventory 555→545 and passthrough 47→45 are recorded deviations, not migration failures. |
| Codex through Sub2API | 200, 335 models; account-base manifest through the retained router: 200, 344 models. |
| Health and isolation | Local router `/healthz` 200, no upstream health requests; CPA endpoints blocked, AISIX/catalog reachable; four obsolete networks absent. |
| Recovery / protected containers | Timer active and service succeeds. Five intended containers recreated, three obsolete ones removed; all 45 protected identities unchanged; restart counts 0. |
| Final component previews | AISIX and CPA both `config_diff: false`, changed=0. |

The seven varying metadata rows are the AIHub 5.6 Sol/Terra rows, CommandCode GLM-5.2/5.3 and DeepSeek-v4.1-flash, Cline DeepSeek-v4.1-flash and Kimi-coding K3. A fresh CPA rich-manifest read was retained. Passthrough drops the Codex Astra and Kimi-coding K3 eligibility entries during account probing; a later auth-status read reports active Codex/Kimi credentials. No credentials or model registrations were changed. This variability does not reduce the catalog's canonical membership/order.

Attempt-4 body hashes (canonical membership/order is unchanged despite Codex metadata differences):

| Body | Before SHA-256 | After SHA-256 |
|---|---|---|
| Internal standard | `8409f5b2302f9f86c7a3117aeb9414d7811a9192c032a34340a2dd63213e60c6` | same |
| Internal Codex | `96bfa49429e7834398cbd6926432305728c85c3a19b31dac4ec9dc279527b462` | `25b8332044cb4795c2ca2c8abd5c625105c948c79f7c71d5e3f3445c8ea9e0dd` |
| Public standard / Sub2API standard | `3de725e1edb72834fdf53564dfbbc68d70b866165157d16b5f6a734fbbbaddb1` | same |
| Public Codex | `b62027741f1800728c20d16de59ce1f774ed2cefb5e83039b4cc69ef6690a82e` | `f025a2ba36842954f0d68a62de0dc05c8bb11efdae29b926eb6cae0f4fb37493` |

AISIX 1.8 access-log parsing uses `status_code`, `downstream_latency_ms` and `requested_model`; pre-period 1.6 parsing uses the former names. The normal read-only deployment warning that automatic heap dumps cannot write their directory is recorded; no permissions or extra mounts were added.

### Attempt 4 closing live-traffic comparison

Both periods are approximately 30 minutes; the pre-period logs were saved before container recreation. Parsing handles the old and new access-log field names. The observation contains ordinary traffic, not the deliberately failing model sweep.

| Measure | Before: 10:02:48–10:32:48 UTC | After: 11:01:53–11:31:54 UTC |
|---|---|---|
| Router HTTP 2xx / 4xx / 5xx | 438 / 1 / 3 | 505 / 0 / 0 |
| Router excluding local healthchecks | 283 / 1 / 3 | 347 / 0 / 0 |
| Router 413 | 0 | 0 |
| AISIX 2xx / 4xx / 5xx | 74 / 1 / 3 | 351 / 0 / 0 |
| AISIX 5xx per requested model | `gpt-6-astra`: 3 | None |
| AISIX timeouts | 0 | 0 |
| AISIX memory.current | Pre-snapshot 138,289,152 bytes | 60,092,416–117,387,264 bytes, 31 samples |
| AISIX memory.peak / limit | 192,155,648 / 201,326,592 bytes | 131,010,560 / 536,870,912 bytes |
| AISIX swap | Pre-snapshot 67,039,232 bytes | 0 |
| OOM / OOM-kill / AISIX restarts | No baseline OOM | 0 / 0 / 0 |
| CPA | Healthy baseline | Healthy in every sample, restarts 0 |
| Recovery | Active before the window | Active throughout, 41 successful runs |

The higher AISIX request count is expected: CPA-only traffic now traverses it instead of bypassing it. A later warm heap sample was 37,641,800 bytes allocated and 48,541,696 bytes resident. No real operational regression or rollback occurred in attempt 4.

**Window:** 10:33:30–11:32:34 UTC, with active changes finished at 11:01:16 and observation finished at 11:31:54. Final backup/environment/plugin audit finished at 11:34:28. All fresh backups hash-verify; only `AISIX_IMAGE` changed among parsed environment values; plugin subtree unchanged. The eight scoped Sub2API account rows equal the earlier captured baseline exactly. No credentials were lost or rewritten.

### Root filesystem preflight and retained backups

Before attempt 4, the root filesystem had only 587 MiB available (98% used). The supervisor authorized compression of **only our four earlier-window SQLite backup files**, one at a time. Each gzip's decompressed SHA-256 matched its original before that uncompressed copy was deleted; gzip files and a hash manifest remain. No live database or unrelated files were removed. Availability rose to 2,751,152,128 bytes (~2.6 GiB), above the 1.5 GiB pre-backup requirement. Two fresh online SQLite backups pass integrity/hash checks. Availability afterwards is about 1.5 GiB (95% used), a separate capacity risk.

Read-only `du -x -B1 -d2 /` top 15 before compression (nested rows overlap; do not sum them):

| Path | Bytes |
|---|---:|
| `/` | 29,539,991,552 |
| `/var` | 11,711,578,112 |
| `/root` | 10,339,889,152 |
| `/root/AI-gateway` | 10,203,025,408 |
| `/var/lib` | 6,502,694,912 |
| `/usr` | 3,562,823,680 |
| `/var/tmp` | 2,927,177,728 |
| `/usr/lib` | 2,157,551,616 |
| `/var/log` | 2,112,356,352 |
| `/usr/bin` | 455,647,232 |
| `/usr/src` | 342,061,056 |
| `/usr/share` | 334,516,224 |
| `/boot` | 248,684,544 |
| `/var/cache` | 166,924,288 |
| `/usr/libexec` | 159,391,744 |

## Attempts 1–2 timeline (UTC)

| Time | Action / result |
|---|---|
| 06:29:55 | Attempt 1 starts. Recovery timer stopped, model sync paused, file/unit backups and online SQLite backup taken. |
| 06:31:07–06:31:27 | Temporary key 41 created; initial bodies, account rows, identities and memory captured. |
| 06:32:34 | In-place filter change hot-reloads without plugin reload. First direct `kimi-coding/kimi-k3` request returns 403 `invalid_authentication_error`; stop. |
| 06:32:53 | Exact original filter restored in place. Inode and plugin-subtree hash unchanged. |
| 06:33:31 | Attempt 1 ends with services unchanged, timer active, sync unpaused. Key 41: DELETE 200, GET 404, file absent. |
| 06:34–06:39 | Narrow, authorized attribution: the original config also returns 503 with and without retention; credential status says the upstream subscription lacks k3 entitlement. Working `kimi-k3` and `sensenova/kimi-k3` return 200. No credential edits. |
| 06:39:54 | Authorized second attempt starts; fresh baseline captured immediately before the filter step. Key 42 created. |
| 06:40:18 | In-place all-protocol filter applied. `kimi-k3` Responses/Chat, streaming/non-streaming: all 200; non-Kimi `claude-sonnet-5`: 200. No plugin reload. |
| 06:40:51–06:45 | C1 and the `*` resources applied after the Ansible diff preview. AISIX recreated at 512 MiB/no swap on the unchanged image digest. |
| 06:46–06:49 | AISIX lists 76 IDs, omits `*`; direct CPA-only bare request returns 200. Source-proven metadata volatility accepted by the supervisor under the explicit gate below. |
| 06:50:27–06:50:58 | C2 applied: old CPA/host relays removed; router namespace and router recreated. Local health passes and CPA addresses become unreachable. |
| 06:51–06:52 | Public known-route and CPA-only bare requests return 200; ordered list IDs unchanged. |
| 06:53:19–06:53:45 | C3 applied. Fresh online SQLite backup, CPA namespace and CPA recreated; both plugins load normally, zero hot reloads. CPA healthy in 26 seconds measured from command start. |
| 06:58:52–06:59:29 | New recovery script deployed after check/diff; sync unpaused; manual and systemd recovery checks pass; timer active. |
| 06:59:48–07:01:32 | Core post-checks pass, but the new capture has passthrough membership 47 instead of fresh-baseline 46. Stop and roll back; do not run the remaining probes. |
| 07:02:18–07:03:22 | Full reverse rollback. Original Compose/resources/config/scripts restored; CPA, router and AISIX recreated. Old CPA path reachable, health passes, timer active and sync unpaused. |
| 07:04:23–07:04:51 | Key 42 DELETE 200, GET 404, file absent. Post-rollback known-model inference returns 200. |

The warm-worker observer should exclude both window intervals: **06:29:55–06:33:31** and **06:39:54–07:03:22 UTC**. Final key cleanup and service verification finished at 07:04:51 UTC.

## Attempt 3: operational validation, then real OOM

The user explicitly replaced strict migration identity with an operational rule: investigate small/source-explained deviations and continue when runtime behavior is sound. Baselines became evidence rather than a byte-identity gate. Rollback remained mandatory for real change-caused service/inference/isolation failures, AISIX OOM, data/credential loss or unexplained catalog membership loss. The earlier gate quoted below is historical and no longer controls a future window.

| Time (UTC) | Action / result |
|---|---|
| 07:36:28 | Attempt 3 starts; timer stopped, sync paused, key 43 created, fresh baseline and online SQLite backup taken. |
| 07:38:30 | All-protocol CPA filter hot-reloads in place. Working Kimi's four protocol/stream checks and the non-Kimi check all return 200; plugin hash unchanged. |
| 07:39–07:47 | The AISIX preview detects a stale managed-file baseline from our own earlier rollback. Back up that baseline and reconcile it to the verified live original checksum, preserving mode/owner; rerun normal check/diff and deploy. Other scoped router baselines already match. |
| 07:48–07:49 | Slim router and network removals applied; health passes, CPA addresses blocked. Public known, CPA-only bare and CPA-only slash probes return 200. |
| 07:51 | CPA namespace recreated and healthy; both plugins load normally. |
| 07:57:16 | New recovery script deployed, recovery service succeeds, timer active, sync unpaused. |
| 07:57:16–08:01:02 | All 53 slash IDs exercised; all 53 exact-row counters increase. Individual unavailable/wrong-endpoint upstream results are recorded, not treated as migration regressions. |
| 08:01–08:06 | Public image generations and multipart edit return 200; public known/CPA-only models, Responses/Chat SSE and keeper-owned continuation pass. |
| 08:06:33–08:22:39 | Repeated-`x` 64 MiB prompt does not finish. Remove only the disposable curl container to cancel it. Public known probes recover to 200; no rollback for this transient test load alone. |
| 08:25:25–08:26:47 | A bounded transport probe sends a 67,108,692-byte valid Responses envelope with short input and oversized metadata, avoiding millions of paid prompt tokens. It returns 502 after 76.9 seconds. Docker records `oom` at 08:26:45, `die` at 08:26:46 and `start` at 08:26:47. **Real rollback trigger.** |
| 08:28:08–08:29:08 | Full rollback restores original topology, scripts, resources and filter. Reconcile the AISIX resource baseline to the verified restored file so the next normal drift check is honest. |
| 08:30:30–08:30:43 | Key 43 DELETE 200 / GET 404 / file absent. Three known public probes return 200; restored recovery service succeeds, timer active, sync unpaused. |

Attempt 3's deployment interval is **07:36:28–08:29:08 UTC**; final verification finished at **08:30:43 UTC**. The warm-worker observation must mark this interval separately as well as attempts 1–2.

### Functional checks in attempt 3

| Check | Result |
|---|---|
| Known bare route, public Responses / Chat | 200. |
| CPA-only bare `claude-sonnet-5` | 404 on the old selector/AISIX path before migration; direct CPA 200; new public path 200. |
| CPA-only slash `xl/gemini-3.5-flash-lite` | 200 before and after migration through the public chain. |
| All 53 AISIX slash IDs | 23 HTTP 200, 18 HTTP 502, nine HTTP 400, two HTTP 429, one 60-second client timeout. Every exact-row counter increased. Image-only IDs were also tested on their correct endpoints below; the seven intentionally excluded axis IDs were not repaired. |
| Slash attribution | All 53 own-row counters increase. `model="*"` also gains background live traffic; that aggregate is not falsely attributed to the slash probes. |
| Images through public chain | `gpt-image-2` generation 200 (16.56 s); `grok-imagine-image-2.0` generation 200 (7.56 s); multipart `gpt-image-2` edit 200 (19.61 s). The same three checks also passed directly through Sub2API. |
| Public Responses SSE | 200; 146 events, first event 2.005 s, completion 6.282 s. |
| Public Chat SSE | 200; 106 events, first event 1.458 s, completion 4.590 s. |
| Keeper parent plus delta | Both 200; keeper-owned `resp_` parent; marker absent from delta recalled correctly. |
| Unknown everywhere | Both public protocols return 400 `upstream_error` / `model_not_found`. |
| Catalog and Codex manifest | Served successfully; initial internal/public standard counts 344/344, public Codex 335. Source-explained metadata and passthrough churn were recorded, not used as rollback gates. |
| Network and health | CPA endpoints blocked, AISIX/catalog reachable; router healthy with local health route. Recovery checks all pass. |
| 64 MiB body | **FAIL: AISIX OOM at 512 MiB.** Full upload recorded; client 502, automatic restart. |

The first large-prompt test exposed a harness weakness: plain `timeout` sends TERM, while Docker's PID-1 curl can ignore it. The disposable client was explicitly removed. The second probe used `--init`, curl's own deadline and a TERM-to-KILL timeout. Both shapes are recorded; neither is represented as a successful giant-prompt execution.

After the OOM, `docker inspect` showed `OOMKilled=false` because AISIX had already automatically restarted. The event sequence, together with restart count 1 and the kernel's `memory: usage 524288kB, limit 524288kB` plus `Killed process ... (aisix)`, is the authoritative evidence. The cgroup reached its full 536,870,912-byte limit. A newly reset cgroup showed only about 185 MB peak; that post-restart value is **not** the OOM peak and must not be used to claim headroom.

Of 53 baseline containers, the same eight intended identities changed during attempt 3 and rollback; all 45 protected identities remain unchanged. No image or credential files were changed. Public rollback probes: `glm-5.3-flash` 200, `claude-opus-5-5` 200, `xl/gemini-3.5-flash-lite` 200. A transient rollback catalog snapshot showed 343 rows without `shuaiapi/claude-sonnet-5`; a later read returned 344 with the original standard-body hash and the table marked that row kept. No catalog edit was made.

## Attempt 3 closing live-traffic observation after rollback

The candidate OOM required rollback before a successful deployed-state observation could finish. A separate **30-minute observation of the restored path**, 08:33:51–09:03:52 UTC, confirms recovery; it is not claimed as proof of a successful migration. There were 31 cgroup/health samples.

| Measure | Same-length pre-window period, 07:06:28–07:36:28 | Restored path, 08:33:51–09:03:52 |
|---|---|---|
| Router HTTP 2xx / 4xx / 5xx | Exact old container logs unavailable after recreation | 511 / 0 / 0 |
| Router requests excluding healthchecks | Not reconstructable exactly from those logs | 344 2xx / 0 4xx / 0 5xx |
| Durable Sub2API evidence for the eight relay accounts | 396 usage-success records; zero final error records | Router counts above use a different seam; no false rate comparison is made |
| AISIX 2xx / 4xx / 5xx | Exact old container logs unavailable | 121 / 0 / 2 |
| AISIX 5xx and timeouts per model | Not retained | `claude-opus-5-5`: two 5xx, both timeouts; no other model has 5xx/timeouts |
| Router 413 | Not retained | 0 |
| AISIX memory.current | Initial snapshot only, not a 30-minute series | 77,041,664–119,406,592 bytes |
| AISIX memory.peak / limit | Initial snapshot only | 192,155,648 / 201,326,592 bytes |
| AISIX OOM / OOM-kill / restarts in restored container | Baseline no OOM | 0 / 0 / 0 |
| CPA state / restarts | Healthy baseline | Healthy in every sample / 0 |
| Recovery timer | Active before the window | Active in every sample; 48 successful service runs |

The exact pre-window APISIX/AISIX logs were lost when those containers were recreated: the request for this comparison arrived afterwards. Persisted Sub2API records supplement the evidence but do not replace HTTP access-log status counts. The two restored-path AISIX timeouts occur on the original topology with the pre-existing 120-second stream-gap policy; they are separate upstream findings, not a migration regression. Router responses can retain HTTP 200 after a streamed upstream timeout.

Candidate-period logs, which include deliberate validation/error probes and the large-body stress, were saved before rollback. They contain router 668 2xx / 30 4xx / 25 5xx, no 413, and AISIX 475 2xx / 32 4xx / 24 5xx. The per-model AISIX 5xx counts were four for `claude-opus-5-5`, two for `xl/claude-haiku-5-5`, and one each for `gpt-6-astra`, `aihub/gpt-5.6-sol`, `aihub/gpt-5.6-terra`, `aihub/gpt-6-astra`, the five `codex/gpt-image-*` IDs, three tested CommandCode IDs, `kimi-coding/kimi-k3`, three tested Supergrok image IDs and two tested XL image IDs. No completed access-log entry was classified as an AISIX timeout in that captured candidate period. AISIX received no `/healthz` requests; the router answered those locally. These mixed test/live totals are not presented as a clean pre/post performance comparison.

## Why the earlier strict gates stopped attempts 1–2

### Independent Kimi entitlement failure

The first attempt's Kimi credential failed independently of the filter edit. After restoring the original config, the exact model returned 503 both with and without `prompt_cache_retention`; its read-only credential state was `error`, unavailable, with an upstream message explaining that its subscription did not include k3. Native inventory lost that ID (555 → 554) and passthrough membership fell from 47 to 46. Nothing repaired or altered the credential.

The supervisor approved using working `kimi-k3` for the second attempt's retention proof, plus `claude-sonnet-5`. All five probes returned 200. CPA logged `payload.filter: updated (1 -> 1 rules)`, with the same file inode and unchanged plugin hash. The filter was restored to Responses-only during final rollback.

### Metadata volatility and final membership failure

The initial rollback showed metadata changes on `codex/gpt-6-astra`. During attempt 2, `aihub/gpt-6-astra` gained rich metadata; every changed field matched a fresh CPA rich-manifest read. Its native row had also appeared. The compatibility credential was enabled, had one credential/auth index and registered that model. The supervisor accepted this source-proven inventory recovery and fixed the gate to the following exact rule:

1. Catalog-visible output must stay byte-identical to the fresh baseline in membership, IDs and order. This covers the standard, Codex and public lists and the passthrough membership.
2. Metadata-field differences are allowed only when every changed field matches a fresh read of its source.
3. CPA native inventory differences are allowed only when they change none of the outputs in (1) and a CPA credential/auth status read explains them.
4. Anything else means: roll back per the runbook and STOP.

At 07:01:32, `kimi-coding/kimi-k3` reappeared in passthrough membership: **46 → 47**. Standard/Codex/public membership and order remained unchanged, but passthrough membership violated rule 1. The window was therefore fully rolled back without asking for another exception. The ID remained present after rollback, supporting independent inventory churn rather than a routing-induced change.

## Attempt 2 checks completed before rollback

| Check | Result |
|---|---|
| File/unit backups and two online SQLite backups | Readable, SHA-256 verified; both databases pass `integrity_check`. |
| Retention: working Kimi, Responses and Chat, stream/non-stream | Four 200 responses; non-Kimi additional check 200. |
| AISIX limit/digest/list | 512 MiB, no swap, restart count 0, correct digest; 76 IDs, no `*`. |
| CPA-only bare through AISIX and public chain | `claude-sonnet-5`: 200. |
| Known bare route, Responses and Chat | Both 200. |
| Responses / Chat SSE | Both 200; 9 / 5 data events, first events about 1.36 / 1.40 s. |
| Unknown everywhere | Both public entry protocols return 400 `upstream_error`, code `model_not_found`. Sub2API converts Chat to Responses on this path. |
| CPA-only slash | Devin request 429 `model_cooldown`; CommandCode request 502. Upstream availability was recorded, not repaired. |
| Router local health / network isolation | Router healthy, no CPA key; all four former CPA endpoints blocked, AISIX/catalog reachable. |
| CPA namespace cut | Old target absent; CPA healthy, both plugins loaded normally, zero hot reloads. |
| Recovery | All role checks true; one systemd run succeeds; timer active. |
| Catalogs | Standard/public/Sub2API bytes equal; ordered Codex IDs equal; source-proven metadata differences recorded. Final passthrough gate fails. |
| Full 53-ID sweep / images / keeper continuation / 64 MiB production probe | **Not run:** final gate failed first. |

The 64 MiB acceptance correction was approved but not exercised: no hop may return 413; no AISIX OOM; peak recorded. An upstream context-limit 400 is acceptable. This is not a successful 64 MiB production test.

The direct-probe harness required `--user 0:0` in its short-lived curl container to read its root-only 0600 curl config. Only scratch tooling changed. An initial post-cut command also needed its Go template quoting corrected; these were harness errors, not service failures.

## Attempts 1–3 memory and image identities

AISIX after the C1 activation and small probes: current 151,887,872 bytes, peak 191,639,552 bytes, max 536,870,912 bytes; swap 0 and OOM counters 0. No large-body peak was measured in production. After rollback its limits are again 201,326,592 bytes memory and 268,435,456 bytes memory-plus-swap.

In attempts 1–3, image digests did not change during activation or rollback. Attempt 4 changes only AISIX to the official index digest above; the other digests remain:

| Service | Digest |
|---|---|
| AISIX 1.6.0 | `sha256:26eae08b5c94291050f179d3c0a56ca154f40751f5c755b5922b044efc523690` |
| CPA v8.0.45 | `sha256:ce9583e43fb56e632a279b79e6203bbdedc34f09900a60fc0ced0e3f255bc63c` |
| apisix-models | `sha256:9a7e45dc943fbf10ec916d1232bae4cda9302ae28a4696cfb83c2da01d261141` |
| model-catalog v1.4.0, untouched | `sha256:f98cb5013c337420dd6f98112c053a6ad686185c723cf092062a5b0f541620ad` |

Of 53 containers, eight intended identities changed through activation/rollback; all 45 protected identities stayed unchanged. No containers are missing or added. CPA and the restored router are healthy; intended recreated containers have restart count 0.

## Fresh baseline hashes (attempt 2)

Attempt 3 captured a separate fresh baseline: internal standard and public/Sub2API standard hashes below were unchanged; internal Codex was `e361fdeecdb76671cfeaed5c394e507b922082cab5f5de40e53badc568f3435d`, public Codex `58d52042201725397a5e4625ba5c4906541c2f066cba4687ec7dcf7c244261d3`. Counts were 344 internal, 335 public Codex, 47 passthrough and 556 native. Its comparisons were operational evidence, not a byte gate.

| Body | SHA-256 |
|---|---|
| Internal standard | `8409f5b2302f9f86c7a3117aeb9414d7811a9192c032a34340a2dd63213e60c6` |
| Internal Codex | `baaedc1037122300aa295180ae50f968aaaf68be4a1ad67ba8f946ca9d7fad27` |
| Public standard / Sub2API standard | `3de725e1edb72834fdf53564dfbbc68d70b866165157d16b5f6a734fbbbaddb1` |
| Public Codex | `4762ef460c486cbcf7788750ab327e78cfc1960e90243119e43b3b1d90022119` |

Counts: internal standard/Codex 344, public standard 344, public Codex 335; fresh passthrough 46. Final rollback captures keep the same list IDs, with upstream metadata changes and passthrough 47. They are not represented as byte-identical.

## Rollback history and current residual risk

Rollback restored the backed-up Compose, resources, router files and recovery script, rewrote the original CPA config in place, recreated the old CPA namespace, restored the removed relays/networks, and recreated the router and AISIX. Its active-service phase took 64 seconds. CPA health, known inference and the old network reachability were verified. The controller's private Compose/resources copies match their original hashes again.

Attempt 4 supersedes the restored-state outcome above: production now has the `*` row and new topology, and final component previews match the live configuration. The former image pin and fresh backups remain available for rollback; the change is not archived in this window.

The explicit trade-off remains: apisix-models is a pipe, while AISIX parses whole bodies. Corrected session-derived Codex-history allocation measurements are about **3.6× peak**, not 7.6×: the old 7.6× figure used synthetic 400–600-byte random items. Adding rc.1's roughly 31 MiB idle heap to that multiplier gives only a **lower bound**, not a sizing figure; allocator, cgroup and runtime overhead also count. Attempt 3's OOM was real but used a different synthetic oversized-metadata envelope on 1.6.0 with its roughly 150 MB baseline, not representative traffic.

Local containers with a 512 MiB limit and no swap measured the following cgroup `memory.peak` values using real session-derived Codex-history bodies. The baseline is unpatched upstream main, with the same request-body path as rc.1. Values are rounded to MiB; these are local tests, not production maximum-size tests. The 96/128 MiB cases exceed the unchanged 64 MiB gateway contract.

| Body / concurrency | Unpatched baseline | Pending A+B proposal |
| --- | --- | --- |
| One 64 MiB | 333 MiB | 241 MiB |
| Two concurrent 64 MiB | 496 MiB | 401 MiB |
| Three concurrent 32 MiB | 363 MiB | 321 MiB |
| Three concurrent 48 MiB | OOM | 455 MiB |
| One 96 MiB | 505 MiB | 347 MiB |
| One 128 MiB | OOM | 405 MiB |
| Three concurrent 64 MiB | OOM | OOM |

One real 64 MiB history fits. Two concurrent ones measured at the edge, with only **16 MiB headroom**; three concurrent large histories can OOM. No concurrency guarantee is made. Evidence: `/var/tmp/ai-aisix-bodymem/mem-base.jsonl`, `mem2-base.jsonl`, `mem-patched.jsonl` and `mem2-patched.jsonl`. No private session contents are included here.

The schema-memory fix is included in the deployed image. The separate A+B proposal avoids the deep clone when only `model` changes and reuses wire bytes for streaming estimation, measuring about **2.3× real-body allocation peak and 1.0× held while streaming**. It is **not yet submitted**, is not in this image and is not claimed as a delivered optimization.

### Attempt 4 rollback commands

Use the fresh `bk/attempt4-files.tar`, restore the controller's matching private Compose/environment/resources copies, and retain the old image pin. On the host, from the deployed checkout (`W` is the window scratch directory):

```bash
systemctl stop ai-gateway-recovery.timer
docker pause ai-gateway-cpa-model-sync-1
for f in compose.yaml .env data/cpa/conf/config.yaml \
  services/routing/aisix/resources.yaml ops/scripts/host/gateway-recovery.py; do
  tar -xOf "$W/bk/attempt4-files.tar" "$f" > "$f"
done
tar -xf "$W/bk/attempt4-files.tar" services/routing/apisix-models
DC=(docker compose -p ai-gateway -f compose.yaml -f compose.override.yaml --env-file .env)
"${DC[@]}" up -d --no-deps --no-build --pull never --force-recreate cpa-netns cli-proxy-api
# Wait for CPA healthy before the next two groups.
"${DC[@]}" up -d --no-deps --no-build --pull never --force-recreate \
  apisix-models-host-netns apisix-models-host-relay apisix-models-cpa-relay \
  apisix-models-netns apisix-models
"${DC[@]}" up -d --no-deps --no-build --pull never --force-recreate aisix
cat services/routing/aisix/resources.yaml > .ai-ops-state/aisix__resources.yaml.baseline
# Verify old health/inference/CPA reachability before resuming the guards.
docker unpause ai-gateway-cpa-model-sync-1
systemctl start ai-gateway-recovery.timer
```

Wrap each remote/runtime operation in a timeout as in the runbook. Restore the CPA config in place, not by rename; do not change plugins or restore the quota database for an ordinary topology rollback. The database backups are disaster-recovery artifacts, not permission to discard new accounting data.

Private evidence and the reviewed runbook remain under the window scratch directory. No credentials, private addresses or request/response content are included here.
