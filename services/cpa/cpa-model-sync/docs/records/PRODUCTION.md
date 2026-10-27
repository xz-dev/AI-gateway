# Production attempt — rolled back

Date: 2026-09-06. Host: `<tailscale-ip>`, `/root/AI-gateway`.
The user authorized a minimal production cutover and observation. **Production
acceptance failed; the original deployment is restored.**

## Corrected-policy attempt — also rolled back

At 12:36 UTC, a separate protected baseline was created under
`/root/AI-gateway/.model-sync-filtered-cb312e5afa90/`; the first attempt's backups
were not overwritten. The original successful acceptance identity/input pipeline
was recovered from session evidence and reused. No alternate identity was tried.
Baseline checks returned 265 full records, 71 visible, and 4,535,123 bytes with
exact visible metadata, cache HIT, ETag/304 and opaque 404.

This attempt deployed the NIM three-model and ShuaiAPI Claude-only policies,
not `channels: {}`. The first summary at 12:36:59 UTC was **9 updated, 1 unchanged,
2 failed, 0 unconfirmed**. NIM remained at three; ShuaiAPI became thirteen.
The next naturally scheduled summary was **1 updated, 9 unchanged, 2 failed,
0 unconfirmed**. OpenRouter updated again; the reason for its changed inventory
was not captured, so this is not evidence of an all-channel zero-write round.
Both rounds still failed for `glm-coding` and `gmicloud`.

The four-component resource observer recorded no new OOM/restart in its measured
components, but the subsequent all-sibling check failed: **Sub2API restart count
increased from 1 to 3**. Bounded kernel-log inspection confirmed Sub2API cgroup
OOM kills at **12:40:20 and 12:41:14 UTC**. `State.OOMKilled=false` after restart
did not negate those events. Docker's retained event query was empty; it is not
negative evidence. Causality between the rollout and these OOMs remains unproved.

The candidate public-catalog load probe was not completed before this stop.
Local four-component budget results cannot substitute for that missing production
measurement or for real Sub2API coverage.

Rollback completed at approximately 12:52 UTC:

- Stopped Rust, restored old Go, then sequentially restored/read back `models`
  for nine changed channels. Used a fresh stopped-state comparison snapshot,
  not the stale first-round OpenRouter hash. No whole-config restoration.
- Restored original Compose, environment and inactive policy bytes; removed only
  the new stopped synchronizer and dedicated network. CPA/namespace unchanged.
- All 47 services running with original images and protected files restored.
  Sub2API's observed restart drift remains recorded; do not claim all 46 siblings
  had unchanged lifecycle state.
- Fresh rollback probe: **265 full / 71 visible / 4,535,123 bytes**, cache HIT,
  exact metadata, routing identity, ETag/304 and opaque 404 passed.

Evidence in that protected directory: `baseline.json`, `state-first.json`,
`resources-watch.json`, `sub2api-bounded-check.json`, `rollback-current.json`,
`models-rollback.json`, `rollback-final.json`, and `public-restored.json`.
The local [catalog report](CATALOG-LIMIT.md) separates policy correction, the
withdrawn 32 MiB experiment, and the remaining production blocker. Sub2API was
not reconfigured/repaired and no resource budget was raised. The subsequently
authorized [read-only OOM investigation](SUB2API-OOM.md) found large Responses
inference bodies near the kills, but did not establish allocation-level or rollout
causality; deployment remains blocked.

## Required ordering for any future retry

A retry needs a separate decision on the Sub2API blocker first. Start monitoring
all related services, including real Sub2API and CPA, before mutation. Immediately
after cutover, verify the candidate's real catalog load, authorization and worker/
process OOM deltas. Only after those pass may the natural scheduler observation
begin. Do not wait for a timer round before that immediate gate, and do not replace
whole-chain acceptance with a four-component fixture or surviving container master.

## Candidate identity and scope

- Rust source image: `localhost/cpa-model-sync:v0.1.0`, Podman ID
  `cb312e5afa90b4c28d355cc38ee852ce521a4156fea1f8ea1ed0f23f202faf13`.
- Loaded Docker ID:
  `f4f3f27e37f687f9765eedce3b3bca155790c0a109c82e7a7005d1da4fc6a9e1`.
- Rust executable SHA-256:
  `92ea8501cd771042aafa0a54ca9148624977f8950d229bf9080bef2a6d948f0e`.
- Go candidate: `localhost/models-enricher:membership-a43a985a8f23`, loaded Docker
  ID `7bebd1de279e712e7ddc2120d4599e574b28f7dce84996095503fdb775543ea1`.
