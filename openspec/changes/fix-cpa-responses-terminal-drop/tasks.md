## 1. Fix CPA

- [x] 1.1 Reproduce the dropped `response.completed` deterministically: build deployed fork commit `45a74cc343`, replay a recorded real upstream stream through it, and ablate the single `codex.client.keepalive` line.
- [x] 1.2 Add a regression test that fails on `45a74cc343` and upstream `dev`. Fix `responsesSSEStartsNewDataFrame` (fork commit `24af18c6`). Verify that `go test ./sdk/api/handlers/...`, vet and the server build pass, and that the replay delivers `response.completed` at zero delay and at the original timing. Record that `TestResponsesInterruptStopsHTTPUpstream` is flaky before the change as well.

## 2. Publish and activate

- [x] 2.1 Build the candidate image in fork CI (`ci-responses-sse-data-only-event-image`, `SOURCE_SHA=24af18c6`, handler tests). Record the GHCR digest, and verify the image labels and source revision with a read-only manifest/config inspection.
- [x] 2.2 In private-config, set `CLI_PROXY_IMAGE` to `ghcr.io/xz-dev/cli-proxy-api:<tag>@sha256:<digest>` and make the `cli-proxy-api` profile pull-enabled. Prove an image-only guarded preview for `component=cli-proxy-api` with no environment, resource or config delta. The operator approved going live (2026-11-15); confirm that no other component rollout is running.
- [x] 2.3 Apply CPA only. Verify the running image and executable identity, unchanged environment and resources (1g/2g, `GOMEMLIMIT=700MiB`), health, zero restarts and OOMs, and the protected neighbours.
- [x] 2.4 Send one real `xl/claude-opus-5-5` request through CPA that produces a large tool call longer than 36 s, and verify that `response.completed` arrives. Confirm that Sub2API logs no new `missing terminal event` for Pi traffic afterwards. Write a change record with rollback instructions, adopt the private pin, and pass `openspec validate --all --strict`.

## 3. Upstream

- [x] 3.1 After operator review, open issue #7579 and PR #7580 (`upstream/fix-responses-sse-data-only-event`, based on `dev`) on `router-for-me/CLIProxyAPI`.
- [ ] 3.2 Upstream closed #7579 as NOT_PLANNED (the provider's frame is non-standard). The operator kept PR #7580 open with the refinement and a reply. Until upstream merges an equivalent change, or the `xl` provider switches to SSE comment keepalives, the fork carries the patch; drop it on a later rebase only after one of those happens.

## 4. Review refinement (2026-11-16)

- [x] 4.1 Address the valid PR review finding: a typed payload followed by its own `event:` field (data-before-event order) must stay one frame. Fork commit `f38c87df` splits only when the `event:` name differs from the pending payload `type`. `TestResponsesSSEFramerKeepsTypedDataBeforeMatchingEvent` fails on `24af18c6` and passes now; handler tests and vet pass; the real-stream replay still delivers `response.completed`. Ported to the PR as `4173ec24`.
- [x] 4.2 Build `ci-f38c87df-responses-sse-data-only-event@sha256:56f5a1a0…` in fork CI. Apply it CPA-only through a fresh guarded preview, with operator approval to restart immediately. Verify the image, health, environment and a large-tool-call request again.
