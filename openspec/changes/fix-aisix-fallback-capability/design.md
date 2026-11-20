## Context

AISIX has a private `resources.yaml`. Each aggregate names direct model entries in target order. A declaration can be syntactically valid but resolve to a model absent from CPA's native inventory. The current resources have no sibling aggregate using Congee as fallback, so no Congee target will be added.

## Goals / Non-Goals

- Delete unsupported alternate references and the `xl/gpt-6.2-sol` direct entry. Preserve primary targets, remaining alternate order, unrelated declarations and routing policies.
- Reuse the deployment-time policy validator. No change to gateway error classification, CPA registrations, provider credentials, model catalog or AISIX runtime code.

## Decisions

1. Compare native CPA model IDs with the `model_name` of each secondary CPA-backed direct entry, not the AISIX display label. A wildcard or an unresolved target is not positive evidence of capability. Use CPA's native endpoint behind the existing private access path, not the merged public list; don't write the inventory or keys to Git.
2. If a secondary target is absent, corroborate absence against the live registration config to distinguish a configuration mismatch from a momentary availability condition. Don't remove a primary merely because it is absent. Deployment still fails closed on missing secondary evidence rather than assuming a target can serve.
3. Normalize fallback counts using the existing tool. For incident replay, use the deployed AISIX image with a mock CPA. The old XL branch returns 400, while the corrected single-primary route returns 503 on overload and cooling without calling XL or changing runtime status classification.
4. Publish only scrubbed audit and test evidence. Independently review the guarded private diff and candidate hashes. Use the managed-file path with a fresh preview and protected recovery snapshot; verify effective routes, protected neighbours and no drift with read-only, zero-token checks.

## Risks / Trade-offs

- A native list can omit a registered but temporarily unavailable model. A failed capability gate blocks an activation but does not modify live routes.
- A single-target aggregate deliberately has no internal failover. The bot can fail over on 503. If a correctly registered fallback later changes capability, the runtime can still surface a 400 until the separate classification problem is addressed.
