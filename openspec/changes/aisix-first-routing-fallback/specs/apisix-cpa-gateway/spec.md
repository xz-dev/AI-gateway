## ADDED Requirements

### Requirement: Classified inference is a bounded streaming pipe

The internal instance SHALL classify only POST `/v1/responses`, `/v1/chat/completions` and `/v1/images/generations`, including front-bridge WS turns forwarded through HTTP-only Sub2API. Before any complete-body read, it SHALL read client chunks through **one spooled sliding-window buffer**, regardless of model position. `MAX_ROUTING_PREFIX_BYTES=65,536` SHALL bound its in-memory window, not model search distance. Overflow SHALL flush oldest bytes automatically to a lazily created private disk-backed file. Scanner depth, extracted model length and I/O chunks SHALL be separately bounded.

The buffer SHALL expose `append(chunk)`, `drain(sink)` and idempotent `close()`; drain SHALL send file bytes then window bytes in order with backpressure and release consumed storage. The scanner SHALL expose `feed(chunk) -> needMore | model | uncertain`, with explicit EOF. The routing sequence SHALL be read → feed → append until decided → choose → drain → pipe the rest. Every body byte SHALL pass through feed/append; after selection the decision is latched and the remaining upload uses the same append/drain machinery. An early model SHALL use the same code with an empty file part; there SHALL NOT be separate pipe-versus-spill routing branches. A token split at the window boundary SHALL remain `needMore`, not uncertainty.

The gateway SHALL NOT decode a complete inference body into a JSON document, retain/spool a body for cross-backend replay, or make an inference-path discovery network request. Initial model search MAY require bounded disk storage approaching the body ceiling if model is at the end; it SHALL drain once, not retry. After one backend choice it SHALL stream response chunks preserving status, end-to-end headers, model/session identifiers, payload and SSE bytes. The gateway SHALL NOT strip `prompt_cache_retention`; targeted removal SHALL be owned by CPA configuration after the gateway boundary. It SHALL handle cancellation/framing without retrying another backend.

Ingress limits SHALL remain 64 MiB for Responses and 16 MiB for other classified JSON. The pipe SHALL successfully handle 64 MiB Responses within the existing 128 MiB container cap; the old whole-body decoder's OOM SHALL NOT waive this gate. Known over-limit lengths SHALL be rejected before forwarding where possible. When a chunked/unknown-length upload crosses its limit after streaming begins, the pipe SHALL stop and close its selected upstream without replay, returning 413 if response commitment permits; it SHALL NOT claim no bytes reached that upstream.

No trustworthy model at EOF, malformed/untrustworthy scanned JSON or duplicate model detected before commitment SHALL follow uncertain-to-AISIX routing, not the retired local whole-document validation contract. A valid model located beyond the memory window SHALL receive the same membership decision as one at offset 1. After dispatch the same scanner SHALL continue bounded JSON token/structure validation through EOF, inspecting each chunk before upstream release. A later duplicate top-level `model` or malformed JSON SHALL cancel the selected upstream and clean spool/accounting resources without rerouting. If client response headers are unsent the gateway SHALL return 400; otherwise it SHALL abort the client connection. This SHALL NOT imply execution/billing has not begun or can be undone. Requests outside classification SHALL retain current routing: multipart edits to AISIX, messages/direct WS/catch-all to CPA, catalog to model-catalog. Catch-all compatibility SHALL change only by removing its whole-body retention rewrite. Catalog membership/order/priority and Sub2API authorization SHALL remain unchanged.

#### Scenario: Large body has an early model
- **WHEN** accepted 1, 16 and 64 MiB Responses bodies expose the same model in the same early prefix and continue uploading
- **THEN** the gateway begins forwarding to its one selected backend before client EOF with first-byte latency independent of total body size
- **AND** request memory stays bounded by prefix/chunk state rather than body length, under the existing container cap

#### Scenario: Late model uses the same pipe
- **WHEN** a valid accepted request completes its top-level model at offset 4 MiB instead of offset 1
- **THEN** the same scanner/buffer/selection/drain code runs with a fixed 64 KiB window and automatic disk overflow
- **AND** a CPA-only model absent from usable membership selects CPA, with first drain starting when the model completes rather than waiting for client EOF
- **AND** all forwarded payload bytes match the client body without gateway retention filtering

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

#### Scenario: Later duplicate or malformed JSON before response headers
- **WHEN** an already-dispatched request contains a second top-level `model` or malformed JSON at suffix offset 1 KiB or 4 MiB and client response headers remain unsent
- **THEN** the same scanner cancels upstream before releasing the offending chunk, returns 400 and cleans file/window/quota resources
- **AND** no second backend is attempted and no guarantee of zero upstream execution/billing is made

#### Scenario: Later duplicate or malformed JSON after response commitment
- **WHEN** the same later faults are detected at 1 KiB or 4 MiB after client response headers or SSE bytes have been sent
- **THEN** the gateway cancels upstream, aborts the client connection and cleans spool/accounting resources without changing HTTP status or rerouting

