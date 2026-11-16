## ADDED Requirements

### Requirement: CPA preserves the Responses terminal event across data-only upstream events

CPA SHALL deliver the upstream `response.completed` (or other terminal) event to a streaming `/v1/responses` client when the upstream stream contains data-only SSE events that have a JSON `type` and no `event:` line, such as `{"type":"codex.client.keepalive"}`. Every delivered `event:` name SHALL match its payload `type`. CPA SHALL NOT mark a stream terminal on a payload whose `type` is not terminal.

#### Scenario: Keepalive data event during a long tool call

- **WHEN** an upstream Codex-compatible stream sends, in order, event/data pairs, SSE comments, a bare `data: {"type":"codex.client.keepalive"}`, more event/data pairs, `response.output_item.done` and `response.completed`
- **THEN** the client receives `response.completed` as the final event, and no delivered event name is paired with a different payload type

#### Scenario: Client identity does not matter

- **WHEN** the same upstream stream is requested with and without Codex client headers
- **THEN** both clients receive `response.completed`

### Requirement: Existing Responses framing behaviour is unchanged

CPA SHALL keep untyped data, and typed data whose `type` matches the following `event:` field, as one event. It SHALL keep recovering fragmented JSON frames, delivering in-stream error and failure events, and framing images streams exactly as before.

#### Scenario: Untyped data before its event field

- **WHEN** the upstream sends `data: {"response":{...}}` followed by `event: response.completed`
- **THEN** CPA emits them as one `response.completed` event

#### Scenario: Typed data before its own event field

- **WHEN** the upstream sends `data: {"type":"response.output_text.delta",...}` followed by `event: response.output_text.delta`
- **THEN** CPA emits them as one `response.output_text.delta` event, and the following events keep their own names
