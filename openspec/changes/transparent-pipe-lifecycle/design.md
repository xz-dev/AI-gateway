## Context

The owner confirmed the acceptance examples in the proposal and delta specs. Existing deployments use SSE proxy `1c489390927a986b7aacd3ebdfef3ea33f51d15d`, bridge `83bb9819a5afbda0dbf0c6c7af998d92de82adae`, and keeper `b49ad9ac6c1eb65b1785d66ecb7249413d4c82bf`. These are the source baselines, not an instruction to discard newer published work. Each source branch must reconcile relevant upstream changes before publication.

## Goals / Non-Goals

**Goals:** remove duplicated lifecycle/count policy from pure pipes; keep bounded streaming/backpressure, cancellation and storage/continuation integrity; deliver one small source slice at a time.

**Non-Goals:** change AISIX's intended 120-second upstream-read policy, Squid, public authentication/security policy, container resource limits, upstream business/pool settings, or topology. No paid inference or synthetic production stress. CPA upgrade and paused Sub2API maintenance remain separate.

## Decisions

1. **SSE write forwarding is the first independent slice.** Replace the established-WebSocket custom timed Write wrapper with existing bounded streaming writes and real connection termination/cancellation. Remove its obsolete runtime setting and update its existing tests/docs. Retain heartbeat cadence/probe accounting, connect-only handshake protection, HTTP/SSE keepalive behavior, faithful close propagation and shutdown grace. A blocked destination must not grow an unbounded queue or prevent an independent session progressing.
2. **Bridge is a separate slice.** Remove its independent turn-idle and text-frame-write expiry decisions and fixed global socket-count rejection. Preserve per-connection Responses ordering and backpressure; do not drop extra valid creates as a substitute for sequencing. Inspect byte reservations before modifying admission so requests cannot deadlock while holding input/collector reservations and waiting for output scratch.
3. **Keeper requires removal of the global processing gate, not a renamed gate.** A global mutex would retain cross-session head-of-line blocking, and an uncancellable mutex wait would regress cancellation. Keep only necessary short storage-atomicity locks. Parse/rewrite and retained-output lifetimes must be bounded using existing streaming/spooling or cancellation-aware byte backpressure without reservation cycles. GOMEMLIMIT is a soft GC target, not permission for unchecked allocations or OOM. Remove both the eight-second phase wait and replay/write deadlines; do not preserve them under new names. The observed long phase holder remains unknown; compaction is a possible path, not a proven incident trigger.
4. **Do not fake APISIX timeout removal.** Exact 3.19.0 configuration schema rejects zero and omission inherits a finite default. A large finite value is not disabled. This change may prepare verified limitations, but configuration or proxy-engine/topology changes beyond supported native semantics require a separate decision. APISIX investigation does not block the independent self-owned source slices, and its unresolved deadline prevents claiming whole-path timeout removal.
5. **Publish only independently reviewed exact revisions.** Use dedicated maintenance refs, full-source binary/OCI stamping and immutable image identities. Reuse fail-closed publication patterns: existing tags and auth/network/unknown registry outcomes refuse publication. No main push, tag overwrite, hidden test exclusions or retry-until-green. Run the existing full relevant suites and focused cancellation/resource checks; preserve original failure evidence.

## Risks / Trade-offs

- Removing write expiry can leave a genuinely stalled peer connected until a real close/cancel → retain bounded buffers, prove cancellation/shutdown unblocks both legs, and measure resources in isolated fixtures.
- Removing a fixed count can expose retained-buffer costs → a real 65-plus lightweight-session test is necessary but not sufficient; resource/concurrency regression must stay within the unchanged container envelope. Do not substitute cgroup OOM for application memory safety.
- Existing tests encode the old timeout/rejection policy → replace only those contradictory expectations with accepted public behavior, preserve unrelated validation, and demonstrate pre-change RED for the corresponding new behavior.
- Mock failures can hang cleanup or pass the wrong criterion → register release/cleanup before assertions, bound test execution, check every I/O error, and distinguish handshake admission from completed-turn acceptance.
- Store metadata and continuation pins are stateful → preserve atomicity/owner boundaries, no replay, old-image recovery material and consistent protected backups.

## Delivery Plan

For each repository: record its small implementation plan, demonstrate the unmet accepted example locally, implement the minimum slice, run full/race/resource tests as appropriate, obtain independent review, then publish the source and image under existing consent. The first slice is SSE; bridge and keeper proceed only when their specific resource/ordering plans are sound. Source completion does not imply deployment.

For integration: preserve concurrent gateway main changes, public defaults and private desired state as distinct surfaces; remove only obsolete pipe settings and update accepted pins/gitlinks. Validate OpenSpec and final-revision gateway CI. For each production application, refresh backup/capacity/identity evidence, use a new guarded component preview/digest, recreate only the selected application, and verify image, namespace, neighbours, cancellation and controller no-drift. A failed backup/capacity gate stops activation without deleting recovery material or lowering the gate. Record untested public/real-provider behavior explicitly.

SSE rollout note (2026-11-03): the first private candidate changed an unused `.env` image variable while private Compose still selected an old literal. The controller's successful transaction therefore did not deliver the new image. Effective-image assertions, a selected-literal-only correction and a newly reviewed identity-bound digest preceded the corrected rollout. Exact live image/revision, unchanged limits/47 neighbours, two-file private adoption and zero drift then passed; see [SSE rollout record](../../../docs/records/sse-transparent-write-2026-11-03.md). This receipt covers SSE only, not keeper/bridge or overall integration completion.
