## Why

The deployed Sub2API source retains Responses WebSocket keepalive patches even though the front bridge now converts those turns to HTTP before Sub2API. Refresh the upstream baseline and remove that unused downstream maintenance without losing rerank or changing the gateway's existing behavior contracts.

## What Changes

- Consume upstream Sub2API v0.2.19, the latest stable release checked on 2026-11-02, with only necessary downstream fixes.
- Retain rerank, including its existing provider adapters, account controls, usage handling and pricing additions.
- Apply the separately approved minimum frontend security updates: Vue I18n 11.4.14 and its matching Intlify packages, decode-uri-component 0.5.1 and follow-redirects 1.16.2. Verify compatibility without adding audit exceptions or new libraries.
- Retain explicit-session propagation and error-rule field preservation unless the selected upstream revision demonstrably supplies equivalent behavior.
- Remove the two downstream Responses WebSocket keepalive commits only after confirming the live front-bridge/account-mode invariant.
- Publish a source-traceable immutable image in the existing owned fork, update gateway image pins and deploy only Sub2API through the existing guarded transaction.
- Preserve account/group/pool settings, retry policy, network topology, other components and persisted data, except the explicitly approved upstream migration effects below. On 2026-11-02 the operator separately approved official migrations 246/247/248 after isolated validation; this includes migration 246 enabling image generation for matching Gemini/Antigravity groups.

## Capabilities

### New Capabilities

None. This is release maintenance; `.openspec.yaml` sets `skip_specs: true`.

### Modified Capabilities

None. Existing session-affinity, client-error, routing and front-bridge requirements remain authoritative. Characterization and release checks establish compatibility rather than inventing new product behavior.

## Impact

- Sub2API source changes belong to its separately authorized repository and isolated maintenance workspace, not this gateway change's edit root. Only owned-fork publication is authorized; no upstream PR or unrelated branch work is included.
- Gateway-owned changes: `compose.yaml`, `.env.example`, this change's artifacts, a scoped `PLAN/` entry and a sanitized deployment record.
- Deployment inputs: ignored controller configuration and a Sub2API-only image pin change. Do not commit private configuration or make additional business-setting edits; only the approved upstream migrations may change persisted schema/state.
- Production disruption is limited to Sub2API application recreation; PostgreSQL, Redis, front-chain services, AISIX and CPA remain running.
- An unrelated existing untracked deployment record and untracked source-repository tool directories remain untouched.
