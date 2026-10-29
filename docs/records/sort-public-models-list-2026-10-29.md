# Plain public model-list ordering — deployed, 2026-10-29

## Outcome

Deployed implementation `e9ec1f7` through one front-door APISIX recreate, with no rollback. Its Validate run **40886153407** completed successfully before activation. After supervisor review, the completed change was synced to the main specification and archived under `openspec/changes/archive/2026-10-29-sort-public-models-list/`. Documentation close-out `c7bf1cc` passed Validate run **40899470701** before archival.

Window: **14:22:35–14:31:03 UTC**. Only `apisix` changed container identity. All 49 other container snapshots, including `cloudflared-apisix-relay`, remained identical in ID, StartedAt, status, health and restart count. The new APISIX is healthy with zero restarts.

The public hostname was used for all client-facing captures. Plain lists were already in the desired order in this particular live inventory, so their body hashes did not change. The new handler is nevertheless active: reviewed file hashes match, and both plain routes now return the final-entity ETag/private-cache policy and satisfy conditional requests. Sorting of unsorted inputs is covered by the implementation's pinned-image contract tests.

## Preconditions and acceptance policy

- No other production window was active; the earlier disk cleanup had finished.
- Both component previews, from `ops/ansible/`, used `deploy.yml -e component=apisix --check --diff` with the existing Docker runtime/Compose selection. Each returned `ok=63 changed=0 unreachable=0 failed=0`.
- Both live front-door files matched the parent of `e9ec1f7` before upload.
- A temporary normal-client key, ID **45**, used the existing normal-client group. No existing client key was changed.
- The supervisor explicitly reconciled task 3.3 with the user's behavior-based migration policy before activation. Investigate and record benign inventory changes; roll back for an unhealthy/restarting service, non-200 plain list, unexplained missing entries, modified record content, incorrect ordering/cache policy, a changed Codex manifest order, lost opaque 404, or new Lua/module/route-load failures. No membership exception was needed: the captured lists match exactly.

## Backup and file verification

Host backup directory: `.ai-ops-state/sort-public-models-list-20261029T142328Z/` (0700). Both backups and their hash manifest are 0600. The Lua and YAML files were uploaded from the reviewed commit; SHA-256 was checked after upload and again at window end. YAML used a replacement inode so the old running single-file mount stayed unchanged until recreate.

| File | Before SHA-256 | After SHA-256 |
|---|---|---|
| `services/firewall/apisix/apisix.yaml` | `3f7624efb488c9641a0739fe2cdcfe0ee120f7a83f7a7be13d7cf4c23c86ce6f` | `564e63e84f8355ac390cbf8caf7e4c843d27bdc0b375626fef06cd0ddcf6c59e` |
| `services/firewall/apisix/lua/model_list_intersection.lua` | `8c82f2f039baf9694b29bb9528be06e008dda7fdb21e6aa460cc2663bcdf0504` | `ac7ac7e787addd01680c8529cc063d508ca7caaa52f333908421a4c5489ea2b0` |

No image, Compose, environment, private configuration, relay or namespace change was made.

## Connection samples and recreate evidence

Established connections on the front listener (port 9080, TCP state 01, both address families):

| Sample UTC | Established |
|---|---:|
| 14:23:30.770 | 4 |
| 14:23:40.899 | 4 |
| 14:23:50.987 | 4 |

The three samples were stable at four connections. The window proceeded at that low count using the component's existing secret-free `env -i PATH=... docker compose` arguments and `up -d --force-recreate --no-deps --no-build --pull never apisix`.

| Evidence | Value |
|---|---|
| Compose invocation | 14:24:04.614 UTC |
| Old APISIX ID | `2ed7b244a0e0bb077b6e3ba48d44a20aa943c3e02750ea3c96c35c1ae2974502` |
| New APISIX ID | `7615bb16901d92d2ce03aa665c5b042ad1d2b5b625bc268306e5b19ba4be3d8e` |
| New StartedAt | 14:24:19.388 UTC |
| Healthy confirmed | 14:24:27.687 UTC |
| Recreate-to-healthy | Approximately **23.1 seconds** |
| New container restarts | **0** |
| Relay ID, unchanged | `914da3d428c6a759ea0c438061a7d1e7bb88dc850fceb4ef53a39771504c11f5` |
| Actual listener outage | **UNMEASURED**; not inferred from recreate duration |

The outage probe was invalid: direct host-to-container-IP requests were unreachable both before and after activation. All 112 probe results were failures, so the apparent 22.728-second interval was discarded rather than reported as an outage. Docker's historical events query returned no events for the interval. The supervisor approved retaining the reliable lifecycle timings above and not disrupting public streams with another recreate merely to remeasure. The public post-captures and the existing local unauthenticated `/v1/models` healthcheck succeeded (the latter expects 404).

## Public pre/post checks

An independent Python check compared ID multisets (including multiplicities), parsed records, raw JSON record fragments and all other top-level fields. The plain order check used `sorted(ids, key=lambda model: ('/' in model, model))`, not the Lua implementation.