- Go executable SHA-256:
  `a43a985a8f231531357131dec2a6fafb04ed46e3b08006df9b73d23c6cf9f9b3`.

Docker import changed image IDs. Root filesystem layers, entrypoint, command,
environment, user, working directory, labels and extracted executable hashes were
verified against the local candidates. The transfer archive checksum matched.
Production `compose.override.yaml` was included in effective-config comparison.
Only the Go image selector, new synchronizer and its direct internal network were
changed. CPA and its network namespace were not recreated.

## First attempt: observed first synchronization

At `2026-09-06T10:50:39Z`: **10 updated, 0 unchanged, 2 failed, 0 unconfirmed**.

| Prefix | Disabled | Models before → after | Outcome |
| --- | --- | --- | --- |
| axis | yes | 4 → 14 | updated |
| xl | no | 2 → 22 | updated |
| openrouter | yes | 0 → 431 | updated |
| zcode | no | 10 → 10 | replaced model records |
| glm-coding | yes | 9 → 9 | upstream request failed; preserved |
| ollama-cloud | no | 17 → 19 | updated |
| nim | no | 3 → 81 | updated |
| congee | yes | 0 → 5 | updated |
| shuaiapi | no | 2 → 21 | updated |
| commandcode | no | 0 → 67 | updated |
| zakk | yes | 20 → 18 | updated |
| gmicloud | no | 1 → 1 | upstream request failed; preserved |

Non-model fields, disabled flags, unmanaged configuration and all 46 other
containers' identities/start times remained unchanged. No default route was added.
First post-round Rust sample: RSS 8,004 KiB, process high-water RSS 11,728 KiB,
zero restarts and no OOM. This is one production sample, not a multi-round resource
acceptance result.

The causes of the `glm-coding` and `gmicloud` fetch failures were **not established**.
They are not classified as pre-existing problems or successful synchronization.

## Failed downstream acceptance

Before cutover, the full catalog had 265 records / 4,534,595 bytes. The existing
credential had 92 entitlement IDs and 71 visible records. Exact metadata,
authorization intersection, cache HIT, ETag/304 and opaque empty 404 passed.

After switching to the new Go image, the authenticated front returned **502**.
APISIX emitted:

```text
model-list intersection failed: response body exceeds limit
```

The original-catalog access logs recorded approximately **17.8 MB** responses
(e.g. APISIX-models 17,808,857 bytes), exceeding the front's **16 MiB** response
limit. Restoring only the old Go image did not remove the failure: approximately
17.8 MB responses and 502 persisted with the synchronized CPA model configuration.
Restoring the prior models then restored the original catalog and passing checks.

This evidence identifies the production boundary violated by the model inventory
expansion. It does not justify raising the limit, dropping unknown metadata or
changing the model policy without a separate decision. Local Rust-to-CPA and
fixed downstream fixtures did not establish this combined production acceptance.

## Completed rollback

1. Stopped the synchronizer.
2. Restored the original `models-enricher:layered-eac910bbfb2b` image.
3. Verified no external model drift against the first-round snapshot, then restored
   only `models` for the 10 modified channels using sequential Management PATCHes.
   Each replacement was read back. No whole CPA configuration restore was used.
4. Verified all CPA configuration was semantically restored, including non-model
   fields and unmanaged kinds (absent/empty models both represent zero configured
   models).
5. Restored original Compose and environment bytes; removed only the new stopped
   synchronizer container and its new network. Retained candidate/rollback images,
   the inert policy file and protected deployment receipts/backups.

Final verification: 47 original services running with original images; other 46
container IDs/start times unchanged; no new OOM or restart. Sub2API's restart count
of one was already present in the baseline and did not increase. CPA was never
recreated. No new default route remains.

Final public catalog: **265 full / 71 visible**, 4,534,595 bytes. Exact records,
routing IDs, output-limit aliases, authorization, cache HIT, ETag/304 and empty 404
all passed again. The second natural sync round was **not observed** because the
rollout was stopped for the regression. No production convergence claim is made.

## Receipts and next boundary

- Local sanitized receipts and operation scripts: `/tmp/cpa-sync-deploy.YnAHzK/`.
- Protected remote backups/receipts:
  `/root/AI-gateway/.model-sync-cb312e5afa90/`.
- First snapshot: `state-first.json`; rollback: `models-rollback.json` and
  `rollback-final.json`; final public check: `public-rollback-models.json`.

The candidate remains local-validation evidence, not production accepted. Resolving
the real catalog-size regression requires a new, bounded implementation/acceptance
decision before another rollout. No limit changes, metadata truncation, unrelated
repairs, commits or pushes were made during this attempt.
