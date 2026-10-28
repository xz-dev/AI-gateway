## ADDED Requirements

### Requirement: Classified inference is a bounded streaming pipe

The internal instance SHALL classify only POST `/v1/responses`, `/v1/chat/completions` and `/v1/images/generations`, including front-bridge WS turns forwarded through HTTP-only Sub2API. Before any complete-body read, it SHALL read the client body incrementally and retain at most one bounded routing prefix. The prefix limit SHALL be one named constant, `MAX_ROUTING_PREFIX_BYTES`, whose **value is pending the separately requested client survey**; a numeric default SHALL NOT be silently selected before that review. Scanner depth and extracted model length SHALL also be bounded.

The gateway SHALL NOT decode a complete inference body into a JSON document, retain/spool the complete request for replay, or make an inference-path discovery network request. After one backend choice, it SHALL forward the retained prefix once followed by unread chunks through a backpressured body iterator. It SHALL stream the response incrementally, preserving status, end-to-end headers, model/session identifiers, payload bytes and SSE event bytes, except an explicitly approved retention-field transformation. It SHALL handle cancellation and HTTP framing without retrying another backend.

Ingress limits SHALL remain 64 MiB for Responses and 16 MiB for other classified JSON. The pipe SHALL successfully handle 64 MiB Responses within the existing 128 MiB container cap; the old whole-body decoder's OOM SHALL NOT waive this gate. Known over-limit lengths SHALL be rejected before forwarding where possible. When a chunked/unknown-length upload crosses its limit after streaming begins, the pipe SHALL stop and close its selected upstream without replay, returning 413 if response commitment permits; it SHALL NOT claim no bytes reached that upstream.

Malformed/incomplete/duplicate model information within the retained prefix SHALL follow uncertain-to-AISIX routing, not the retired local whole-document validation contract. Complete-document validation and retention-removal semantics SHALL require explicit user agreement as specified below before implementation is locked. Requests outside classification SHALL retain current routing/compatibility behavior: multipart edits to AISIX, messages/direct WS/catch-all to CPA, catalog to model-catalog. Catalog membership/order/priority and Sub2API authorization SHALL remain unchanged.

#### Scenario: Large body has an early model
- **WHEN** accepted 1, 16 and 64 MiB Responses bodies expose the same model in the same early prefix and continue uploading
- **THEN** the gateway begins forwarding to its one selected backend before client EOF with first-byte latency independent of total body size
- **AND** request memory stays bounded by prefix/chunk state rather than body length, under the existing container cap

#### Scenario: Chunked upload remains a pipe
- **WHEN** a classified request arrives with chunked transfer encoding
- **THEN** decoded payload bytes are forwarded incrementally with correct upstream framing and no whole-body spool
- **AND** crossing the applicable limit stops the selected upload without replay or a credential change to another backend

#### Scenario: SSE passes through incrementally
- **WHEN** the selected backend emits delayed/split data or event frames, including reasoning/encrypted items
- **THEN** the same SSE bytes reach the downstream incrementally before backend EOF without event rewriting or complete-response buffering

#### Scenario: Client cancels an upload or stream
- **WHEN** the client disconnects while upload or response forwarding is active
- **THEN** upstream I/O stops, incomplete connections are closed and retained prefix/chunk resources are released
- **AND** no other backend is attempted

#### Scenario: Non-classified routes are unchanged
- **WHEN** a multipart edit, direct WebSocket upgrade, catch-all message or catalog request reaches its existing route
- **THEN** it keeps its current upstream and compatibility processing without entering the classified pipe

### Requirement: Pipe compatibility changes require an explicit contract

Before implementation or mechanism lock, the change SHALL enumerate existing complete-body validation and `prompt_cache_retention` stripping by route and obtain explicit user decisions for pipe-compatible handling. The implementation SHALL NOT preserve compatibility by reinstating complete-body buffering or silently drop the stripping behavior.

A bounded streaming top-level key filter and backend-owned stripping SHALL remain proposed alternatives until chosen. A filter SHALL remove only the approved top-level field and required separator, preserve other bytes and correctly re-frame changed length. Backend-owned handling SHALL require evidence that the relevant backend actually supports equivalent removal; a backend patch or ignored compatibility error SHALL NOT be inferred as authorized. Complete JSON validity and duplicates beyond an already-forwarded prefix cannot be guaranteed before dispatch; delegation to the backend SHALL be acknowledged as a change to the former local-400/no-forward behavior.

#### Scenario: Compatibility decision is not yet made
- **WHEN** retention filtering, complete-document validation delegation or the request-prefix cap remains unresolved
- **THEN** planning records the open decision and implementation/mechanism lock remains blocked
- **AND** no full-body parser, replay buffer or unapproved backend modification is introduced as a workaround

## REMOVED Requirements

### Requirement: Slash-rule passthrough to CPA or AISIX

**Reason**: The user selected ordered membership-based routing with a bounded streaming pipe instead of slash conventions or error replay. Uncertain prefixes deliberately go to AISIX; pre-dispatch complete-body validation is no longer assumed.

**Migration**: Resolve the prefix survey and explicit compatibility decisions, then prove the APISIX 3.19 pipe gate before replacing the classified seam. Keep non-classified routes, catalog, authorization, credentials and terminal backend errors. No error-based cross-backend fallback is permitted.
