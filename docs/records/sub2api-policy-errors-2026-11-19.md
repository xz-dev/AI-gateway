# Sub2API request-scoped policy errors — 2026-11-19

## Status

Candidate source, publication identity and the local compatibility ladder are verified.
Gateway integration review and the guarded production rollout are pending. This
record does not yet claim production activation or policy end-to-end acceptance.

The owner approved this repair on 2026-11-19. Scope: preserve a finite known
content-policy classification across Responses and Chat before account retry,
failover, cooldown, disabling or health penalties. No account/group setting,
APISIX 401/404 opacity, deployment script or unrelated component change is approved.

## Source and behavior

- Production baseline: `f249326936c2d76e90e2e2fc67869c01564d62d6`, image `e26d4b7d…`.
- Reviewed source: [`ede9e5ff12bdf186c80b523ad21d4a71ccda20bc`](https://github.com/xz-dev/sub2api/commit/ede9e5ff12bdf186c80b523ad21d4a71ccda20bc), branch `fix/preserve-policy-errors-20261119`.
- Publisher: [`c1a4759682ee24b3e22b6677d4ced62479476360`](https://github.com/xz-dev/sub2api/commit/c1a4759682ee24b3e22b6677d4ced62479476360), whose parent is that reviewed source. It adds only the existing guarded `ghcr-ci.yml` recipe with this branch and exact source SHA inputs.
- Published and registry-verified image: `ghcr.io/xz-dev/sub2api@sha256:8f835a12151d4af9d88db206b8a5b84f0c7aa6a6c582f54bd16d8bcf2cba11d3`.
- Platform `linux/amd64`, revision label = reviewed source, version `ci-ede9e5ff12`; local image ID `56cd20443f8b04b421ad0bee9fa5f45a1a5aa097c659f4176d0b28d490f8b72d`.

For HTTP 400/403, the shared classifier recognizes only structured
`content_filter`, `content_policy`, `content_policy_violation` and
`content_moderation`. Unknown/conflicting codes, credential/endpoint 403 and the
existing dedicated `cyber_policy` path retain their established handling. A safe
provided type is preserved; missing/unsafe type gets the documented policy-specific
fallback. The fixed client explanation is `Request blocked by upstream content policy.`
No arbitrary upstream message or unknown classification value is reflected.

Before commitment, Responses and Chat return JSON with the original 400/403 and
safe `error.code/type/message`. After a committed stream, the protocol-correct
terminal error carries those fields and keeps HTTP 200. It does not retry or emit
synthetic success. The new HTTP-status classifier does not reclassify arbitrary
policy-looking text in an otherwise successful upstream SSE stream.

## Tests and independent source review

The synthetic mock matrix covers 400/403 × Responses/Chat, with retries and an
alternate account available. It asserts exact safe code/type and fixed message,
one upstream attempt, zero account-state/health-side-effect calls and no reflected
provider detail. Negatives cover missing/unsafe fields, unknown/conflicting codes,
credential 403 and non-policy compatibility retries; existing cyber/default behavior
is retained.

Initial candidate `733cccc9` received REQUEST-CHANGES. Independent mock overlays
found two P2 omissions: passthrough field normalization could replay policy before
classification, and committed-stream writers lost or rewrote the safe type.
`ede9e5ff` fixes both and adds formal regressions. Independent re-review was
APPROVE-WITH-NOTES; the parent also reran both original overlays successfully.
The normative specification was not weakened to accept merely non-empty fields.

Full changed handler/service packages, focused race tests, vet and the required
source CI checks passed. A wider local race run has seven failures reproduced on
the unchanged baseline; it is not reported as wholly green. No real policy content
or production credential was used for these tests.

## CI and the single platform-capacity exception

| Check | Exact commit | Result |
| --- | --- | --- |
| [Source CI 44565397352](https://github.com/xz-dev/sub2api/actions/runs/44565397352) | `ede9e5ff` | PASS on attempt 2, including actual macOS shell execution; unit/integration, lint, frontend and release helpers passed |
| [Source Security Scan 44565397378](https://github.com/xz-dev/sub2api/actions/runs/44565397378) | `ede9e5ff` | PASS |
| [Publisher CI 44565406545](https://github.com/xz-dev/sub2api/actions/runs/44565406545) | `c1a47596` | Overall FAILURE: duplicate macOS shell cancelled before execution; other four jobs passed |
| [Publisher Security Scan 44565406911](https://github.com/xz-dev/sub2api/actions/runs/44565406911) | `c1a47596` | PASS |
| [Image 44565406630](https://github.com/xz-dev/sub2api/actions/runs/44565406630) | `c1a47596` | PASS; immutable digest independently verified from registry |

**Capacity waiver: owner 委托的监工 01a1ef13 批准，2026-11-19.** It applies only
to publisher run `44565406545`, attempt 2, shell job
[`135721115932`](https://github.com/xz-dev/sub2api/actions/runs/44565406545/job/135721115932).
It remains recorded as **cancelled, 0 steps**, never success. GitHub's annotation:
`The job was not acquired by Runner of type hosted even after multiple attempts`.
Notices also identified macOS arm64 capacity constraints. No third retry was made.

Equivalent successful execution is source job
[`135721078754`](https://github.com/xz-dev/sub2api/actions/runs/44565397352/job/135721078754):
`Check deployment scripts` passed on an actual `macos-15` hosted runner,
16:55:40–16:55:55 UTC. `git diff --stat ede9e5ff c1a47596` shows only
`.github/workflows/ghcr-ci.yml` (+65). Both commits have identical `deploy/` tree
`49d90809f489e170ccac101ed2809134852dcc51` and identical backend-CI workflow blob
`751f1fb0fd0c283354f3b77da4aeacdfebc15a86`. Thus the checked scripts and their
job definition are byte-identical. Any later code/deployment-script change requires
full CI again. This does not waive registry verification, ladder/lock, gateway
review, guarded apply, acceptance or archive requirements.

## Mock compatibility ladder and lock

Run `20261119T173020Z-2408968`, 17:30:20–17:32:27 UTC, ran once and passed all ten
default rungs with a 256 MiB cap; this is not a nightly or production run. Only the
Sub2API immutable image changed. All eight fixture containers had zero restarts;
all rungs had `offlist=0` (mock-only upstreams).

- Clean harness: `0b5b5d3ce10776e903c7e0596d35afd39a728252`.
- Unchanged project-scoped no-swap wrapper: `c06422093799a87cc0e37193bf62439addadc78c3c7a56172d1e9f212eaab19f`.
- Manifest SHA256: `d089f32b2d5c89bd626bdece2bc89960cad82bc39e7b4b790a705d693575601d`.
- Summary SHA256: `29afcf72de4a20edc580809b655327e1c078ea2bd7e8c68836861b0303637f89`.
- Generated lock SHA256: `8bb5425f3a472f50adc1af22b3d58c299fc31a9acc32455a2dcc0e712c3b7933`.

The existing generator produced the new lock from this run's passing receipt.
Resource/footprint/inline/fallback limits and the other seven image inputs are
unchanged. Effective cgroup limits were read before tests; memory swap was zero
for the seven bounded services. The streaming mock remains intentionally unbounded,
as in the unchanged harness. Cleanup removed the fixture; its ports are free.

Sub2API peak was 93.7 MiB against its 320 MiB limit (previous lock: 80.6 MiB).
Keeper reached its unchanged 384 MiB peak, as in earlier locks; the existing OOM
and margin assertions passed. These are observed local measurements, not causal
performance claims or a refresh of the nightly lock.

Private controller evidence is retained under
`/var/tmp/ai-gateway-policy-20261119/`: `publication.json`, registry inspection,
source/review test logs, the annotated and pre-annotation manifests, summary,
runtime readbacks, generated lock and the original ladder log. No private config,
address, key or token is included in this record.

## Gateway integration validation

The candidate lock and the `SUB2API_IMAGE` defaults in `.env.example` and
`compose.yaml` are promoted together; the service README points to this record.
Template validation, the standard-cache topology/lock guards, full strict OpenSpec
validation and `git diff --check` passed locally. The ignored private Compose
override was moved into protected scratch only for the foreground template check
and restored byte-identical on exit.

The first local template invocation used restrictive umask 077, which made the
egresstest's copied Unbound configuration unreadable to its non-root process.
The unchanged fixture passed under its normal umask 022; no product source,
permission policy or acceptance assertion was changed. Both logs are retained.

## Guarded rollout, acceptance and rollback

**Pending.** The intended window changes only the Sub2API image through the existing
approved-digest component transaction. Business settings, APISIX opacity and all
neighbouring service identities must remain unchanged. Readiness and zero-token
observations are separate from local mock policy acceptance; no real policy content,
new key or temporary production route is permitted.

Rollback target: `ghcr.io/xz-dev/sub2api@sha256:e26d4b7dbef57fb55ee4e635a7010edd319245ead53265d5b4563e2e31f68f39`
(source `f249326936`). The source delta has no migrations; database rollback is
not part of this image-only change. Actual activation time, interruption, protected
snapshot, readback, zero-token outcomes and final drift remain to be recorded.
