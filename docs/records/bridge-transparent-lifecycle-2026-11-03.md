# Bridge transparent-lifecycle rollout — 2026-11-03

## Delivered scope

Only `responses-ws-sse-bridge` now runs the accepted transparent-lifecycle implementation (no pipe-invented turn-idle/text-write expiry, no fixed global socket refusal). No keeper, SSE keepalive, CPA, Sub2API, namespace-owner or adjacent-container rollout occurred. This is not whole-path timeout removal, and it does not resolve the other components' online blockers.

- Source: `xz-dev/responses-ws-sse-bridge@b482f7128b5f904c50a123889a22030aaa890d8a` (branch `maintenance/transparent-lifecycle`); exact-source CI [41602112775](https://github.com/xz-dev/responses-ws-sse-bridge/actions/runs/41602112775) passed. The repaired source received independent approval in review run `68d54b22` before publication.
- Immutable image: `ghcr.io/xz-dev/responses-ws-sse-bridge:b482f7128b5f904c50a123889a22030aaa890d8a@sha256:c7c010be8e71f9ebc5365af315ce0b7323a1403593d9661d3c1ba0586ba40c35` — single `linux/amd64` manifest, published OCI config digest `2578a61c09ecdc2d89979fec4ce3e666bd26bc7acf9e38ffffa5e33bd6cde018`.
- Gateway public change: only the existing `RESPONSES_WS_SSE_BRIDGE_IMAGE` default pin (compose default and `.env.example`) moved to the immutable reference above. No other component, environment, topology or resource change. Unrelated plugin gitlinks and the rustls-0.998 integration are outside this change; no plugin rebase.

## Accepted isolated runtime evidence

Isolated resource CI [41615071846](https://github.com/xz-dev/responses-ws-sse-bridge/actions/runs/41615071846) passed at harness fixture `53040cf36f87d8bd0ced0743b73fc6debf791dcf`, after independent fresh review (`9caba`) approved the fixture's targeted flow. Actual runtime, not config intent: rootless Podman 4.9.3; app cgroup 256 MiB RAM, total 256 MiB, swap 0, `GOMEMLIMIT=192MiB`, CPU 0.5, PIDs 64, UID 65534, read-only rootfs, capabilities all zero, private isolated network namespace, separate driver cgroup. The named volume's actual filesystem is ext4 (`ef53`/4096) — the same type the fresh production host exposes, not the controller's ZFS.

- 65 completed lightweight sessions all retained their WebSocket and every Ping; a 32-second unfinished source stayed blocked while an unrelated session B completed.
- Drain delivered exactly 1024 × 64 KiB = 64 MiB with exact bytes, type and order, terminal event, and same-connection continuation.
- A second large event blocked ≥ 2 s at plateau, with 5 actual samples ≥ 1 MiB entirely inside that phase before cancellation; no sample predated it.
- Blocked backend and selected reader joined in a combined **0.402435 ms** (≤ 200 ms CloseNow budget).
- 152 actual samples, peak memory 12,804,096 bytes, no OOM, no restart; spool files and open spool FDs returned to 0; clean stop took 0.101 s; nonce cleanup verified.

This fixture proves bounded bridge forwarding/cancellation behavior, not production provider behavior. No paid-provider request ran. The production native Responses WS-to-SSE provider path was not probed; the isolated synthetic fixture is not provider acceptance.

### Earlier fixture attempts: failed, preserved

Briefly, before `53040cf3`: one attempt used an invalid workflow context; one required Go ≥ 1.26 against 1.24; one rejected Podman's expansion of `CapDrop=ALL` into capability names; one volume first-mounted root `0755` with no proven reset mechanism; and the `03` fixture clamped every client receive to 16 KiB permanently, so it timed out before the full 64 MiB drain could complete. The `530` fixture restores the exact original receive buffer only after the 32-second blocked-source proof, with no application capability, timer, chunk-size or oracle waiver. The failed runs were not re-labelled as passing, and no single sole cause is claimed for them.

## Guarded production activation (bridge only)

Read-back-observed facts: rollout `StartedAt` **2026-11-03T10:19:09.642579289Z**; health healthy, restart count zero, no OOM. The exact new image/source reference was verified live, and the actual Docker engine image ID is `sha256:c7c010be8e71f9ebc5365af315ce0b7323a1403593d9661d3c1ba0586ba40c35` (directly observed; it differs from the fixture Podman ID `2578…`, and engine IDs were not inferred from the OCI config digest). The immutable RepoDigest matches. The only effective Compose change was the bridge `IMAGE`.

All **47** other project containers kept exact identities, config, host config, image and state summaries; namespace-owner identity and inode unchanged; resource/security/environment settings unchanged. The old image and the 0700 snapshots / 0600 files were SHA-verified and remain, with the protected controller copy retained; no rollback was executed. A fresh image-bound preview and digest approval, shared writer lock with CAS, and capacity gates (1024 MiB reserve, ≥ 200 MiB MemAvailable) were honored; the transaction completed. Private controller copies and the original ignored Compose image were adopted; env and override unchanged. Final read-only preview: `config_diff=false`, `activation_required=false`, lock free. Completed SSE and bridge applies are never replayed.

Production spool root remains **0755** (old baseline), preserved; the fixture's prepared 0700 is not a production permission claim, and no production permission change occurred.

## Limits and remaining work

- Neither the isolated fixture nor the production read-back is a paid-provider native Responses WS-to-SSE end-to-end probe; none ran.
- Downtime is **unmeasured**; `StartedAt` is not an Ansible-elapsed substitute.
- Proper WebSocket Close was **not** tested and is separate from the 200 ms TCP join result.
- AISIX's deliberate 120-second upstream-read policy and Squid are unchanged. SSE keepalive transparent-write is already complete.
- Keeper: the frozen 83-file candidate passed both full and race suites (230 top-level / 412 total passing tests each), including the 96/65 cases, but final review returned **CHANGES_REQUIRED** for 4 P1 byte/lifetime/staging/cancel gaps. A separate baseline defect was reproduced: a legal approximately 2 MiB response returns 413; production keeper still runs old `b49`; no approval, publish or deploy. CPA 8.0.51 + PR7134 immutable candidate is **not** deployed; Sub2API activation stays **paused**. The initial HTTP/2 peer reset / pasted 503 / invalid-frame correlation remains unproven.
- The deployed bridge RELIEF is reported separately from those online blockers; whole-gateway timeout behavior is not claimed fixed.
- OpenSpec `transparent-pipe-lifecycle` stays open: tasks 2.1, 2.3 and 4.1–4.4 (including keeper slice and independent closeout review) remain.
