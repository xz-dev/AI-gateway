# Responses neutral keepalive — source and image published, not deployed — 2026-11-16

OpenSpec change: `fix-responses-neutral-keepalive`. This record covers sections 1–4 (source, image, gateway integration). **Nothing was deployed.** Production desired state still pins the defective previous image; activation (section 5) needs a separately approved window.

## Defect

The keepalive proxy wrote `data: {"type":"response.in_progress"}` as its Responses startup and idle heartbeat. The OpenAI Python SDK rejects a Responses event before the real `response.created`, and rejects a type-only progress event after creation, so otherwise valid responses failed:

- before creation: `RuntimeError: Expected to have received `response.created` before `response.in_progress``
- after creation (idle): `RuntimeError: Lifecycle response identity changed.`

The upstream does not need to lose any event for this to happen. Chat already used an SSE comment.

## Fix

The Responses heartbeat now reuses the existing SSE comment `: keepalive` for startup and idle. Timers (`HEADER_WAIT`, `IDLE_INTERVAL`), flushing, terminal detection, late-error handling, background/resume passthrough, non-stream HTTP, Anthropic and WebSocket behavior are unchanged. Comments signal transport liveness only, never response state or model progress.

| | Previous live | Candidate |
|---|---|---|
| Source | `5ff3e338b1ed301ce72832b9e06893e9975dbd51` (maintenance line) | `09fb8deb7b0f1aa79978ec845ac12e77b828e6fe` (fast-forward of the same line) |
| Image | `sha256:b12366714e08463765266ec60a1f402d045febd4955edcbef590ddffc2bf20ec` | `sha256:6d3daaf1c23f15595b83c5c67c59985ced207bbfb6f8a49b65a741f47b0949fa` |

The candidate is based on the running maintenance revision, not the older integration gitlink `1c489390`, so the deployed WebSocket backpressure fixes are retained. Changed proxy files: `main.go` (one heartbeat case, one comment), `main_test.go`, `README.md`, `test/sdk/`, both workflows. `Dockerfile`, `websocket.go`, `go.mod`/`go.sum` and the guard scripts are unchanged; the image contains no Python.

## Verification

- **Red on the previous source:** 21 Go (sub)tests fail; the real-SDK regression (openai 3.49.0 / httpx2 2.16.0, the Bot's pair) reproduces both errors above on `/v1/responses`, `/responses` and `/backend-api/codex/responses`. The same valid stream without the heartbeat parses (ablation). Reverting only the fix makes the same tests fail again.
- **Green on the candidate:** `gofmt`, `go vet`, `go test -race ./... -count=1` (timing-sensitive tests also repeated 3×), `go build`, immutable-tag guard tests 9/9, the workflow source-scope check, real-SDK regression 3/3, `git diff --check`. Re-run independently on a fresh clone.
- **Independent review:** approved with explicit residual risk, no mandatory findings.
- **Bot-shaped client check (local):** comment heartbeats reset the SDK transport read timeout during startup and post-creation silence (the same silence without the proxy times out); a client cancel during comment-only idle closes the upstream request in about 0.9 s.
- **Publication:** maintenance workflow run `43864076939` on the exact source — source scope, race suite, guard tests and the real-SDK regression passed before the image job; the immutable-tag guard refused overwrite; digest and runtime contract validated. Registry read-back: single `linux/amd64` manifest, OCI revision `09fb8deb…`, user `65534:65534`, entrypoint `/ai-sse-keepalive-proxy`. The previous full-SHA tag still resolves to its original digest.

## Gateway integration

- Proxy gitlink `1c489390` → `09fb8deb`.
- Public template pins (`compose.yaml` default and `.env.example`) → `ghcr.io/xz-dev/ai-sse-keepalive-proxy:09fb8deb7b0f1aa79978ec845ac12e77b828e6fe@sha256:6d3daaf1c23f15595b83c5c67c59985ced207bbfb6f8a49b65a741f47b0949fa`.
- `docs/security.md` no longer calls the heartbeats parser-visible.
- **Private desired state was not changed.** It still pins `5ff3e338…@sha256:b1236671…`, so a component deploy or recovery run recreates the defective image until section 5 is approved and applied.

## Pending

- Section 3.2: Codex/Hermes client compatibility with comment-only silence — not available to test; pending.
- Section 5: approved window, `component=ai-sse-keepalive-proxy` preview/apply, public-ingress canary with `Accept: application/json` and a first real event delayed past the startup threshold and the ~30 s window ([2026-10-01 record](keepalive-header-wait-10s-deployment-2026-10-01.md)), real Bot path and bounded observation. Suggested acceptance signal: zero creation-order `RuntimeError`s in the Bot log during the window, counted separately from missing-`response.completed` (CPA) failures.
- Pre-existing, unrelated: the proxy README says AI-gateway builds the image locally and does not depend on GHCR; the gateway consumes pinned GHCR images. Correcting it needs a new proxy revision and image.
