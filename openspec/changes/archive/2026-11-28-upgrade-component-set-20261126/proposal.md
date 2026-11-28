## Why

Production has five newer published image selections and three newer upstream releases that require porting the deployed forks. Update the tested set together without replacing private configuration or losing downstream behavior.

## What Changes

- Update APISIX (both planes), cloudflared, ZCode, PostgreSQL, and Redis after their applicable gates pass.
- Port the deployed CPA, Sub2API, and AISIX forks to their selected upstream releases and validate the exact built artifacts.
- Keep the other ten image groups unchanged when no newer compatible artifact is available.
- Push the complete tested set to `main` once, then roll it out one service or proven coupled group at a time. Stop if any artifact, rollback, or rollout gate fails.

## Capabilities

### Modified Capabilities

- `production-operations-control`: enforce the requested single-push full-set readiness gate before serialized rollout.

## Impact

Tracked selectors and the generated compatibility lock. Update the private image selectors and migrate only the format of the AISIX resource file: retain IDs, references, routes, keys, ACLs, and bindings. Redis is an approved restartable cache; retain its pre-switch snapshot separately from durable PostgreSQL data.
