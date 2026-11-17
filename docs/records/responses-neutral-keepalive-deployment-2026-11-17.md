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

A read-only observer samples every 10 minutes for 6 hours from 02:01 UTC. It counts Responses requests through the proxy (and those past 10 s / 25 s, i.e. heartbeat-eligible), plus Bot log `creation_order`, `lifecycle_identity`, `missing_completed` and `memory update failed` events since activation. Acceptance: zero creation-order or lifecycle-identity `RuntimeError`s, with heartbeat-eligible Bot traffic actually observed. Missing-`response.completed` errors count separately as CPA/provider faults. The result will be added below; until then, task 5.4 stays open.

## Rollback

Use the guarded path only: `playbooks/rollback.yml -e component=ai-sse-keepalive-proxy --check --diff`, then apply with its own fresh approval digest. Rolling back restores the known heartbeat defect, so it must be recorded as recovery to the old baseline, not as a fix.
