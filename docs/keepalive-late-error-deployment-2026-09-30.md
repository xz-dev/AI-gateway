# Keepalive late-error deployment — 2026-09-30

## Result

The reviewed late-error fix is deployed. Only `ai-sse-keepalive-proxy` was replaced; the other 48 container identities, running states, restart counts and OOM flags were unchanged. All 49 services were running after deployment.

This makes quota/credit failures more informative. **It does not replenish GLM quota or repair non-streaming 103 forwarding through Cloudflare Tunnel.**

## Artifact and scope

- Functional commit: [`3abe475`](https://github.com/xz-dev/ai-sse-keepalive-proxy/commit/3abe475f9524dd4be2a77cf86fd896cc4095d7b7).
- Published source revision: `bd38e50f9fc09e1c446d3573b31cd54ecec583df`.
- Image: `ghcr.io/xz-dev/ai-sse-keepalive-proxy:bd38e50f9fc09e1c446d3573b31cd54ecec583df@sha256:c4af69df946aebaefe284df54245158fd1db624b8186e03b2919844cbeb2c5c5`.
- Observed Docker image ID: `sha256:c4af69df946aebaefe284df54245158fd1db624b8186e03b2919844cbeb2c5c5`.
- Container started: `2026-09-30T10:40:19.883226317Z`; health `healthy`.
- Retained settings: `HEADER_WAIT=30s`, `IDLE_INTERVAL=15s`, `WS_PING_INTERVAL=15s`. **Superseded 2026-10-01:** `HEADER_WAIT=30s` made Cloudflare reset slow `Accept: application/json` streams; production now uses 10s ([details](keepalive-header-wait-10s-deployment-2026-10-01.md)).
- Production private `.env` changed only the keepalive image selector. Compose bytes were unchanged. Credentials, routing, other images and Cloudflare settings were not changed.
- Changes were committed directly to `main` and pushed; no feature branch or GPG signature was required by the operator.

After HTTP 200 is committed, complete identity-encoded JSON errors up to 64 KiB for 400/404/409/422/429 preserve the upstream message and supported string code/type fields. Internal statuses, malformed/oversized/incomplete bodies and transport failures remain generic. Error-body acquisition runs asynchronously; idle heartbeat and cancellation handling remain active.

## Checks

- New regressions failed on the initial candidate, then passed after repair. They cover stalled body/cancellation, capped prefixes, incomplete response, compressed body rejection and unrelated JSON field types.
- Local `go vet ./...` and `go test -race -count=1 ./...` passed. New regressions repeated 15 times with the race detector also passed.
- Independent re-review closed all three initial findings and returned `OK with notes`. The residual note is that the incomplete-body regression exercises the declared-length guard; the below-cap read-error branch was correct by inspection but did not receive an additional dedicated regression.
- [Keepalive CI](https://github.com/xz-dev/ai-sse-keepalive-proxy/actions/runs/36702403943) passed tests on Go 1.26.x and published both image architectures.
- Exact published image passed a loopback-only synthetic-origin canary before activation: late 400 and 429 carried the intended message/code, 403 stayed generic, a stalled JSON body retained seven heartbeat frames, and `HEADER_WAIT=30s` preserved a fast HTTP 400 response. The old image failed the same late-message assertion.
- One initial canary run encountered reuse of a just-removed temporary container name; the fixture was corrected to use distinct names per run, then all cases passed. This was not a product-code change.
- Canary origin and containers were temporary, loopback-only and removed. No model inference, API keys or production routing changes were needed.

Deployment used `ansible-playbook ops.yml --tags deploy-ws-keepalive`: read-only plan showed only the image-selector change; apply bound the exact digest and source revision, then verified the recreated container and protected neighbours. Apply recap: `failed=0`, `rescued=0`.

Post-activation inspection independently confirmed the exact image/revision, health and retained settings, matched remote `.env` to private desired state, and compared all 49 container records against the pre-activation snapshot.

## Limits and rollback

No live Pi/Codex inference or retry-classification test was performed. The image fixture verifies gateway behavior, not recovery of exhausted GLM accounts. HTTP 200 cannot be changed after commitment; a late error is necessarily an in-stream failure.

Rollback, if needed: restore the previous keepalive selector in private desired state and run the same scoped Ansible plan/apply with its digest/revision. Keep the current `HEADER_WAIT` (10s since 2026-10-01, see [the follow-up](keepalive-header-wait-10s-deployment-2026-10-01.md); do not restore 30s):

```text
ghcr.io/xz-dev/ai-sse-keepalive-proxy:639eca292db8ba056b2a2e339209b3326cf77683@sha256:76c8d6e534e62d361eb24e519cb5bf4afcacdb14b07069001e413b040c2607b0
```

The existing Ansible pre-deploy copy and sanitized receipts remain under the private deployment's `.ai-ops-state/`. No rollback was required.

See [the Cloudflare Tunnel investigation](cloudflare-tunnel-103-diagnosis-2026-09-30.md) for the confirmed 103 loss boundary, canary limitations and Enterprise configuration options.
