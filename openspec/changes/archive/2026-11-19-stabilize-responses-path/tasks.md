> **SUPERSEDED — 2026-10-23.** This is a historical task ledger, not an active
> deployment queue. Unchecked items stay unchecked; surviving acceptance work
> is tracked separately. Do not execute or sync old topology tasks.
> See [SUPERSEDED.md](SUPERSEDED.md).

> **Administrative archive — 2026-11-19.** Superseded; owner 2026-11-19
> approved archiving with `--skip-specs`. This does not complete the three
> unchecked historical acceptance tasks. The inner `/rk` keeper, keeper-side
> routing and last-known-good topology are retired; their deltas must not be
> synchronized into the current main specs or redeployed.

## 0. Preconditions

- [x] 0.1 Confirm the keeper streaming-memory core (PLAN/keeper-oom.md) is accepted and deployed, and record its keeper commit and image digest as this change's base. Verify that `docker inspect` of production keeper shows that digest and that the named cache volume is mounted.
- [x] 0.2 Record the production baseline: 24 h counts of 127-byte selector 503s, keeper `ws previous=false/true`, apisix requests with upstream `127.0.0.1:9080`, and container IDs of all services. Verify the numbers are saved in `verification.md`.

## 1. A — Last-known-good fallback in the apisix selector

- [x] 1.1 Add a bounded shared dict (`routing_lkg`, 1 MiB) to `apisix-models/config.yaml`. Verify that `apisix test` or a container start succeeds and `ngx.shared.routing_lkg` is non-nil.
- [x] 1.2 Update `routing_selector.lua`:
  - store `cpa`/`aisix` decisions with a 15 min TTL;
  - delete the entry on `not_found`/`unavailable`;
  - on transport failure, use the stored decision, or return 503 when none exists;
  - log one warning per fallback use, with model and age only.

  Verify that `routing_selector_test.lua` covers: fresh fallback, expired fallback, no entry, `not_found` and `unavailable` override, and malformed body treated as a transport failure.
- [x] 1.3 Run the Lua tests in the apisix image. Verify all pass and that the existing four-outcome tests are unchanged.

## 2. B — Keeper classifies and forwards directly

- [x] 2.1 Add the routing client to keeper (`ROUTING_INDEX_URL`, 300 ms/300 ms/1 s budget, four-outcome and generation validation identical to Lua). Verify with Go unit tests against the shared fixture of index bodies, including malformed bodies, non-200 responses, timeouts, and generation 0.
- [x] 2.2 Add the last-known-good store to keeper (4096 entries, 15 min, same rules as 1.2). Verify with unit tests that mirror 1.2 and with a race test under concurrent lookups.
- [x] 2.3 Add the upstream map (`UPSTREAM_CPA_URL`, `UPSTREAM_AISIX_URL`, `CPA_API_KEY`) while keeping single-`UPSTREAM_URL` mode. Verify that config validation rejects partial configuration, and that existing tests pass in single mode.
- [x] 2.4 Classify the top-level `model` during request preparation (HTTP and each WebSocket turn), before any upstream request or history access. Map `not_found` to 404 and `unavailable`/no-fallback to 503, in protocol shape: JSON error, SSE error event, and WebSocket error frame with the socket left open. Verify with tests for each transport, including two WebSocket turns that route to different backends.
- [x] 2.5 Credentials: use the CPA key for CPA and the original `Authorization` for AISIX. The owner is always derived from the original `Authorization`, and credentials never appear in logs. Verify with a test where two callers on CPA cannot continue each other's IDs, a test that AISIX receives the caller header, and a log grep for sentinel keys.
- [x] 2.6 Strip top-level `prompt_cache_retention` from HTTP bodies and WebSocket frames in the streaming writer. Verify the upstream body lacks the field and otherwise equals the accepted bridge writer output, with other top-level values keeping their original encoding (golden test). Operator decision 2026-10-19: keep existing bridge semantics rather than literal original-wire bytes.
- [x] 2.7 Run `go test -race ./...`, `go vet`, and the existing memory-only capacity workload once, unchanged. Verify there is no regression from the keeper-oom acceptance numbers.

