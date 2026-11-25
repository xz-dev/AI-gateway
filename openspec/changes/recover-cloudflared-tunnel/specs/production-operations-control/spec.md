## ADDED Requirements

### Requirement: Bounded recovery for an unready Cloudflare connector

The host guard SHALL use the owned, running `cloudflared` connector's loopback `:20241/ready` endpoint. It SHALL qualify a restart only after three consecutive recovery probes return HTTP 503 from the same running instance, outside the existing startup grace. A non-503 response, an unavailable probe, or a cycle deferred because another service already restarted SHALL clear the streak. Cooldown, hourly budget, fresh identity and running-state checks, and the one-restart-per-cycle limit SHALL apply. The guard SHALL NOT start a stopped connector or restart its dependencies. A missing connector SHALL NOT prevent other application checks.

#### Scenario: Three consecutive readiness responses are 503

- **WHEN** the same running connector outside startup grace responds with HTTP 503 three times in succession and the restart budget permits it
- **THEN** an apply cycle may restart only that still-owned connector, while an observation-only cycle never restarts it

#### Scenario: A connector recovers its readiness or the probe cannot confirm 503

- **WHEN** HTTP 200, another status, or an unavailable probe interrupts the 503 streak
- **THEN** the existing streak does not authorize a restart

#### Scenario: Another application uses the restart slot

- **WHEN** another application restart defers the connector's recovery probe
- **THEN** the guard clears the connector's prior streak without changing the connector's restart budget

#### Scenario: Connector is intentionally stopped or absent

- **WHEN** the connector is stopped, paused, restarting, or absent
- **THEN** the guard does not start it and continues to check other allowlisted applications
