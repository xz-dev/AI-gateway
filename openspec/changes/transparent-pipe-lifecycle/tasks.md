## 1. SSE forwarding slice

- [x] 1.1 Prepare the SSE-only small plan and prove an established write remains backpressured beyond the old budget, with a genuine pre-change RED and bounded fixture cleanup.
- [x] 1.2 Remove the independent WS write-expiry wrapper/configuration, preserve bounded copying and real termination/cancellation, and pass the accepted stalled-write plus independent-session, heartbeat, close and shutdown regressions.
- [x] 1.3 Pass the existing full/race/static tests, obtain independent source/publication review, and verify exact-revision CI plus the immutable image's binary/source identity and isolated runtime contract.

## 2. Bridge forwarding slice

- [ ] 2.1 Record a minimal byte-lifetime/ordering plan and demonstrate real fixed-slot admission and independent-deadline RED cases; distinguish a small handshake test from 65 completed lightweight sessions.
- [ ] 2.2 Remove the turn-idle and text-write expiry decisions and fixed global socket refusal, preserve protocol ordering/backpressure and prompt cancellation, and pass the accepted local public-interface examples.
- [ ] 2.3 Pass full/race/resource tests and independent review; publish the exact approved source and immutable image with real final-revision CI and runtime evidence.

## 3. Keeper forwarding slice

- [ ] 3.1 Approve a bounded-buffer/spool and short-storage-lock plan that removes global materialization serialization without a replacement global mutex, unchecked allocations, a reservation deadlock or a renamed timeout.
- [ ] 3.2 Implement cancellation-aware independent processing, remove fixed global count refusals and autonomous phase/replay-write expiry, and verify more than 64 lightweight sessions, unrelated-session progress and prompt resource cleanup through the real entry points.
- [ ] 3.3 Pass existing storage/owner/pin/continuation/no-replay regressions, full/race tests and unchanged-envelope resource tests; obtain independent review and verify exact-source CI and immutable image acceptance.

## 4. Integration and staged rollout

- [ ] 4.1 Reconcile current gateway main and source gitlinks/pins without changing AISIX's 120-second policy, Squid, public authentication/security rules, container limits or the original user worktree; verify the exact integration diff and strict OpenSpec checks.
- [ ] 4.2 For each accepted image, create verified protected recovery material and a fresh capacity/identity-bound component preview; activate only that application with the reviewed digest and verify health, actual image, namespace, neighbours and cancellation/readback.
- [ ] 4.3 Adopt each completed private pin, prove final no-drift previews, and write sanitized source/CI/runtime/backup/rollback/behaviour receipts with independent closeout review and exact-final gateway CI.
- [ ] 4.4 Record APISIX's verified no-native-off limitation and any untested edge/provider behaviour explicitly; do not claim end-to-end policy cleanup or change proxy implementation/topology without the separate required decision.

### SSE-only rollout receipt (2026-11-03)

SSE subset of 4.1–4.3: functional gateway `e62a51f` CI passed; source `5ff3e338` / immutable image `b1236671…` passed accepted isolated resource/cancellation checks. The first approved private candidate still rendered the old literal image and did not deliver the source change. A selected-image-only correction, effective-image assertions, fresh preview and separately reviewed digest preceded the successful second apply. Exact live image/revision, health/no OOM/restarts, unchanged limits/namespace/47 neighbours, original two-file private adoption and final zero-drift preview passed. Both transaction snapshots remain. See [rollout record](../../../docs/records/sse-transparent-write-2026-11-03.md) for UTC windows, evidence and rollback distinctions.

These parent tasks stay unchecked: keeper/bridge deployment and independent closeout review remain open; exact-final documentation CI is verified separately in the closeout receipt. No production paid-provider/terminal/reconnect acceptance was run. Keeper 8s/global-1/64, bridge 30s/64 and APISIX's finite native timeout limitation remain unresolved; AISIX 120s and Squid are unchanged.