Implementation evidence for 2.1–2.5/2.7 is the isolated candidate `/var/tmp/keeper-route-b/source`, not canonical or deployed keeper. Review and routed capacity receipts are recorded in `verification.md`. Task 2.6 is accepted against the operator-selected bridge-writer contract; reviewer evidence is in `verification.md`.

## 3. B — apisix and compose wiring

- [x] 3.1 Update `apisix-models/apisix.yaml`:
  - `/v1/responses` and `/rk/v1/responses` (GET+POST, websocket) → `response-keeper`, with no body plugins;
  - remove `/v1/responses` from `catalog-routing-selector`;
  - keep `/rk/*` passthrough for other paths;
  - set the Responses route body limit to 32 MiB.

  Verify with a config render plus a local apisix: no Responses request reaches `self`, and a 20 MiB body reaches the fake keeper.
- [x] 3.2 Update the keeper service in `compose.yaml` (public example) and `ansible/private-config/compose.yaml`:
  - routing and upstream environment, `CPA_API_KEY`;
  - named volume `response-keeper-cache:/var/tmp/response-keeper`, `STORE_DIR`, `GOMEMLIMIT`;
  - keep `read_only`, `pids_limit: 64` and `mem_limit`;
  - update the stale "loops back to 9080" comments.

  Verify that `docker compose config` shows only keeper and apisix-models changed (diff against the baseline render).
- [x] 3.3 Local integration with the compose subset (apisix-models, keeper, fake CPA, fake AISIX, fake enricher). Verify:
  - single traversal per turn, from access logs;
  - continuation across HTTP and WebSocket;
  - owner isolation;
  - enricher paused → fallback used, then 503 after expiry or cold start;
  - `not_found` gives 404 on HTTP and a WebSocket error frame;
  - 16–32 MiB bodies are accepted.

## 4. D — Pi extension cross-socket continuation

- [x] 4.1 Add a per-identity anchor map, written when a turn completes with store acknowledged, and use it to seed new connections in `acquire()`. Clear it on cooldown, on a recovery probe, and on rejection. Verify with a test where an idle close and reconnect makes the first frame carry `previous_response_id` plus delta, and the notification reports `WS/continuation`.
- [x] 4.2 Treat `previous_response_not_found` on a bridge-owned anchor as the one-shot full-history repair signal. Verify that exactly one retry follows, that it does not repeat for 5xx errors, partial output or auth errors, and that a different credential never reuses an anchor.
- [x] 4.3 Run the extension test suite (`bun test` / existing harness). Verify all pass, including the durable-provider and checkpoint tests.

## 5. Review

- [x] 5.1 Independent review of the keeper routing, credential and owner changes, the apisix routes and the Lua fallback against the specs. Verify there are no P1/P2 findings open, and that minor findings are recorded as follow-ups.
- [ ] 5.2 Independent review of the paired native/plugin continuation changes, including actual supported-host propagation. Verify there are no open P1/P2 findings in the agreed scope, and record the explicitly accepted unmarked-legacy limitation.

## Acceptance follow-up status — 2026-10-21

