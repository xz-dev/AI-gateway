# CPA Responses terminal-drop fix (2026-11-15)

OpenSpec: `openspec/changes/fix-cpa-responses-terminal-drop/`. Upstream: router-for-me/CLIProxyAPI#7579 (issue), #7580 (PR against `dev`).

## Symptom and cause

Pi requests to `claude-opus-5-5` failed with `upstream_error: Upstream request failed`. Sub2API logged `openai.forward_failed` with `stream usage incomplete: missing terminal event` for account CPA → credential `codex-2` (prefix `xl`). The old host showed the same failure 100 times on 2026-11-12, so the migration did not cause it.

When the `xl` upstream sends no data for about 30 s, it emits one data-only SSE event, `data: {"type":"codex.client.keepalive"}`, with no `event:` line. Claude holds back large tool-call argument deltas, so turns that write large files hit this threshold. CPA's Responses SSE framer receives one line per chunk and never sees blank-line delimiters. The data-only event shifted event/data pairing for the rest of the stream: the final `event: response.completed` was paired with the `output_item.done` payload, and the real `response.completed` was dropped. CPA logged a normal 200.

## Evidence

- The upstream sent `response.completed` on every direct request: over HTTP SSE and over WebSocket, from both the new and the old egress.
- A CPA built locally from deployed fork commit `45a74cc343` dropped `response.completed` on every replay of a recorded upstream stream, at zero delay and at the original timing. Removing only the keepalive line fixed the replay. The result was the same with and without Codex client headers. Upstream `main`/`dev` have the same bug.
- Regression test `TestResponsesSSEFramerDataOnlyEventKeepsTerminal` fails without the fix and passes with it. `go test ./sdk/api/handlers/...`, vet and the build pass. `TestResponsesInterruptStopsHTTPUpstream` is flaky on the unfixed base as well and is skipped in the candidate CI.

## Fix and image

- Fork commit `24af18c62e039bff4d20a0362e657b89aab2c6ae` on branch `fix/responses-sse-data-only-event`, based on `45a74cc343`. `responsesSSEStartsNewDataFrame` now closes a pending, typed data-only event when an `event:` line arrives.
- Fork CI branch `ci-responses-sse-data-only-event-image` (commit `7305eb7a`, run 43720795944) ran the handler tests and published
  `ghcr.io/xz-dev/cli-proxy-api:ci-24af18c6-responses-sse-data-only-event@sha256:2d14956b7378a98a8c41c244fd510094f4cfafdf195f62d7935d69ddc6114734`.
  The image labels carry revision `24af18c6…`. The package allows anonymous pulls, and the production host could read its manifest.

## Rollout

- Private config: `CLI_PROXY_IMAGE` now pins the digest above; the previous offline pin is kept as a commented rollback line. The `cli-proxy-api` component profile changed from preload-only (`pull: false`) to pull-enabled. No other environment, resource or config change.
- Baseline preview before the edit: `activation_required=false`, `config_diff=false`, 69 ok / 0 changed / 0 failed.
- Candidate preview: image-only change, `environment_changes=[]`, image proof `registry_manifest` with `digest_verified=true`, 47 protected containers, writer lock free. The approval digest was `7ccd21ee64830597fb88b7ed2abde9b75e2608f3dfbce85221ae28aaaf18cbe7`.
- The operator approved going live. Before the restart, 7–8 connections to CPA :8317 were established.
- Apply: 13:56:02–13:57:23 UTC, 152 ok / 9 changed / 0 failed. CPA was recreated at 13:57:09 UTC. Readback matched image ID `sha256:2d14956b…`. All 47 protected containers were unchanged. The private snapshot is `.ai-ops-state/front-cli-proxy-api-i5mkt_3d`, with `front-cli-proxy-api-last.json` as the rollback reference.

## Acceptance

- CPA healthy, restart count 0, no OOM, `GOMEMLIMIT=700MiB`, memory limit 1 GiB (unchanged).
- The same large-tool-call request through CPA (`xl/claude-opus-5-5`, about 17 KB of arguments, no Codex headers), which failed before, returned `response.completed` after 51.4 s.
- After the deployment, Sub2API logged no `missing terminal event` and no `forward_failed`.

## Observed interruption

As planned, the recreate interrupted in-flight CPA streams. Around 13:57:08 UTC, key 27 (`gpt-6.1-sol`, Sub2API account 10 → CPA `codex`) logged one `unexpected EOF` at 186.6 s, ten `502 (failover)` at 127–157 s and fourteen `no available OpenAI accounts` while CPA restarted. The operator's long-context test was running at the time and saw these as timeouts. They are deployment casualties, not a context-size limit: in the same 10 h, key 27 completed request bodies up to about 660 KB.

## Rollback

Run a fresh `playbooks/rollback.yml -e component=cli-proxy-api --check --diff` and approve its own digest. The previous image (`ci-45a74cc343-no-memory-admission`, manifest `sha256:b187c5e3…`) is still present on the host. A rollback restores the terminal-drop bug and requires restoring the preload-only profile line in private config.
