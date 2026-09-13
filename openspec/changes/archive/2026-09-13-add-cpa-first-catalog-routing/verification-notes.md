# Verification and coordination notes

## Production outcome — 2026-09-13

The separately authorized production rollout completed solo at `2026-09-13T08:45:06Z`. Accounts 1/4/5/6 now serve through the internal APISIX entrance, not directly through AISIX.

- **Deployment:** models-enricher runs `ghcr.io/xz-dev/ai-gateway-models-enricher:cpa-first-routing-20260913@sha256:b8c7206a606de7214d5f29043f44a92e35fa885ffed39fa204323f4bfd9fff86`. It was the only service whose image changed; Sub2API's image remained unchanged. Production tracked drift and unowned files were preserved, with no change to published bindings.
- **Account preservation:** a row-locked transaction changed only `credentials.base_url` to `http://cli-proxy-api:8317/v1` and `updated_at`. Every other credential key and account column was checked inside the transaction. The post-check confirmed exact credential preservation apart from the URL, including caller keys, `model_mapping`, `pool_mode=true`, `pool_mode_retry_count=1`, and `[400,401,403,404,429,500,502,503,504]`. No Headroom placement was overwritten.
- **Routing:** real requests and access logs proved CPA-only `xl/gpt-6-astra` and overlap `zcode/glm-5.3` went to CPA and returned 200. `axis/gpt-6-astra` went to AISIX and returned 400, with no cross-backend attempt. The operator had intentionally disabled axis; AISIX declarations were left unchanged.
- **Accepted boundary:** delivery proves correct classification/forwarding and checks for migration regressions, not that every catalog model is currently callable. Existing or intentionally disabled upstream failures are not, by themselves, deployment failures. This applies beyond the axis trio; `z-ai/glm-5.3-flash` was classified to AISIX without requiring a successful inference.
- **Client checks:** internal standard and Codex catalogs both returned 200 with the same 238 IDs in that captured generation. Public and Sub2API catalog/SSE calls returned 200; SSE included terminal events. Public unauthorized access returned an empty 404. Five one-minute health/integrity samples passed.
- **WS limitation:** the public WebSocket handshake succeeded and response events arrived, but inference terminated with `response.failed`. This is not recorded as successful end-to-end WS inference; its cause was not established. Offline legacy-WS compatibility evidence remains separate from this live result.
- **Coordination:** the parallel owner confirmed no production, account, shared-file, or Sub2API-image changes during this rollout. This archive does not claim completion of error propagation, session affinity, or Headroom work.

Private evidence is retained under `/root/rollouts/add-cpa-first-catalog-routing-20260913T081506Z-solo`: `SUCCESS`, `receipts/backend-provenance.json`, `receipts/account-migration.json`, `receipts/account-postcheck.json`, `receipts/final-post-check.log`, `receipts/observation.log`, `receipts/public-websocket.json`, and backup/checksum files. The earlier attempt under `/root/rollouts/add-cpa-first-catalog-routing-20260913T070838Z` remains frozen with its rollback evidence; its `PHASE1_OK` is not the final deployment receipt. Archiving changes local documentation only and does not remove production recovery assets.

## Implementation boundary (historical)

The implementation phase prepared local source, Compose topology, and offline acceptance evidence without production mutation. Publication and deployment followed under separate operator approval, as recorded above. This change did not edit the parallel changes `fix-sub2api-error-propagation`, `preserve-end-to-end-session-affinity`, or `add-headroom-context-compression`.

## Parallel-change coordination

- The routing seam does not consume backend failures. The isolated APISIX fixture preserves CPA 429/502 status and body, surfaces a selected-backend timeout, and does not replay a committed partial SSE stream through AISIX. The parallel error-propagation owner reports a verified minimal native Responses SSE patch, but still classifies that work as partial; this change does not claim that its HTTP, WS, exhaustion, or overall spinner work is complete.
- The selector reads only the JSON `model` field. It leaves session-affinity headers untouched; the real APISIX fixture sends `session_id: session-exact` and the selected CPA fake observes the exact value.
- Headroom remains separate, unimplemented, and independently approved. Ordinary accounts will target the internal APISIX entrance. Any combined rollout for a Headroom-opted-in account waits for the Headroom owner's approved rebase so Headroom forwards to the internal entrance (`Sub2API → Headroom → internal APISIX → CPA/AISIX`). This change must not blindly overwrite such an account's `base_url`.

## Pre-rollout Sub2API account/config requirements (historical)

At implementation handoff, production work required separate approval and a fresh authorized read of each complete account/config object. The parallel owner's protected read had not established live global switch/timeout policy; source defaults were not a substitute. The completed rollout above preserved current account policy rather than applying a future peer unset.

The pre-write requirements were:

