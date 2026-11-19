## Purpose

Let the Pi OpenAI Responses extension continue a session across WebSocket reconnects using the server-stored response ID, instead of resending full history whenever a socket closes.

## ADDED Requirements

### Requirement: Continuation survives a socket reconnect

When the extension opens a new Responses WebSocket for a session whose last completed turn was stored by the server (`store: true` acknowledged), it SHALL send `previous_response_id` with only the delta input, under the same conditions that already govern same-socket continuation:

- the request envelope, excluding input, is identical;
- the new input extends the previous input plus the assistant output;
- the connection identity (endpoint, credential, headers, session) is the same.

A continuation anchor SHALL be kept per connection identity and session. It SHALL NOT be shared across credentials, endpoints or sessions.

Once the extension is in a state that requires full history, it SHALL NOT use an anchor. Such states are: checkpoint cooldown, a recovery probe, and an explicitly rejected anchor.

#### Scenario: Reconnect after idle close
- **WHEN** the previous turn completed and was stored, the socket was then closed for idleness, and the next turn opens a new socket
- **THEN** the first frame on the new socket carries `previous_response_id` set to the stored response ID, plus only the new input items
- **AND** the transport notification reports `WS/continuation`

#### Scenario: Reconnect after a failed turn
- **WHEN** a turn fails and its socket is closed
- **THEN** the anchor stays at the last turn that completed and was stored
- **AND** the failed turn is never used as an anchor

#### Scenario: Different credential does not reuse anchor
- **WHEN** the next request uses a different API key or connection headers
- **THEN** no `previous_response_id` is derived from the other identity's anchor

### Requirement: Missing stored state falls back exactly once

If a cross-connection continuation is rejected before generation with `previous_response_not_found`, or with the known invalid-reference rejection, the extension SHALL retry the same request exactly once on a fresh socket with full history and no `previous_response_id`. It SHALL then mark that anchor as unusable. Any other failure, including failures that are ambiguous, auth-related, rate-limited or that occur after partial output, SHALL NOT trigger this retry.

#### Scenario: Keeper restarted
- **WHEN** keeper was restarted and answers `previous_response_not_found` to a cross-connection continuation
- **THEN** the extension sends exactly one full-history retry
- **AND** the user sees no error if that retry succeeds

#### Scenario: Non-reference failure is not replayed
- **WHEN** a cross-connection continuation fails with a 5xx error or after partial output
- **THEN** no full-history retry is sent for that request
