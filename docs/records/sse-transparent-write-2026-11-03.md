# SSE transparent-write rollout — 2026-11-03

## Delivered scope

Only `ai-sse-keepalive-proxy` now runs the accepted transparent established-WebSocket write implementation. No keeper, bridge, CPA, Sub2API, namespace-owner or adjacent-container rollout occurred. This is not whole-path timeout removal.

- Source: `xz-dev/ai-sse-keepalive-proxy@5ff3e338b1ed301ce72832b9e06893e9975dbd51`; exact-source CI [41566630106](https://github.com/xz-dev/ai-sse-keepalive-proxy/actions/runs/41566630106) passed before rollout.
- Immutable image: `ghcr.io/xz-dev/ai-sse-keepalive-proxy:5ff3e338b1ed301ce72832b9e06893e9975dbd51@sha256:b12366714e08463765266ec60a1f402d045febd4955edcbef590ddffc2bf20ec`.
- Functional gateway integration: `e62a51fae82078adae5833d86a7e97be2688844b`; exact-revision gateway CI `41569639910`, attempt 1, passed all four jobs / 37 steps. Existing gateway main changes were preserved.
- Gateway public changes remain the SSE gitlink, immutable default pin and obsolete `WS_WRITE_TIMEOUT` mapping removal. Private Compose used a literal image, so updating its unused `.env` image value alone did not change the effective image.

## Accepted isolated runtime evidence

Parent independently accepted the actual published image under the unchanged 128 MiB memory / 256 MiB memory-plus-swap, CPU 0.5, PID 128, read-only, cap-drop envelope, with `GOMEMLIMIT=96MiB` and isolated networking. A blocked write remained backpressured at 110 of 512 frames; an independent connection progressed. Resumption delivered all 512 × 64 KiB frames (32 MiB), exact type/order/bytes and same-connection echo. A second blocked-session cancellation released upstream in less than 1 ms while another connection remained live. Peak measured memory was 13,787,136 bytes, no OOM, zero restarts; normal stop took 0.139 s. Protected evidence: `runtime-probe/resource-acceptance.json`. CI's 160/160 smoke result was not substituted for this resource gate.

This fixture proves bounded forwarding/cancellation behavior, not production provider behavior. Production received no paid inference or stress traffic.

## Recovery material and guarded activation

Protected old `.env`, Compose and unchanged override copies were hash/mode verified on the controller. The initial remote backup had only the two changed files; before applying, the unchanged override was copied only after its live hash matched the expected baseline. The prior remote manifest was preserved before adding the override entry. All three remote backup hashes then verified; private directories/files remain 0700/0600. One initial manifest verification used the wrong working directory, failed without changing deployment, then passed from the manifest's intended parent directory. No rollback image, snapshot or backup was removed.

Both component transactions used the documented Ansible profile and a 35-minute controller bound, normal exclusive writer lock, fresh identity/CAS checks and unchanged capacity gates (at least 200 MiB MemAvailable and 1024 MiB disk). Only the selected service used `--force-recreate --no-deps --no-build --pull never`.

### First attempt: incomplete delivery, not success

UTC controller window: **04:05:50–04:15:18**. The approved original preview bound a candidate whose private Compose still hardcoded the old image. The controller exited zero and recreated SSE at **04:13:13.104313855Z**, but exact independent readback showed old revision `1c489390927a986b7aacd3ebdfef3ea33f51d15d`. The obsolete mapping was removed and the service was healthy; the new implementation was **not** delivered. No original-controller adoption or blind retry followed.

Read-only diagnosis found the selected private Compose image literal overrode the newly staged `.env` variable. The override file had no selected-image override and remained unchanged. Original approval `2b29d8f8ccdd471121f4157fe5b2f0caf626a2e2d303e70c2e3692d3ece9342a` was retired. Its complete transaction and snapshot remain recovery evidence.

### Corrected attempt: exact image verified

Only the selected private Compose image literal was corrected. Independent `env -i` old/new effective renders were equal after replacing that one image back; other services and all non-image fields were unchanged. Both staging copies asserted the complete target reference before preview and apply. The fresh preview bound the current post-first-attempt container and 47 protected neighbours. Parent independently verified and approved new digest `ec822324654d9072aa9dcc183a709b590960bf92a608107ea07bff602c7aa591`. Preview capacity was 919 MiB MemAvailable / 1985 MiB disk; apply rechecked normal gates under lock.

UTC controller window: **04:29:01–04:37:31**. Correct target `StartedAt`: **04:35:31.501774908Z**. Independent readback at **04:37:57Z** verified:

- Full container image reference exactly equals the published `5ff3e338…@sha256:b1236671…` pin; container/image OCI revision labels both equal full source revision.
- Actual Docker engine image ID is `sha256:b12366714e08463765266ec60a1f402d045febd4955edcbef590ddffc2bf20ec`, and its RepoDigest matches. This is observed engine identity, not an assumption that OCI config digest equals engine ID; published OCI config digest is `bab2f8d3c31a44d60805e44675706ad756961f56785772451b69e4811b5a5699`.
- Original `/ai-sse-keepalive-proxy` entrypoint and UID/GID `65534:65534`; no invented version-banner probe.
- Healthy/running, no OOM, restart count zero; memory 134217728 bytes, memory-plus-swap 268435456 bytes, CPU 0.5, PID 128, read-only rootfs, cap-drop ALL and `GOMEMLIMIT=96MiB` preserved. `WS_WRITE_TIMEOUT` absent.
- Same namespace owner and matching network-namespace inode. All **47** other project containers kept exact IDs, image references/IDs, StartedAt, Config/HostConfig hashes, status, health, OOM and restart summaries across both attempts.
- Selected HostConfig hash changed on the first recreate relative to the original container; its cause was not identified. It stayed identical through the corrected recreate. Approved effective Compose structure and explicit live resource/security limits were preserved; no exact pre-first target HostConfig-byte claim is made.
- Bounded, non-inference local `/healthz` probes passed: SSE/keeper/bridge 200, front APISIX 404, inner APISIX 200. These prove only local liveness/routing rejection, not paid upstream request success. Existing model-catalog unhealthy status and AISIX's absent HEALTHCHECK were not reclassified as rollout regressions.

These are controller and StartedAt timestamps, **not measured downtime**. Selected in-flight streams could be interrupted by each recreation; interruption duration was not measured.

## Adoption and zero drift

Only after exact live validation, the original controller adopted exactly two ignored private files: `.env` and `compose.yaml`. Before-copy backups/hashes were retained. Independent original/candidate effective models differed only in selected SSE image and removal of `WS_WRITE_TIMEOUT`. The private override stayed byte-identical. Original HEAD, index, refs, tracked content, existing public dirty state, `.pi` and `.playwright` were snapshotted and equal before/after adoption; unrelated work was preserved.

Final fresh selected-component preview exited zero: `config_diff=false`, `activation_required=false`, `environment_changes=[]`, exact target pin, healthy, 47 protected containers and writer lock unheld. No recreation was proposed. Controller/live/adopted three-file hashes agree.

Protected receipts: `controller/.ai-ops-state/sse-apply.log`, `sse-readback.json`, `sse-corrected-preview.log`, `sse-effective-pin-correction.json`, `sse-corrected-apply.log`, `sse-corrected-readback.json`, `sse-live-validation.json`, `sse-health-probes.json`, `sse-original-adoption.json`, `original-protected-{before,after}-adoption.json`, and `sse-final-no-drift.log`. Public record intentionally omits private configuration and host identifiers. Exact final documentation-commit CI is checked separately in the protected closeout receipt; earlier functional CI does not substitute for it.

## Rollback and remaining work

Both immutable transaction snapshots and initial three-file backups remain. The old `1c489390…@sha256:bc5e360f…` image remains locally available. Latest guarded rollback is bound to the corrected transaction's immediate predecessor (old image, obsolete mapping already absent), not automatically the pre-first-attempt files. Use a fresh identity-bound `rollback.yml --check --diff`, review its own digest, apply only with approval, then independently verify exact old image, health, namespace, neighbours and adopted no-drift state. A failed/unknown receipt requires reconciliation, never blind retry or backup overwrite.

- Keeper **8-second phase wait / global processing gate of 1 / 64-count policy** and bridge **30-second policy / 64-slot policy** remain **not fixed** by this rollout.
- APISIX 3.19 has no verified native timeout-off setting: zero is rejected and omission inherits a finite default. No APISIX configuration/engine/topology change occurred.
- AISIX's intentional **120-second** policy and Squid remain unchanged. CPA production 45 and Sub2API production 16 remain unchanged; Sub2API upgrade stays paused. No account, DB, credential, resource or public-security change occurred.
- Paid provider path, public SSE/WS terminal/reconnect behavior and end-to-end timeout-policy removal remain untested in production. Overall OpenSpec integration tasks remain open for remaining components and independent closeout review.
