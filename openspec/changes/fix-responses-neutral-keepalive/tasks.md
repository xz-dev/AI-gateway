## 1. Lock the baseline and add failing regressions

- [x] 1.1 Start an isolated proxy repair branch from the verified production `5ff3e338` maintenance line, preserving the deployed transparent-WebSocket-write fixes; verify the exact source ancestry, live-image delta, and publication workflow before making changes, rather than starting from the older gateway gitlink.
- [x] 1.2 Update/extend the existing Go startup and idle cases for all three Responses paths to require comment heartbeats, unchanged valid upstream creation/completion events, prompt flushing with delayed headers or first body, and no fabricated progress; verify the neutral-heartbeat assertions fail on the baseline.
- [x] 1.3 Add a bounded real-HTTP SDK regression using the actual proxy and a controlled valid upstream in a task-scoped OpenAI Python 3.49.0/httpx2 2.16.0 environment; verify delayed startup reproduces the exact missing-creation RuntimeError on the baseline, while the same valid stream without the fabricated heartbeat parses successfully. Include post-creation idle silence followed by successful completion.
- [x] 1.4 Extend or retain assertions for terminal suppression/trailing bytes, cancellation, safe late errors, fast responses, background passthrough, non-stream HTTP, other API protocols, and WebSockets; verify they describe the production maintenance baseline and do not silently accept an unrelated behavior change.

## 2. Repair and document the heartbeat

- [x] 2.1 Change the shared Responses heartbeat case to reuse the existing SSE comment representation for startup and idle output, without new configuration or changes to timers, flushing, provider events, or terminal/error policy; verify the Go and SDK regressions from section 1 pass.
- [x] 2.2 Update the proxy README's Responses behavior and startup/idle explanation, and correct the already-stale Chat heartbeat row to match its existing implementation; verify documentation distinguishes transport liveness from model progress and leaves other protocol behavior unchanged.
- [x] 2.3 Wire the bounded SDK regression into the applicable source and maintenance publication CI using pinned test-only dependencies; verify it runs before image publication and the production Docker image gains no Python/runtime dependency.

## 3. Verify the complete candidate

- [x] 3.1 Run `test -z "$(gofmt -l .)"`, `go vet ./...`, `go test -race ./... -count=1`, `go build ./...`, and the existing maintenance immutable-tag/backpressure checks; verify all pass on the exact candidate revision and retain the red/green SDK evidence.
- [ ] 3.2 Check the deployed Bot SDK and selected deployed Codex/Hermes Responses client versions with comment-only startup and post-creation idle intervals; verify completion and cancellation, record application-event timeout behavior separately, and leave unavailable or failing client acceptance pending rather than restoring synthetic progress.
- [x] 3.3 Independently review the full candidate against the live `5ff3e338` baseline, the delta spec, and existing WebSocket/error guarantees; verify no mandatory findings remain, no SDK error is merely swallowed, and existing maintenance fixes are retained.

## 4. Publish and integrate without activating production

- [x] 4.1 Publish the reviewed source through the existing maintenance-line workflow and its immutable-tag guard; verify exact-revision CI, full-SHA image availability, platform/digest identity, and OCI revision, without replacing any existing SHA tag or using `latest` as evidence.
- [x] 4.2 Update the AI-gateway proxy gitlink and immutable image reference to the published source/image pair, preserving Compose topology; verify `python3 ops/scripts/tests/test-standard-cache-topology.py`, the repository's applicable validation gate, `git diff --check`, and `openspec validate fix-responses-neutral-keepalive --strict` pass, with no unrelated pin or private-config changes.
- [x] 4.3 Record the source commit, image digest, baseline comparison, SDK/Go results, and pending live checks in a sanitized release record; verify publication is not labeled deployment or end-to-end acceptance and no private address, hostname, credential, or application payload is committed.

## 5. Deploy and accept only after separate production approval

- [ ] 5.1 Obtain an explicit low-traffic single-component window and approved canary scope; preview `playbooks/deploy.yml -e component=ai-sse-keepalive-proxy --check --diff` from `ops/ansible/`, then verify the approved digest, renderer/capacity/drift gates, exact pre-pulled image, protected rollback files, and proxy-only recreation plan. If approval is absent, leave this section pending.
- [ ] 5.2 Apply through that reviewed component transaction only; verify the running image/revision and unchanged heartbeat settings, health, unrelated container IDs, and recorded interruption. On failure use the existing freshly previewed/approved rollback and record restoration to the old defective baseline honestly.
- [ ] 5.3 Run the authorized bounded public-ingress canary with `Accept: application/json`, a valid first real event delayed beyond both the startup wait and prior approximately 30 s first-byte window, and a real SDK consumer; verify timely flushed comments, subsequent real creation/terminal events, and no creation-order exception. Do not add unapproved production routes, replay group messages, or substitute a fast probe for this evidence.
- [ ] 5.4 Verify the real Bot path and selected-client acceptance after activation, and observe a documented bounded window for recurrence; record non-secret event timings and outcomes separately from readiness, keep missing checks pending, and distinguish unrelated primary-connectivity or CPA missing-terminal failures from this repair.