| Check | Before | After | Result |
|---|---|---|---|
| `/v1/models` | 200; 343 entries | 200; 343 entries | Added 0, removed 0; independently ordered |
| `/models` | 200; 343 entries | 200; 343 entries | Added 0, removed 0; independently ordered |
| Plain record/envelope fidelity | Captured | Parsed records and raw fragments identical | 0 changed entries; envelope identical |
| Direct current upstream inventory | 343 entries | 343 entries | Same ID multiset and records as public list |
| Codex manifest (`client_version=0.101.0`) | 200; 334 entries | 200; 334 entries | Bytes, IDs and order identical |
| Unauthenticated `/v1/models` | 404; empty body | 404; empty body | Opaque behavior preserved |
| Plain `Cache-Control` | Captured | `private, no-cache` | Pass, both routes |
| Plain `Vary` | Captured | Includes `Authorization` | Pass, both routes |
| Plain final-body ETag | Captured | Strong `"apisix-<md5(body)>"` | Pass, both routes, identity encoding |
| `If-None-Match` | Not required pre-window | 304; empty body | Pass, both routes |
| Failed `If-Match` | Not required pre-window | 412; empty body | Pass, both routes |

Body SHA-256 values:

| Capture | Bytes | Before / after SHA-256 |
|---|---:|---|
| Both plain paths | 28,993 each | `90ea820939303bd9708c1508834390979b2ccbd277ed65dad41c6eb9131f13c2` |
| Codex manifest | 2,868,908 | `5459ba1d8705e06e0387f4b80e2d642dc43f5d93532bb3e1414b2a77c638c2dc` |
| Unauthenticated body | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

### Header and log observations

The first post-capture negotiated public-edge `zstd` compression. That edge changed the otherwise matching ETag to `W/"apisix-..."`; the existing Codex route showed the same behavior. An exact strong-tag assertion therefore initially failed in the verification helper, not in the application. Repeating with `Accept-Encoding: identity` verified the strong final-body hash and both 304/412 responses through the public hostname. No configuration change was made to accommodate the probe.

At final verification there were no ordering-skipped WARNs, Lua stack traces or module/route-load failures. The log did contain **28** instances of:

```text
attempt to set status 401 via ngx.exit after sending out the response status 404
```

The supervisor's subsequent read-only analysis established that this change **introduced the error on the plain route**. The old plain route proxied Sub2API's 401, which the global `response-rewrite` changed to 404 in `header_filter` without a Lua exit. The new `list()` fetches Sub2API itself and returns that non-2xx 401 through `core.response.exit`. Sending the body runs the header filter, which rewrites the status to 404 and sends headers; the final `ngx.exit(401)` then logs the status conflict. The parameterized `run()` path has the same latent pattern for unauthenticated `client_version` requests.

A later sample contained **185** such lines between **14:24:24 and 15:10:41 UTC**, all unauthenticated `GET /v1/models HTTP/1.0`, approximately four per minute from a periodic client (about 5,800 lines/day if sustained). The earlier search of 299 retained host scratch/state logs and controller logs found no pre-window sample; that absence is not used to classify this as pre-existing.

The supervisor accepted this error-level log-noise regression as a follow-up, **not a rollback condition**, because the client-visible response remains the correct opaque 404 and authenticated requests succeed. No unrelated error category was observed.

### Follow-up fix — deployed

The Lua follow-up `7264e56` is **deployed**; see the auth-shaping fix window below. Both `list()` and `run()` now share `basic_passthrough()`: a basic-leg 401 becomes an empty 404 before any upstream headers are copied. All other basic-leg statuses retain their previous status, body and header behavior; the catalog leg and global response-shaping configuration are unchanged.

The added regression failed against the old handler (`list did not return an opaque 404`). All **17** contract checks now pass in the pinned APISIX image, covering both handlers and both model-list paths, absent upstream headers and no enriched-inventory request after auth rejection. Foreground `./ops/scripts/validate.sh .env.example` returned 0 in an isolated worktree with no Compose override before or after; the existing Lua codec/global-variable warnings remain.

A separate reviewed APISIX-only window deployed the fix after its Validate run **40905854228** completed successfully. It changed only `services/firewall/apisix/lua/model_list_intersection.lua`; no YAML, image, relay or other service changed. The pre-fix Lua file remains available for rollback.

## Close-out

- Temporary key **45**: DELETE **200**, follow-up GET **404**, then the host-local key file was deleted and absence verified at **14:27:58 UTC**. No key material was copied to the controller or committed.
- Final component preview: `changed=0`, no failures; it guards Compose/environment drift, while the separate file hash checks guard the manual front-door upload.
- Final snapshot: 50 containers, only APISIX changed identity; new health healthy and restarts=0.
- Implementation CI: `e9ec1f7`, Validate **40886153407**, completed/success.
- Window-record CI: `3ba32d8`, Validate **40892719024**, completed/success. Task 3.4 is checked against that published evidence. The subsequent documentation-only close-out records the auth-shaping cause and this CI result; it makes no production change.
- Raw private captures and summaries remain under `/var/tmp/ai-sort-models-window/` on the host and controller. No credentials, public hostname, addresses or private configuration contents are included here.

