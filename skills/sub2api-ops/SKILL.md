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

## Verified endpoints

The Sub2API admin API contract is version-specific. Only the error-passthrough
rule set is documented locally (`docs/sub2api-error-passthrough-configuration.md`,
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
