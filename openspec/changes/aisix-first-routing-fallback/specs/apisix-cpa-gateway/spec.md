## ADDED Requirements

### Requirement: Classified inference is a bounded streaming pipe

The internal instance SHALL classify only POST `/v1/responses`, `/v1/chat/completions` and `/v1/images/generations`, including front-bridge WS turns forwarded through HTTP-only Sub2API. Before any complete-body read, it SHALL read client chunks through **one spooled sliding-window buffer**, regardless of model position. `MAX_ROUTING_PREFIX_BYTES=65,536` SHALL bound its in-memory window, not model search distance. Overflow SHALL flush oldest bytes automatically to a lazily created private disk-backed file. Scanner depth, extracted model length and I/O chunks SHALL be separately bounded.

The buffer SHALL expose `append(chunk)`, `drain(sink)` and idempotent `close()`; drain SHALL send file bytes then window bytes in order with backpressure and release consumed storage. The scanner SHALL expose `feed(chunk) -> needMore | model | uncertain`, with explicit EOF. The routing sequence SHALL be read → feed → append until decided → choose → drain → pipe the rest. Every body byte SHALL pass through feed/append; after selection the decision is latched and the remaining upload uses the same append/drain machinery. An early model SHALL use the same code with an empty file part; there SHALL NOT be separate pipe-versus-spill routing branches. A token split at the window boundary SHALL remain `needMore`, not uncertainty.

The gateway SHALL NOT decode a complete inference body into a JSON document, retain/spool a body for cross-backend replay, or make an inference-path discovery network request. Initial model search MAY require bounded disk storage approaching the body ceiling if model is at the end; it SHALL drain once, not retry. After one backend choice it SHALL stream response chunks preserving status, end-to-end headers, model/session identifiers, payload and SSE bytes except an explicitly approved retention transformation. It SHALL handle cancellation/framing without retrying another backend.

Ingress limits SHALL remain 64 MiB for Responses and 16 MiB for other classified JSON. The pipe SHALL successfully handle 64 MiB Responses within the existing 128 MiB container cap; the old whole-body decoder's OOM SHALL NOT waive this gate. Known over-limit lengths SHALL be rejected before forwarding where possible. When a chunked/unknown-length upload crosses its limit after streaming begins, the pipe SHALL stop and close its selected upstream without replay, returning 413 if response commitment permits; it SHALL NOT claim no bytes reached that upstream.

No trustworthy model at EOF, malformed/untrustworthy scanned JSON or duplicate model detected before commitment SHALL follow uncertain-to-AISIX routing, not the retired local whole-document validation contract. A valid model located beyond the memory window SHALL receive the same membership decision as one at offset 1. Complete-document validation, later duplicates and retention-removal semantics SHALL require explicit user agreement before implementation lock. Requests outside classification SHALL retain current routing/compatibility behavior: multipart edits to AISIX, messages/direct WS/catch-all to CPA, catalog to model-catalog. Catalog membership/order/priority and Sub2API authorization SHALL remain unchanged.

#### Scenario: Large body has an early model
- **WHEN** accepted 1, 16 and 64 MiB Responses bodies expose the same model in the same early prefix and continue uploading
- **THEN** the gateway begins forwarding to its one selected backend before client EOF with first-byte latency independent of total body size
- **AND** request memory stays bounded by prefix/chunk state rather than body length, under the existing container cap

#### Scenario: Late model uses the same pipe
- **WHEN** a valid accepted request completes its top-level model at offset 4 MiB instead of offset 1
- **THEN** the same scanner/buffer/selection/drain code runs with a fixed 64 KiB window and automatic disk overflow
- **AND** a CPA-only model absent from usable membership selects CPA, with first drain starting when the model completes rather than waiting for client EOF
- **AND** all forwarded bytes match the early-model contract except the explicitly approved retention transformation

#### Scenario: Chunked upload remains a pipe
- **WHEN** a classified request arrives with chunked transfer encoding
- **THEN** decoded payload bytes use the same bounded sliding spool and drain with correct upstream framing
- **AND** crossing the applicable limit stops the upload and cleans storage without replay or a credential change to another backend

