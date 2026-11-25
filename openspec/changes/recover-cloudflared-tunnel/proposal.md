## Why

A running Cloudflare Tunnel connector can repeatedly lose every edge connection without exiting. The host guard already detects HTTP 503 from its readiness endpoint but reports it without taking action.

## What Changes

- Allow the existing host guard to restart only the owned `cloudflared` connector after three consecutive readiness responses of HTTP 503.
- Reuse the existing startup grace, restart budget and ownership checks. Do not change the tunnel's transport, Compose configuration or other services' policies.

## Capabilities

### Modified Capabilities

- `production-operations-control`: Add bounded connector recovery to the host guard.

## Impact

A recovery attempt can interrupt this connector's in-flight requests. Deploy only the host guard using the existing recovery playbook; no deliberate production outage is needed to validate the rollout.
