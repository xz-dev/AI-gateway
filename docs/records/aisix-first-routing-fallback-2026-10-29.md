# AISIX-first routing window — rolled back, 2026-10-29

## Outcome

**Not deployed: attempt 3 hit a real AISIX OOM at 512 MiB.** Attempts 1–2 stopped under the earlier strict gates. The user then replaced those gates with operational validation and authorized attempt 3. That attempt continued through benign upstream inventory churn and completed the functional probes, but a large-body probe OOM-killed AISIX. All window changes were reversed again; no fourth attempt was made.

Production again uses the original selector, CPA relay and host relay, AISIX's original resources without `*`, AISIX at 192 MiB plus 64 MiB swap, the original recovery script and the original Responses-only CPA retention filter. The earlier 120-second AISIX stream timeout and model-catalog v1.4.0 remain unchanged.

The user approved immediate execution, the all-protocol retention filter and a CPA namespace recreation rather than a live network disconnect. One operator owned CPA throughout. The last-30-minute access-log sample contained 700 requests including 166 healthchecks (23.3/minute), below the approved 30/minute postponement threshold.

## Timeline (UTC)

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

## Closing live-traffic observation after rollback

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

## Memory and image identities

AISIX after the C1 activation and small probes: current 151,887,872 bytes, peak 191,639,552 bytes, max 536,870,912 bytes; swap 0 and OOM counters 0. No large-body peak was measured in production. After rollback its limits are again 201,326,592 bytes memory and 268,435,456 bytes memory-plus-swap.

Image digests did not change in either forward activation or rollback:

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

## Rollback and residual risk

Rollback restored the backed-up Compose, resources, router files and recovery script, rewrote the original CPA config in place, recreated the old CPA namespace, restored the removed relays/networks, and recreated the router and AISIX. Its active-service phase took 64 seconds. CPA health, known inference and the old network reachability were verified. The controller's private Compose/resources copies match their original hashes again.

The next window needs a new large-body/resource decision: a single near-64 MiB envelope has now OOM-killed the 512 MiB candidate. The functional checks completed in attempt 3 do not erase that failure. Main intentionally remains ahead of production: its AISIX policy requires the `*` row, while production has no such row. Do not use current-main AISIX resource deployment blindly.

The explicit architectural trade-off remains: apisix-models is a pipe, but AISIX buffers whole bodies with a measured peak near **7.6× body size**. Concurrent 64 MiB requests at 512 MiB are near the limit and may OOM; no concurrency guarantee is made. Attempt 3 additionally disproved safety for every single-request JSON shape at that limit. The pending upstream jsonschema fix removes only the roughly 106 MiB idle baseline, not this body multiplier. At the restored 192 MiB limit, large-body burst risk remains.

Private evidence and the reviewed rollback commands remain under the window scratch directory. No credentials, private addresses or request/response content are included here.