#### Scenario: SSE passes through incrementally
- **WHEN** the selected backend emits delayed/split data or event frames, including reasoning/encrypted items
- **THEN** the same SSE bytes reach the downstream incrementally before backend EOF without event rewriting or complete-response buffering

#### Scenario: Client cancels an upload or stream
- **WHEN** the client disconnects while upload or response forwarding is active
- **THEN** upstream I/O stops, incomplete connections are closed and window/chunk/file/quota resources are released
- **AND** no other backend is attempted

#### Scenario: Non-classified routes are unchanged
- **WHEN** a multipart edit, direct WebSocket upgrade, catch-all message or catalog request reaches its existing route
- **THEN** it keeps its current upstream and compatibility processing without entering the classified pipe

### Requirement: Selection spooling has explicit disk and lifecycle bounds

File plus window bytes SHALL NOT exceed the applicable 64 MiB Responses / 16 MiB other classified-body ceiling. Spools SHALL use a dedicated private disk-backed directory `/var/tmp/apisix-models-spool`, not tmpfs or an unbounded writable layer. Aggregate quota/reservations SHALL be enforceable across workers; the initial proposal is 256 MiB, subject to prototype/storage review before mechanism lock. Admission SHALL reserve the route's possible spill at first overflow and release the reservation after drain/close. Success, all failures, cancellation and restart orphan cleanup SHALL remove private files without exposing contents or following untrusted links. Cgroup testing SHALL include filesystem page cache and bounded chunk/scanner overhead, not only the 64 KiB window.

Resource exhaustion SHALL fail explicitly without guessing another backend, silently dropping bytes, raising ingress/memory limits or retaining replay data. Within body/resource/time budgets, legal field position alone SHALL NOT cause wrong routing or unavailability.

#### Scenario: Spool resource failure is not routing uncertainty
- **WHEN** disk reservation, creation or writing fails
- **THEN** the request fails with a controlled resource error before dispatch, or stops/closes its already selected upstream if dispatched
- **AND** all files/reservations are cleaned without trying AISIX or CPA as a substitute

#### Scenario: Spool lifecycle is complete
- **WHEN** a late-model upload succeeds, errors or is cancelled, or the selector restarts after interruption
- **THEN** active/final cleanup removes its files and releases disk/window reservations
- **AND** repeated runs show no accumulating spool files or retained body memory

### Requirement: Pipe compatibility changes require an explicit contract

Before implementation or mechanism lock, the change SHALL enumerate existing complete-body validation and `prompt_cache_retention` stripping by route and obtain explicit user decisions for pipe-compatible handling. The implementation SHALL NOT preserve compatibility by reinstating complete-body buffering or silently drop the stripping behavior.

A bounded streaming top-level key filter and backend-owned stripping SHALL remain proposed alternatives until chosen. A filter SHALL remove only the approved top-level field and required separator, preserve other bytes and correctly re-frame changed length. Backend-owned handling SHALL require evidence that the relevant backend actually supports equivalent removal; a backend patch or ignored compatibility error SHALL NOT be inferred as authorized. Complete JSON validity and duplicates beyond an already-forwarded prefix cannot be guaranteed before dispatch; delegation to the backend SHALL be acknowledged as a change to the former local-400/no-forward behavior.

#### Scenario: Compatibility decision is not yet made
- **WHEN** retention filtering, complete-document validation/later-duplicate delegation or spool quota/mount enforcement remains unresolved
- **THEN** planning records the open decision and implementation/mechanism lock remains blocked
- **AND** no full-body parser, replay buffer, order-dependent cutoff or unapproved backend modification is introduced as a workaround

## REMOVED Requirements

### Requirement: Slash-rule passthrough to CPA or AISIX

**Reason**: The user selected ordered membership-based routing with one bounded sliding-window spool instead of slash conventions, order-dependent overflow or error replay. Uncertain scanned information goes to AISIX; a valid late model does not. Pre-dispatch complete-body validation is no longer assumed.

**Migration**: Review completed survey/passthrough findings, verify separate keeper model-first delivery, resolve storage/compatibility decisions and prove the APISIX 3.19 single-path gate before replacing the classified seam. Keep non-classified routes, catalog, authorization, credentials and terminal backend errors. No error-based cross-backend fallback is permitted.
