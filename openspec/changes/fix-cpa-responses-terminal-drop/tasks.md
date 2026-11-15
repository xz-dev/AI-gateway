## 1. Fix CPA

- [x] 1.1 Reproduce the dropped `response.completed` deterministically: build deployed fork commit `45a74cc343`, replay a recorded real upstream stream through it, and ablate the single `codex.client.keepalive` line.
- [x] 1.2 Add a regression test that fails on `45a74cc343` and upstream `dev`. Fix `responsesSSEStartsNewDataFrame` (fork commit `24af18c6`). Verify that `go test ./sdk/api/handlers/...`, vet and the server build pass, and that the replay delivers `response.completed` at zero delay and at the original timing. Record that `TestResponsesInterruptStopsHTTPUpstream` is flaky before the change as well.

## 2. Publish and activate

- [ ] 2.1 Build the fixed CPA binary from `24af18c6` with a compatible toolchain and build mode. Replace only the server binary in the deployed runtime image, and label the source provenance. Export the archive, transfer it, verify the controller and host SHA-256 and the loaded image ID, and keep the private preload-only profile.
- [ ] 2.2 Snapshot the live inputs. Prove an image-only guarded preview for `component=cli-proxy-api` with no environment, resource or config delta. Get explicit operator approval, choose a quiet window, and confirm that no other component rollout is running.
- [ ] 2.3 Apply CPA only. Verify the running image and executable identity, unchanged environment and resources (1g/2g, `GOMEMLIMIT=700MiB`), health, zero restarts and OOMs, and the protected neighbours.
- [ ] 2.4 Send one real `xl/claude-opus-5-5` request through CPA that produces a large tool call longer than 36 s, and verify that `response.completed` arrives. Confirm that Sub2API logs no new `missing terminal event` for Pi traffic afterwards. Write a change record with rollback instructions, adopt the private pin, and pass `openspec validate --all --strict`.

## 3. Upstream

- [ ] 3.1 After operator review, open the issue and the PR (`upstream/fix-responses-sse-data-only-event`, based on `dev`) on `router-for-me/CLIProxyAPI`. Track the merge so a later fork rebase can drop the local commit.
