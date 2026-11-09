# Converted Chat capacity errors: compatibility candidate

## Scope

CI compatibility lock only; no deployment, production calls, production resource changes, or nightly-workflow edits. The success-code-neutral fix preserves named inner 413/503/507 envelopes before converted Chat retry/failover handling. Memory floors and allocation factors are unchanged.

## Published source and image

- Reviewed source: `02918ea957f0639db932b23e7e5d00a6f2298308` (five production lines, existing approved regression tests).
- Fork branch: `xz-dev/sub2api:ci/chat-capacity-errors`.
- Published tip: `9d3b7060075245f5d11f9a8d62ab566a16288661`; cherry-picked source: `b98eceaf40b03453f834f4f5d8418d82063c6bf4`.
- Effective files match the approved source exactly except the existing GHCR workflow. The workflow retains M2's Linux/amd64 build arguments and labels the approved source, while checking the exact cherry-picked parent.
- Green build: https://github.com/xz-dev/sub2api/actions/runs/42660300885
- Before: `ghcr.io/xz-dev/sub2api@sha256:340273920f7f2abf3a6eb23fd1593fcc6206d60a4b113850ef43f31c8a232160`
- After: `ghcr.io/xz-dev/sub2api@sha256:56557012dc75e96feb22e26fdb36fce057ab1e660cba8f907bf4b3d85472472a`
- Anonymous digest pull succeeded. Image readback: Linux/amd64, `org.opencontainers.image.revision=02918ea957f0639db932b23e7e5d00a6f2298308`.

Exactly one of the eight locked image references changes: Sub2API. Keeper, bridge, AISIX, CPA, PostgreSQL, Redis, and mock base retain their previous digests. All locked memory limits, CPU quotas, assertions, margins, and declared charge factors remain unchanged.

## Isolated error integration

The published digest ran at 512 MiB, no swap, with the original 288 MiB `GOMEMLIMIT` and unchanged capacity configuration. A separate internal-only container network hosted PostgreSQL, Redis, and a synthetic fixture server; no provider network was reachable.

Twenty cases passed: 18 named-inner cases returned exact status/body and the allowed upstream request-ID header after **one** synthetic upstream attempt; two ordinary provider 503 cases retained **two-account** failover. Both streaming flags were exercised. Fixtures reuse the five approved handler envelopes plus four approved service envelopes with meaningful `limit_bytes=169328640`. The ordinary negative retains the approved no-hop provider body. Each synthetic Chat request was observed converted to `/v1/responses` with `input`, not `messages`; capability-probe requests were recorded separately and excluded from user-request attempt counts. The request-ID header coexists with the application's own request-ID header.

## Fresh green ladder and generated lock

- Fresh main baseline/harness: `36608b1` in an independent scratch clone; no historical harness checkout was modified.
- Run: `20261109T171054Z-97154`; verdict: **pass**, 10/10 rungs, zero container restarts.
- HTTP/WebSocket 1, 16, 64, and 256 MiB plus HTTP small/tiny 16 MiB all passed the unchanged verdict implementation, including existing body fidelity, ticket/privacy, OOM, and memory-margin assertions.
- Existing `ci/lock_from_manifest.py` generated the lock from this run's actual manifest, summary, and results, using the new digest. Only source-method annotations were added afterwards; no verdict was relabelled or borrowed from an old run.
- Manifest SHA256: `51c414c7b06674871eb37a789ccf52de1a46ebd6d0044c57ab6ad5f6b660d466`.
- Summary SHA256: `9672722ce5bb13f130ea6cb0340cd687aa5e36e8b816e2c262283fefd870e9e0`.

### Local no-swap runtime correction

Local `podman-compose 1.6.0` omitted the declared `memswap_limit`; preflight detected 512 MiB memory with 1 GiB combined memory/swap. No test verdict was accepted under that configuration. A separately approved, scratch-only wrapper added `--memory-swap=<memory>` at creation, scoped to the two uniquely named throwaway projects and their Compose project labels. No system, production, source, or tracked harness settings changed.

Wrapper SHA256: `c06422093799a87cc0e37193bf62439addadc78c3c7a56172d1e9f212eaab19f`.

The enriched manifests record the wrapper hash, effective creation argv, and pre-test cgroup readbacks. Every resource-limited service had `MemorySwap=Memory` and actual `memory.swap.max=0` before testing:

| Service | Ladder memory MiB | CPU quota |
| --- | ---: | ---: |
| keeper | 384 | 0.5 |
| bridge | 256 | 0.5 |
| sub2api | 320 | 1.5 |
| aisix | 512 | 1.0 |
| cpa | 768 | 1.75 |
| pg | 400 | 1.0 |
| redis | 80 | 0.5 |

Integration used Sub2API 512 MiB instead of the ladder's unchanged 320 MiB. The streaming mock remains resource-unbounded, as in the original harness; it is not a tested gateway hop.

Private local evidence is retained under `/var/tmp/sub2api-chat-errors-evidence`, `/var/tmp/sub2api-chat-errors-integration`, and `/var/tmp/sub2api-chat-errors-ladder`. Raw argv, network details, and runtime logs are deliberately not committed.

## Limits and next gate

The PR ladder must independently pass before merge. The local no-swap result does **not** establish hosted-runner swap enforcement: the existing Compose-provider omission remains a CI-environment risk until hosted cgroup readback proves otherwise. No broad workflow fix is included here. Nightly 1 GiB testing is deliberately unchanged and was not rerun for this error-only patch. This compatibility candidate does not authorize deployment or production resource changes.
