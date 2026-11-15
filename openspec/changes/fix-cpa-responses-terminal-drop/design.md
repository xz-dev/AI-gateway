## Context

See proposal.md. The deployed CPA is the private, preload-only image `ci-45a74cc343-no-memory-admission` (manifest `sha256:b187c5e3…`), built offline from fork branch `fix/remove-cpa-memory-admission`, because GHCR rejected the existing token's write scope (docs/records/cpa-memory-gate-removal-2026-11-12.md). Current CPA resources come from the 2026-11-15 memory raise: 1g RAM / 2g memswap, `GOMEMLIMIT=700MiB`.

## Goals / Non-Goals

**Goals:** Make CPA deliver `response.completed` when the upstream stream contains a typed data-only event. Ship the fix as a reversible, CPA-only image change that keeps every other part of the deployed binary.

**Non-Goals:** Filter `codex.client.keepalive` in the executor, rework the framer to forward blank-line delimiters, rebase the fork onto upstream, change CPA config or resources, or change any other component.

## Decisions

1. Fix the framer generically rather than dropping the one known upstream event. `responsesSSEStartsNewDataFrame` already closes a pending data-only event when another `data:` line arrives; it now also closes it when an `event:` line arrives and the pending payload has a JSON `type`. Untyped data followed by `event:` stays one frame, which keeps `TestResponsesSSEFramerWaitsForEventFieldAfterData` valid. The function is shared with the images accumulator, whose tests pass unchanged.
2. Prove the bug in both a unit test and a replay. The regression test feeds the production chunk shape: event/data pairs, `: keep-alive` comments, one bare typed `data:` event, then the terminal. It fails on `45a74cc343` and on upstream `dev`, and passes with the fix, for both Codex and non-Codex clients. A local replay of a recorded real upstream stream (about 2.4k events) through a locally built CPA confirms the result: without the fix the stream ends after `output_item.done` on every run; with it `response.completed` is delivered at both zero delay and the original timing.
3. Base the fix on deployed `45a74cc343`, not on upstream `main`, so the binary carries no unrelated changes. Fork commit `24af18c6` changes one function (+14/−1) and adds one test file.
4. The operator chose a CI-built immutable image. The fork's existing candidate-workflow pattern builds it: branch `ci-responses-sse-data-only-event-image` pins `SOURCE_SHA=24af18c6`, runs `go test ./sdk/api/handlers/...`, builds the repository `Dockerfile` for linux/amd64 and pushes it to `ghcr.io/xz-dev/cli-proxy-api` using the workflow's own `GITHUB_TOKEN`, recording the digest. The deployment pins that reference by SHA-256 digest, and `cli-proxy-api` becomes a normal pull-enabled component profile instead of the 2026-11-12 private preload-only exception. The production host can already read this package's manifests. The flaky `TestResponsesInterruptStopsHTTPUpstream` (decision 2) is skipped in CI and is the only test skipped. If the pull fails, the offline archive path from 2026-11-12 stays available as a fallback, but only after the operator decides to use it.
5. Activation requires explicit operator approval after the guarded preview. Pick a quiet window, because recreating CPA interrupts in-flight streams. Do not run another component rollout, including the keepalive proxy change, at the same time.

## Risks / Trade-offs

- [An upstream emits typed data followed by its own `event:` line in one event, which SSE allows] → No known Responses/Codex emitter does this, and CPA already splits typed data from following `data:` lines. Existing framing, recovery, error and images tests pass.
- [CPA restart interrupts streams] → Use a low-traffic window, check established connections before the restart, and use the existing health gates.
- [Fork drift from upstream] → Submit the upstream PR. When upstream merges an equivalent fix, a later fork rebase can drop the local commit.
- [Known flaky test] → `TestResponsesInterruptStopsHTTPUpstream` fails intermittently on the unfixed base and on upstream `dev`. Record it, and do not treat it as a regression.

## Migration Plan

Regression test (red, then green) → package tests, vet and server build → local replay → build the image on the deployed base → verify archive and image identity → guarded CPA-only preview → operator approval → apply → verify image and executable identity, unchanged environment and resources, health, neighbours, one real `xl/claude-opus-5-5` large-tool-call request through CPA, and the absence of new `missing terminal event` errors from Sub2API → record. Rollback is a fresh `rollback.yml -e component=cli-proxy-api --check --diff` preview of the saved snapshot. The old image remains available, and restoring it brings back the terminal-drop bug.
