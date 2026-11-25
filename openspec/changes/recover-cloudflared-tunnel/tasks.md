## 1. Implementation and verification

- [x] 1.1 Verify the current production baseline and add focused offline cases for consecutive 503 responses, non-503 streak reset, and skipped connectors.
- [x] 1.2 Reuse the existing bounded recovery path for `cloudflared`, preserve readiness observations, and run the recovery tests and strict OpenSpec validation.

## 2. Rollout

- [x] 2.1 Preview and apply the recovery guard on the production host without changing Compose or deliberately restarting a container.
- [x] 2.2 Read back the active policy, `/ready` and container identities; record the deployment and rollback path.
