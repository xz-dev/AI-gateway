## ADDED Requirements

### Requirement: Bound temporary front catalog transfer observations

A temporary observation of versioned catalog transfers SHALL preserve the normal front `warn` destination, `send_timeout` and request behavior. It SHALL accept only local diagnostic traffic, discard raw messages in memory and persist only UTC time, a fixed event class and a validated request ID in bounded private storage. The operator SHALL inspect natural client or tunnel failures independently of observing a timeout. Activation SHALL require a tested host-owned rollback that works without the controller session.

#### Scenario: Raw route configuration passes through the diagnostic sink
- **WHEN** nginx emits a matched route, arbitrary request data, a malformed message, or a timeout on an unrelated path
- **THEN** the receiver writes no event for it, and no unfiltered info message is added to the normal Docker log or another persistent destination.

#### Scenario: Front times out on the versioned catalog response
- **WHEN** a native timeout record identifies the versioned GET and matches a committed HTTP 200 catalog access record by front request ID
- **THEN** the record identifies a front timeout for that request; other requests are not attributed to it from wall time alone.

#### Scenario: Natural transfer fails without a matching native timeout
- **WHEN** a catalog transfer fails independently of the diagnostic sink and the available evidence does not prove the closer
- **THEN** the operator records an inconclusive attribution, not a confirmed or excluded front timeout.

#### Scenario: Failure, deadline or observer fault closes the window
- **WHEN** the first confirmed natural failure occurs, 48 hours from verified activation elapse, or safe collection cannot continue
- **THEN** the host restores and verifies the baseline, removes the extra destination and receiver, and records coverage gaps and unrelated-component identities without requiring a live controller session.