## Rollback (not exercised)

On the host, set `DEPLOY_ROOT` to the existing deployment checkout and `REMOTE_PATH` to its normal remote PATH. Restore only the two front-door files from the retained backup, preserving the live file modes, then recreate only APISIX:

```sh
B="$DEPLOY_ROOT/.ai-ops-state/sort-public-models-list-20261029T142328Z"
for rel in services/firewall/apisix/apisix.yaml services/firewall/apisix/lua/model_list_intersection.lua; do
  cp "$B/$(basename "$rel")" "$DEPLOY_ROOT/$rel.restore"
  chmod --reference="$DEPLOY_ROOT/$rel" "$DEPLOY_ROOT/$rel.restore"
  mv "$DEPLOY_ROOT/$rel.restore" "$DEPLOY_ROOT/$rel"
done
sha256sum "$DEPLOY_ROOT/services/firewall/apisix/apisix.yaml" \
  "$DEPLOY_ROOT/services/firewall/apisix/lua/model_list_intersection.lua"
# Compare both values with the Before SHA-256 column before recreating.
timeout 90 env -i PATH="$REMOTE_PATH" docker compose -p ai-gateway \
  -f "$DEPLOY_ROOT/compose.yaml" -f "$DEPLOY_ROOT/compose.override.yaml" \
  --env-file "$DEPLOY_ROOT/.env" \
  up -d --force-recreate --no-deps --no-build --pull never apisix
```

Verify healthy/restarts, unchanged relay identity, restored pre-capture behavior, public plain lists, Codex manifest and opaque unauthenticated 404. No other component needs restarting. A rollback would cut in-flight public streams again; it was not needed in this window.

## Auth-shaping fix deployment

Fix `7264e5613a0c4a6b8c1fc0bdbcc07acd8b3ab3b1` was deployed without rollback during **16:23:15–16:35:23 UTC**, after Validate **40905854228** succeeded. The only reviewed file delta was the Lua module: its live pre-window SHA-256 matched the fix's parent; YAML and APISIX configuration hashes were unchanged. Both component previews returned `ok=63 changed=0 unreachable=0 failed=0`. These previews cover Compose/environment only; the separate SHA-256 checks cover the manually deployed Lua file.

The Lua backup and its hash manifest are 0600 inside the 0700 directory `.ai-ops-state/sort-public-models-list-20261029T162441Z/`. The uploaded Lua SHA-256 is `65aa2feb01ed1fe643e5bb238e22a533ac98b667551a897118071f5cbdb57a0c`. Connection samples at 16:24:44, 16:24:54 and 16:25:04 UTC were **4 / 3 / 3**. Only `apisix` was recreated using the same bounded, secret-free Compose invocation above.

| Lifecycle evidence | Value |
|---|---|
| Recreate invocation | 16:25:13.241 UTC |
| New StartedAt | 16:25:25.773 UTC |
| Healthy confirmed | 16:25:33.274 UTC |
| Recreate-to-healthy | Approximately **20.0 seconds**; actual listener outage unmeasured |
| Old APISIX ID | `7615bb16901d92d2ce03aa665c5b042ad1d2b5b625bc268306e5b19ba4be3d8e` |
| New APISIX ID | `05c819b496ccd7eb5918761f4f32bd6953e3b7b8bc88587199d3d9f339094d01` |
| Final health/restarts | Healthy / **0** |
| Other containers | All **49** unchanged, including the relay |

| Public post-check | Result |
|---|---|
| `/v1/models` and `/models` | **343 → 343** each; bodies and ID order byte-identical to pre-capture |
| Codex manifest | **334 → 334**; body and order byte-identical |
| Cache policy and identity-encoding ETag | `private, no-cache`, `Vary: Authorization`, strong final-body ETag preserved |
| Conditional requests, both plain routes | **304** / **412**, empty bodies |
| Unauthenticated plain routes and parameterized manifest | **404**, empty body; upstream/authentication headers absent |
| Status-conflict errors | **119** in the prior **30 minutes** → **0** over **610.3 seconds** after recreate |
| Other Lua/route-load/error-level log lines | **0** during the same observation |

The successful healthcheck continued every approximately 15 seconds throughout the observation, exercising the unauthenticated model-list path. This distinguishes the zero-error result from an idle service. There were no membership or content deviations.

Temporary key **46** was deleted at **16:26:05 UTC**: DELETE **200**, GET **404**, host-local key file absent. No key material left the host. Final container comparison again showed only APISIX changed; file hashes matched the reviewed target. Private captures and summaries remain under `/var/tmp/ai-authfix-window/` on the host and controller.

Rollback, not exercised: restore **only** `model_list_intersection.lua` from this fix window's backup, verify the original SHA-256 `ac7ac7e787addd01680c8529cc063d508ca7caaa52f333908421a4c5489ea2b0`, then use the APISIX-only recreate command above. Do not restore the earlier window's YAML for this fix. Repeat the health, public-list/manifest and opaque-404 checks; a rollback would reintroduce the known log noise.