- **#49 offline disk-full/slow-disk acceptance remains open.** A narrow real-ENOSPC cache-commit rollback passed; corrected full-disk wire fixture self-check and delayed-filesystem mounting remain blocked. The unthrottled control is not slow-disk evidence. See `verification.md` for preserved failed runs, source identity and limits.
- **5.2 remains unchecked.** Paired policy is now approved: minimal future ancestry fact, existing legacy behavior with disclosed residual risk, and existing summary-plus-retained-context rebase. Targeted review `044df86d` closed design gate B1 only. Two isolated `gpt-6.1-sol` implementation lanes (`5380d983`) returned immutable component handoffs under `/var/tmp/pi-continuation-p2-20261021-ulsoYs/CONTRACT.md`; parent verified both patches and all 16 changed-file hashes. Native normal check is blocked by 11 unmodified catalog-fixture errors (#59); plugin reports 397/397 offline passes. Fresh paired code review `88c271fb` rejected both v1 candidates: R1 (P1, both) permits ordinary user/local ancestry reset; R2 (P2, native) blocks the existing bounded explicit connection-limit recovery on memory references. Corrective workflow `a26cae0d` failed at its 45-minute outer timeout: plugin completed, native stopped after exporting patch/manifest/logs. Parent recovered both v2 candidates with all 16 source hashes and patch identities matching; no new writer or completed-work rerun. Plugin reports normal offline 403/403; native focused/protected tests are green but the same full-check blocker remains. Focused re-review `c447b74a` closed both R1/R2 with 398 passing bounded component invocations; both exact v2 components approved with explicit residual risk. #57 is complete at component scope; #56 stays open for #59. #58 starts actual supported-host diagnosis from the now-satisfied immutable-handoff/component-review gate, not from overall #56 completion. Initial lane `f13b8460` stopped on an invalid graph-tool enum before host tests; parent verified corrected same-MCP call and resumed `eb3fa236`. One compression/lineage overlap has a narrowly authorized host-copy resolution; no host acceptance yet. A concrete actual compaction red is now frozen on the still-documented unpatched compression host: old projection retains negative ancestry/responseId, so plugin conservatively refuses (no third frame/HTTP). #60 checks existing public structured APIs before any adapter or explicit compatibility decision; follow-up `5b67057b` also runs independent outer-repair checks. #60 public capability is now proved through existing hooks/projection (17 handoff hashes checked); original plugin owner `e58c94cc` implements the narrow adapter without new contract/version floor. Independent B diagnostics found a new #61 unsafe host outer retry: one native/corrected/auto/checkpoints-off prompt sent frame4 after accepted partial repair; the completed frozen matrix is RED in all 12 faithful host/terminal/policy combinations, with partial persisted and no HTTP escape. Agent execution later failed with Connection error, but parent read its durable report and verified 145 evidence hashes; no matrix rerun. #60 implementation separately failed before runtime edits and resumes once as `74e766f9` on the same model/protocol. Host propagation, outer lifecycle safety and complete replay/publication acceptance remain unproved. Actual host propagation remains unproved, so no paired acceptance claimed. Parent tasks #56/#57 cover components, #58 requires both durable handoffs, actual host/plugin propagation, protected checks, descendant/full replay and final review.
- **#52 directory acceptance remains open.** Read-only diagnosis and parent offline evidence checks are complete, but not a repair. Both basic and original/catalog legs have slow examples; the dominant 404 cohort matches unauthenticated liveness probes. Remaining phase ownership and client ECONNRESET are unproven. Timing/equivalence or production changes require a separate implementation decision.
- **6.5 remains unchecked.** Operator will restart all Pi processes; then use client-side transport labels per session. Mixed-client keeper `previous=false` aggregates cannot prove plugin improvement.

## 6. Deployment (existing-scope approval verified; validation and rollback gates remain)

- [x] 6.1 Add the scoped Ansible dispatch `deploy-responses-path`:
  - plan → approved-digest gate → pull before mutation → snapshot of the previous keeper image, apisix.yaml, routing_selector.lua, config.yaml and compose block → `up -d --no-deps response-keeper apisix-models` → verify → rescue.

  Verify that `--list-tags` shows it, that plan mode mutates nothing, and that the legacy `deploy-response-keeper` tag is not used.
- [x] 6.2 Run plan mode against production. Present the diff and rollback receipt to the operator.
- [x] 6.3 Apply after approval. Verify:
  - both services are healthy;
  - keeper restart and OOM counts are recorded;
  - no `/v1/responses` request has upstream `127.0.0.1:9080`;
  - other container IDs are unchanged;
  - one real low-cost Responses turn on HTTP and one on WebSocket succeeds (only with operator approval for inference).
- [ ] 6.4 Observe for 24 h and record selector and keeper 503 counts, the keeper `previous=false` ratio, and host swap and load in `verification.md`. Verify the comparison against the 0.2 baseline.
- [ ] 6.5 Release the paired change and record exact commits. After all participating Pi processes load the updated versions, verify that the `WS/full` share drops, measured using client-side `WS/full` versus `WS/continuation` labels per session. Keeper's mixed-client `previous=false` aggregate is context only, not attribution to this change.