#### Scenario: Non-classified upstreams are unchanged
- **WHEN** a multipart edit, direct WebSocket upgrade, catch-all message or catalog request reaches its existing route
- **THEN** it keeps its current upstream without entering the classified pipe
- **AND** catch-all no longer reads/decodes/re-encodes a body to strip retention

### Requirement: Selection spooling has explicit disk and lifecycle bounds

File plus window bytes SHALL NOT exceed the applicable 64 MiB Responses / 16 MiB other classified-body ceiling. Spools SHALL use a dedicated private disk-backed directory `/var/tmp/apisix-models-spool`, not tmpfs or an unbounded writable layer. Aggregate spool quota SHALL be 1 GiB (1,073,741,824 bytes), enforceable across workers with atomic quota/reservation accounting. New requests SHALL receive 503 when the quota is exhausted. Admission SHALL reserve the route's possible spill at first overflow and release the reservation after drain/close. Success, all failures, cancellation and restart orphan cleanup SHALL remove private files without exposing contents or following untrusted links. Cgroup testing SHALL include filesystem page cache and bounded chunk/scanner overhead, not only the 64 KiB window.

Resource exhaustion SHALL fail explicitly without guessing another backend, silently dropping bytes, raising ingress/memory limits or retaining replay data. Within body/resource/time budgets, legal field position alone SHALL NOT cause wrong routing or unavailability.

#### Scenario: Spool resource failure is not routing uncertainty
- **WHEN** disk reservation, creation or writing fails
- **THEN** the request fails with 503 before dispatch, or stops/closes its already selected upstream if dispatched
- **AND** all files/reservations are cleaned without trying AISIX or CPA as a substitute

#### Scenario: Aggregate quota is exhausted
- **WHEN** concurrent spools/reservations exhaust the 1 GiB aggregate quota
- **THEN** new requests receive 503 without selecting a substitute backend or exceeding aggregate/per-request limits
- **AND** released reservations become available again after drain/close cleanup

#### Scenario: Spool lifecycle is complete
- **WHEN** a late-model upload succeeds, errors or is cancelled, or the selector restarts after interruption
- **THEN** active/final cleanup removes its files and releases disk/window reservations
- **AND** repeated runs show no accumulating spool files or retained body memory

### Requirement: Retention compatibility is owned by targeted CPA configuration

CPA SHALL remove `prompt_cache_retention` for rejecting upstreams through existing v8 `requests.payload.filter[].params` rules, initially targeted to verified Kimi effective model/base aliases. The gateway SHALL remove both classified-selector stripping and catch-all `dkjson` decode/strip/re-encode; the scanner SHALL NOT filter retention. Supporting upstreams SHALL retain valid retention behavior, without a catch-all CPA `*` filter or an unapproved backend patch. AISIX-native unsupported fields SHALL NOT be claimed to have equivalent configurable removal.

CPA rule activation SHALL preserve the live worker-owned configuration and plugin-version keys. If no existing CPA config deployment path supports it, activation SHALL use a reviewed manual window with backup/readback/rollback, not an unsupported playbook invocation. Plugin hot reload SHALL NOT be used. The revised plan SHALL require supervisor review and an accepted prototype before runtime implementation; removing the old pre-dispatch whole-document guarantee SHALL NOT reintroduce a full-body parser.

#### Scenario: Kimi retention is handled downstream
- **WHEN** a production Kimi request includes `prompt_cache_retention` and matches the targeted CPA rule
- **THEN** the gateway preserves its payload, CPA removes that field before the rejecting upstream and the supported request returns 200
- **AND** streaming/nonstreaming and retention-present/absent checks demonstrate effective selector matching

#### Scenario: Valid retention on another provider is preserved
- **WHEN** a request targets a supporting provider outside the targeted rejection rules
- **THEN** no gateway filter or global CPA rule removes its valid retention field

#### Scenario: Runtime gates remain closed during planning
- **WHEN** supervisor review or the new APISIX pipe prototype has not completed
- **THEN** no runtime implementation or production activation is inferred from the resolved user decisions

## REMOVED Requirements

### Requirement: Slash-rule passthrough to CPA or AISIX

**Reason**: The user selected ordered membership-based routing with one bounded sliding-window spool instead of slash conventions, order-dependent overflow or error replay. Uncertain scanned information goes to AISIX; a valid late model does not. Pre-dispatch complete-body validation is no longer assumed.

**Migration**: Review completed survey/passthrough findings, verify separate keeper model-first delivery, review the resolved CPA-retention/continuous-validation/1 GiB quota contract and prove the APISIX 3.19 single-path gate before replacing the classified seam. Keep non-classified upstream routing, catalog, authorization, credentials and terminal backend errors; remove catch-all retention processing only as explicitly specified. No error-based cross-backend fallback is permitted.
