# AISIX-first routing window — rolled back, 2026-10-29

## Outcome

**Not deployed.** Both attempts stopped at their acceptance gates. The second attempt reached the slim router, namespace cut and recovery checks, then the final passthrough-membership gate failed. All window changes were reversed. No third attempt was made.

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

## Why the gates stopped

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

## Checks completed before rollback

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

| Body | SHA-256 |
|---|---|
| Internal standard | `8409f5b2302f9f86c7a3117aeb9414d7811a9192c032a34340a2dd63213e60c6` |
| Internal Codex | `baaedc1037122300aa295180ae50f968aaaf68be4a1ad67ba8f946ca9d7fad27` |
| Public standard / Sub2API standard | `3de725e1edb72834fdf53564dfbbc68d70b866165157d16b5f6a734fbbbaddb1` |
| Public Codex | `4762ef460c486cbcf7788750ab327e78cfc1960e90243119e43b3b1d90022119` |

Counts: internal standard/Codex 344, public standard 344, public Codex 335; fresh passthrough 46. Final rollback captures keep the same list IDs, with upstream metadata changes and passthrough 47. They are not represented as byte-identical.

## Rollback and residual risk

Rollback restored the backed-up Compose, resources, router files and recovery script, rewrote the original CPA config in place, recreated the old CPA namespace, restored the removed relays/networks, and recreated the router and AISIX. Its active-service phase took 64 seconds. CPA health, known inference and the old network reachability were verified. The controller's private Compose/resources copies match their original hashes again.

The next window must still complete all remaining acceptance probes. Main intentionally remains ahead of production: its AISIX policy requires the `*` row, while production has no such row. Do not use current-main AISIX resource deployment blindly.

The explicit architectural trade-off remains: apisix-models is a pipe, but AISIX buffers whole bodies with a measured peak near **7.6× body size**. Concurrent 64 MiB requests at 512 MiB are near the limit and may OOM; no concurrency guarantee is made. The pending upstream jsonschema fix removes only the roughly 106 MiB idle baseline, not this body multiplier. At the restored 192 MiB limit, large-body burst risk remains.

Private evidence and the reviewed rollback commands remain under the window scratch directory. No credentials, private addresses or request/response content are included here.
