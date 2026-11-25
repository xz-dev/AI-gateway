## Context

The host guard already checks `cloudflared` at its loopback readiness endpoint. Docker's process restart policy cannot act while the connector remains running and unready.

## Goals / Non-Goals

- Restart the owned connector only after three consecutive HTTP 503 responses, with the same bounds and fresh-identity checks used for application recovery.
- Preserve the readiness observation and leave all other services, the transport and the Compose topology unchanged.
- Do not treat an unavailable probe, a different HTTP status, or an explicitly stopped connector as a restart signal.

## Decisions

Use the existing namespace probe and restart path; count only HTTP 503 as a confirmed failure for the connector. A non-503 or indeterminate probe clears the streak. When another restart consumes the cycle's mutation slot, clear the connector's streak before sending that restart; any subsequent connector probe in that cycle is observation-only. A missing connector does not block recovery for other allowlisted services.

## Risks / Trade-offs

A true upstream outage can outlast a restart; the guard consumes each attempt before asking Docker to restart, enforces the cooldown and hourly budget, and does not restart dependencies. Intermittent failures that never produce three consecutive 503 responses remain observational.

## Rollout and Rollback

Preview the recovery playbook, apply only its reviewed script delta, and verify a healthy connector and unchanged container identities. Roll back by applying the prior reviewed guard with the same playbook; retain the state file and restart history.
