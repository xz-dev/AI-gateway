## Why

Pi sessions using `claude-opus-5-5` keep failing with `upstream_error: Upstream request failed`. Sub2API logs `openai.forward_failed` with `stream usage incomplete: missing terminal event`. The request path is Sub2API account CPA → CPA Codex API-key credential `codex-2` (prefix `xl`). The same failure appears 100 times on the old host on 2026-11-12, so it is not caused by the LXC migration.

Read-only diagnosis and a local replay on 2026-11-15 found the cause in CPA itself:

- When the `xl` upstream has sent no data for about 30 s, it sends one data-only SSE event, `data: {"type":"codex.client.keepalive"}`, with no `event:` line. Claude holds back large tool-call argument deltas, so turns that write large files cross that threshold. Turns that stream continuously do not.
- CPA's Responses SSE framer (`sdk/api/handlers/openai/openai_responses_handlers.go`) receives one line per chunk and never sees blank-line delimiters. The data-only event shifts event/data pairing for the rest of the stream. The final `event: response.completed` is attached to the `output_item.done` payload, and the framer then drops the real `response.completed` silently. CPA logs a normal 200.
- The upstream sends `response.completed` on every direct request, over both HTTP SSE and WebSocket and from both egress paths. A CPA built from deployed fork commit `45a74cc343` drops it on every replay of the recorded stream. Removing only the keepalive line fixes the replay. Upstream `main`/`dev` have the same bug, and no upstream issue or PR exists yet.

## What Changes

- Fix CPA's Responses SSE framer: an `event:` line closes a pending, complete, typed data-only event before starting the next event. Untyped data keeps waiting for its `event:` field, as before. The fix is fork commit `24af18c6` on branch `fix/responses-sse-data-only-event`, based on deployed `45a74cc343`, and includes a regression test.
- Build an immutable CPA image from `24af18c6` in fork CI, publish it to GHCR pinned by digest, and roll out only the CPA component through the guarded component rollout. This replaces the private preload-only exception for CPA with a normal pull-enabled profile.
- Report the bug upstream: issue router-for-me/CLIProxyAPI#7579 and PR #7580 against `dev`, submitted after operator review. They are separate from this deployment.

Out of scope: CPA config, routing, accounts, upstream credentials, Sub2API, AISIX and resource limits; the `fix-responses-neutral-keepalive` change, which is owned by another session.

## Capabilities

### New Capabilities

- `cpa-responses-stream-integrity`: CPA delivers the upstream Responses terminal event and keeps event/data pairing intact when the upstream stream contains data-only events.

### Modified Capabilities

None.

## Impact

CPA fork `sdk/api/handlers/openai` framer and tests; CPA image reference and private deployment inputs; operational verification and rollback evidence. There are no new dependencies and no config, account or group changes. Restarting CPA can interrupt in-flight CPA requests.
