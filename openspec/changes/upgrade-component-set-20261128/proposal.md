## Why

Move the deployed CLIProxyAPI and AISIX forks to the current checked upstream patch releases without dropping the gateway's downstream behavior. Keep the optional ZCode sidecar on the latest release.

## What Changes

- Update tracked CLIProxyAPI and AISIX image defaults and pin their exact tested digests in the generated standard compatibility lock.
- Update the production ZCode image through its private selector. The production model catalog has no nginx sidecar; do not change the obsolete selector or introduce a service.
- Deploy only the three selected services, one at a time. Leave routes, credentials, persistence, namespace owners, account settings, and AISIX resources unchanged.

## Capabilities

No new or modified gateway requirements. This is image maintenance under the existing `production-operations-control`, `production-ops-tooling`, and capacity requirements; `skip_specs: true` is intentional.

## Impact

The selected containers will be recreated separately and may interrupt their in-flight requests. Retain each old image and use a fresh guarded rollback preview on a regression.