1. Exchange current field-level diffs with the `fix-sub2api-error-propagation` owner (their source baseline is pinned to Sub2API v0.2.4 / `5de5e2b`).
2. Re-read each latest complete credentials/config object, then change only this migration's owned `base_url` and strictly necessary routing fields. Preserve credentials, caller keys, `model_mapping`, `pool_mode=true`, `retry_count=1`, and all unrelated fields.
3. Preserve the peer owner's supported retry-status override unset. Unset means the field is absent, never `[]`, and must not restore the default retry count of 3.
4. Keep entrance no-cross-backend-retry scoped to APISIX. Do not alter Sub2API pool retries or AISIX combo retry/cooldown budgets.
5. Back up the pre-write objects and define stop thresholds. Rollback only this migration's owned fields to the immediately prior serving placement (currently direct AISIX at `http://aisix:3000/v1` for accounts 1/4/5/6), while retaining all independent peer edits. A CPA-only catch-all is not an equivalent rollback.

## Independent-review closure

The first fresh, read-only review returned `CHANGES_REQUESTED`. The local implementation now closes its four blockers without production access:

- CPA `NativeManifest` validation distinguishes a valid empty `models: []` inventory from a missing, null, non-array, or ID-less inventory. Invalid authority is removed before read-cache publication and cannot be resurrected by a later transient stale fallback.
- The first collection attempt publishes a generation even when both sources fail. Catalog traffic thereafter consumes that failed snapshot and leaves retries to the bounded periodic owner instead of repeatedly collecting while generation remains zero.
- The real APISIX fixture now holds the second SSE event until the first reaches the client, then asserts the complete ordered stream. Its forced committed truncation asserts the client-visible transport error as well as partial data, absence of a terminal event, and zero cross-backend replay.
- Authorization and retry evidence now includes the unchanged external boundaries below rather than inferring their behavior from this repository's fake Sub2API handlers.

## Offline acceptance evidence

- `go test -race -count=1 ./...` in `models-enricher`: passes strict raw CPA validation, cache invalidation/no-resurrection, initial failed-generation publication, concurrent catalog no-refetch, a synchronized real-refresh atomic-publication test, source availability, bounded-cadence recovery, generation-pinned projection, four-outcome index, standard/Codex parity, and existing enrichment regressions.
- Pinned APISIX 3.18 Lua suites: seven front authorization/intersection contracts and six selector contracts pass.
- Isolated real-APISIX routing fixture (`TestRoutingHTTPFixture`): CPA-only and overlap choose CPA; AISIX-only chooses AISIX; exact Unicode/space/plus/slash ID and body survive; the former HTTP alias remains exact; authoritative miss is 404; unavailable/lookup failure are 503; 429/502/timeout/committed disconnect do not cross backends; the committed disconnect remains a client-visible read error; both SSE events arrive incrementally and in order; legacy WS bypasses classification.
- Existing isolated catalog fixture (`TestCatalogHTTPFixture`): standard and Codex requests reach the enricher through the entrance; canonical raw CPA reads stay at `client_version=1`; public authorization intersection, large manifests, native failure, size boundary, and sidecar path isolation pass.
- Read-only race tests against pinned Sub2API `5de5e2bed035d43591a2e10e51f420ef6a84eb98` (whose sole working-tree modification is the parallel owner's response-handling file, not these tests) pass the group-model authorization middleware, gateway route ordering, unauthorized-list filtering, and exact API-key pool retry sequences. Denied OpenAI inference stops in the group middleware before its handler; listing omits disallowed models; `pool_mode_retry_count=1` produces two attempts on the selected account before the independently bounded account-switch behavior.
- An isolated `--network none` fixture using the intended AISIX `1.2.0+deadlockfix.7d6d14b` image (source revision `adcf0523b9f84deed64fbdea311df292c5bc541b+7d6d14bbf5f5ec4577466da48ce7763d519d81bb`) observes AISIX's own `retries: 1` boundary: a fake target returning 429 once and success next is called exactly twice, and the client receives the successful response. This entrance change does not modify that retry/cooldown configuration.
- `scripts/validate.sh .env.example` passes against a task-scoped empty Compose override: Compose, firewall, one-way/two-member internal edges, private paths, APISIX schemas, and Lua checks are valid. The empty override is necessary only to avoid mixing this public baseline check with the local private `compose.override.yaml`.

## Follow-up disposition at archive

1. Per-model upstream reachability is outside this delivery's acceptance scope. The operator explicitly accepted intentional unavailability and requested that AISIX configuration remain unchanged; no repair or deletion of axis or other declarations was performed.
2. The separately approved publication, production deployment, account migration, and observation are complete, with the live WS limitation recorded above. No additional cutover is pending for this change.
3. Private recovery evidence remains retained. No production cleanup or parallel-change implementation is authorized by this archive.
