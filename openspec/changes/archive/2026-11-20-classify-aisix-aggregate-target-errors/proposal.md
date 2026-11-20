## Why

A client can request a valid public model and receive HTTP 400 because an internal fallback chosen by AISIX no longer exists at its provider. That blames the caller for a routing failure and can send Sub2API down its client-error path instead of its pre-output failover path.

## What Changes

- Report a missing gateway-selected aggregate target as 502, or 503 when the aggregate has no available candidates. Keep genuine client model errors and policy responses unchanged.
- Test the deployed AISIX revision and its successor with synthetic Responses traffic. Publish the fix from the existing AISIX fork and replace only the AISIX image through the guarded component workflow.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `aisix-model-routing`: distinguish a gateway-selected target failure from a caller's unknown model or policy rejection.

## Impact

The source change belongs to `xz-dev/aisix`, based on `6734303611311efb336470cd63f91da75dfa141a`. This repository owns the compatibility evidence, image pin and deployment record. No APISIX, Sub2API, resource, route, cooldown or retry-policy changes. The separate read-only Codex catalog investigation is not an acceptance gate for this change.
