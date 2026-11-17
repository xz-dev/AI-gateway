# Responses neutral keepalive — production activation — 2026-11-17

Change: `fix-responses-neutral-keepalive` section 5. Publication evidence: [2026-11-16 record](responses-neutral-keepalive-publication-2026-11-16.md). The user approved a single-component window and the acceptance scope "real Bot path + log metric". The public-ingress slow-first-byte canary (5.3) was **not** approved and stays pending; no temporary route was added.

## Before

- Live: `ai-sse-keepalive-proxy` image `sha256:b12366714e08463765266ec60a1f402d045febd4955edcbef590ddffc2bf20ec` (revision `5ff3e338`), healthy. `HEADER_WAIT=10s`, `IDLE_INTERVAL=15s`.
- Read-only component preview with unchanged private desired state: `config_diff: false`, 47 protected containers, writer lock not held.
- Bot log baseline (fixed SDK error strings only; no message bodies): every `RuntimeError` on the Responses path was `Expected to have received `response.created` before `response.in_progress``: 24 before the 2026-11-16 Bot cutover and 16 after it (through 2026-11-17 01:47 UTC), all logged by `aibot.context` as `memory update failed`. No missing-`response.completed` failures were present. This confirms that the Bot's residual memory-update failures were this proxy defect.

## Change

Private desired state only: the keepalive image reference in the private `.env` (the effective value) and the private Compose default, both rewritten in place to `ghcr.io/xz-dev/ai-sse-keepalive-proxy:09fb8deb7b0f1aa79978ec845ac12e77b828e6fe@sha256:6d3daaf1c23f15595b83c5c67c59985ced207bbfb6f8a49b65a741f47b0949fa`. Private pre-change copies are kept beside them with mode 0600. No other field changed.

Candidate preview (`playbooks/deploy.yml -e component=ai-sse-keepalive-proxy --check --diff`): `config_diff: true`, `environment_changes: []`, image proof `digest_verified: true` from the registry manifest, `compose.override.yaml` unchanged, recreate argv `up -d --force-recreate --no-deps --no-build --pull never ai-sse-keepalive-proxy`, readiness `docker_healthcheck`, 47 protected containers, approval digest `4f11d26c…c799e`.

## Activation

- Established connections on the shared keepalive namespace, sampled 3× at 10 s (01:56:51–01:57:11 UTC): 3, 3 and 4 on `:8080` (16–22 in the namespace). This was a low-traffic moment.
- Apply with the reviewed digest under a persistent owner and the documented 30-minute bound: 01:57:31–01:59:19 UTC, `failed=0`, `changed=9`. The image was pulled and its digest verified before activation. Container started 01:58:40 UTC, and readiness polling passed once Docker reported `healthy`. Private rollback snapshot: `.ai-ops-state/front-ai-sse-keepalive-proxy-*` on the host.
- Interruption: recreation cut in-flight streams through the proxy (a few at the time of sampling). The exact count is not measured.
- Readback: image `sha256:6d3daaf1…49fa`, OCI revision `09fb8deb…`, `HEADER_WAIT=10s`, `IDLE_INTERVAL=15s`, `LISTEN_ADDR=:8080`, `GOMEMLIMIT=96MiB`, health `healthy`, restarts 0, no panic/fatal log lines, 47 protected containers unchanged, same namespace owner.

## Observation

A read-only observer completed 37 snapshots approximately every 10 minutes, from 02:01:09 to 08:03:53 UTC; its completion marker is present. It counts Responses requests visible in the proxy's retained logs, those lasting at least 10 s / 25 s, and Bot log `creation_order`, `lifecycle_identity`, `missing_completed` and `memory update failed` events since activation. Request duration alone does **not** establish a heartbeat: a long request may keep producing data without a startup or idle gap. Acceptance requires zero creation-order or lifecycle-identity `RuntimeError`s together with evidence of a real Bot stream traversing a heartbeat interval. Missing-`response.completed` errors are counted separately; the counter does not establish their cause.

### Final evidence through 08:03:53 UTC

- Window: 02:01:09–08:03:53 UTC (**6 h 2 min 44 s**), 37 valid JSON snapshots and a `DONE` completion marker. Gateway reads succeeded in 36 snapshots (one failure at 06:33:02); Bot reads succeeded in 36 snapshots (one failure at 07:53:48). These are sampled retained-log observations, not gap-free telemetry.
- Every successful gateway sample reported the expected image `sha256:6d3daaf1…49fa`, healthy, zero restarts and no panic/fatal lines in the retained log view. This is readiness evidence, not proof of successful upstream answers.
- Bot counters remained at **0 creation-order**, **0 lifecycle-identity**, and **0 detected missing-terminal RuntimeErrors** in every successful sample. The final successful sample counted eight ERROR events, including four `memory update failed` events, unchanged from the interim cutoff. A separate sanitized read earlier in the window classified those eight as `openai.RateLimitError`: four foreground reply failures and four memory-update failures. These are not the creation-order signature fixed here; the counters do not cover every warning or establish overall upstream health.
- At 06:43:16 the visible proxy log view contained 751 Responses requests, 35 lasting at least 10 s and nine at least 25 s. Subsequent views fell to 12 requests at 06:53 and one at 07:03; the final view contained 16 requests, one at least 10 s and one at least 25 s. Therefore these are **not reliable lifetime cumulative counters**; retained-log loss or rotation is a possible explanation, not independently verified. Do not sum the snapshots or treat later lower values as negative traffic.
- The proxy serves other clients as well as the Bot. These duration counts neither identify the Bot nor show when headers or real events arrived. Most early requests were rejected with upstream 429s; absence of the SDK error during rejection-only traffic does not exercise the repaired heartbeat path.

**Disposition:** no recurrence of the fixed signature was detected in the sampled Bot logs, but task **5.4 remains pending**: a correlated successful Bot stream spanning an actual startup/idle heartbeat, plus the remaining selected-client checks, has not been demonstrated. Task 3.2 (selected Codex/Hermes versions) and task 5.3 (public slow-first-byte canary) also remain pending. No provider switch, new route, or additional production action is authorized by this observation record.

## Rollback

Use the guarded path only: `playbooks/rollback.yml -e component=ai-sse-keepalive-proxy --check --diff`, then apply with its own fresh approval digest. Rolling back restores the known heartbeat defect, so it must be recorded as recovery to the old baseline, not as a fix.
