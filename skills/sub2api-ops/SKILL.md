---
name: sub2api-ops
description: Read and minimally update Sub2API account/group/pool settings through the supported admin API, with read-back verification and no direct DB access.
---

# Sub2API operator workflow

Use this when the owner asks to inspect or change Sub2API business state
(accounts, groups, model mappings, pool/retry settings). Ansible owns image
deployment only; business settings belong to the panel/API.

## Rules

1. **Inspect first, propose before writing.** Default action is read-only.
   Present exact account identities, the owned fields, and recovery limits,
   then wait for explicit approval.
2. **Fresh read immediately before update.** Pull the latest complete resource;
   confirm the reviewed baseline still matches. If a nested object (e.g.
   `credentials`) is replaced by the API, rebuild it from the latest complete
   representation — never assume partial PUT merging.
3. **Minimal update.** Change only the approved fields. Preserve credentials,
   peer pool/retry settings, and any field not in the approved set.
4. **Read back.** Verify the intended values after the write. On a timed-out
   write, READ the resource before retrying — never blind-retry a possibly
   completed mutation.
5. **No SQL fallback.** The historical doc mentions direct DB writes as an
   emergency option; this skill does not use it. Failed authentication stops
   the task — ask the operator to authenticate.
6. **Secrets stay out of the conversation.** Never echo tokens, raw credential
   objects, or secret-bearing commands into durable output.

## Front-bridge account invariants

When creating or editing an OpenAI API-key account, include these fields in the
reviewed change and read them back after saving:

- While the front keeper and `responses-ws-sse-bridge` are live, set or keep
  `extra.openai_apikey_responses_websockets_v2_mode: "off"`. WebSocket terminates
  before Sub2API; the bridge sends HTTP POST/SSE. Sub2API `http_bridge` drops
  assistant/reasoning history, and `passthrough`/`ctx_pool` do not provide this
  topology's continuation contract. The tested passthrough path rewrites frames
  and leaves the socket unusable after a request-scoped error. Read the
  [WS decision](../../docs/websocket-termination.md#decision) before changing modes.
- On keeper-path accounts, disable the separate `extra.openai_passthrough` flag.
  Its error handler bypasses error-passthrough rules and masks 4xx diagnostics as
  `Upstream request failed`. Preserve native 400 error codes such as
  `previous_response_not_found`; [the error-rule configuration](../../docs/sub2api-error-passthrough-configuration.md)
  explains why a broad 400 rule is wrong. This flag is not a WebSocket mode.

For existing accounts outside the approved edit, report violations and obtain
approval rather than silently changing them. Preserve unrelated `extra` fields.

## Verified endpoints

The Sub2API admin API contract is version-specific. Only the error-passthrough
rule set is documented locally ([configuration guide](../../docs/sub2api-error-passthrough-configuration.md),
method B). Account/group/mapping endpoint paths, request/response envelopes, and
PUT replacement semantics are **pending verification against the deployed
version** — confirm them from the live admin API (e.g. `/api/v1/admin/...`)
before first use, and record the verified shapes in this file or the private
operations notes before automating writes.

## Fixture

`fixture/api_double.py` is a local double demonstrating the
inspect → propose → approve → fresh-read → minimal-update → read-back flow,
including lost-response read-before-retry. Run:

```bash
python3 skills/sub2api-ops/fixture/api_double.py
```
