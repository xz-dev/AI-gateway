## Context

AISIX has a private `resources.yaml`. Each aggregate names direct model entries in target order. A declaration can be syntactically valid but resolve to a model absent from CPA's native inventory. The current resources have no sibling aggregate using Congee as fallback, so no Congee target will be added.

## Goals / Non-Goals

- Delete unsupported alternate references and the non-Claude XL direct entries. Migrate the Fable aggregate and its only target to 5.2. Preserve other primary targets, remaining alternate order, unrelated declarations and routing policies.
- Reuse the deployment-time policy validator. No change to gateway error classification, CPA registrations, provider credentials, model catalog or AISIX runtime code.

## Decisions

1. Compare native CPA model IDs with the `model_name` of each secondary CPA-backed direct entry, not the AISIX display label. A wildcard or an unresolved target is not positive evidence of capability. Use CPA's native endpoint behind the existing private access path, not the merged public list; don't write the inventory or keys to Git.
2. If a secondary target is absent, corroborate absence against the live registration config to distinguish a configuration mismatch from a momentary availability condition. Don't remove a primary merely because it is absent. Deployment still fails closed on missing secondary evidence rather than assuming a target can serve.
3. Normalize fallback counts using the existing tool. For incident replay, use the deployed AISIX image with a mock CPA. The old XL branch returns 400, while the corrected single-primary route returns a failover-eligible 502 or 503 on overload and cooling (never 400), without calling XL or changing runtime status classification. Confirm the resulting Sub2API client status stays 5xx.
4. Publish only scrubbed audit and test evidence. Independently review the guarded private diff and candidate hashes. Use the managed-file path with a fresh preview and protected recovery snapshot; verify effective routes, protected neighbours and no drift with read-only, zero-token checks.

## Risks / Trade-offs

- A native list can omit a registered but temporarily unavailable model. A failed capability gate blocks an activation but does not modify live routes.
- Owner decision (2026-11-20): delete `xl/gpt-6-luna` along with the other non-Claude XL declarations; rename `xl/claude-fable-5-1` and the bare `claude-fable-5-1` aggregate to 5.2. The direct row's display and upstream names and the aggregate's sole target are the only affected fields. CPA registers `xl/claude-fable-5-2`.
- From November 13 at 04:04 UTC to November 20 at 04:04 UTC, Sub2API reported zero usage rows for each old ID. AISIX retained access logs also show zero requests for either ID since November 18 at 12:54 UTC; earlier non-Sub2API HTTP access is not covered.
- Clients pinned to 5.1 lose the exact AISIX route and may fail or take a different path. CPA still registers 5.1 via other channels; neither its inventory nor AISIX's wildcard changes. A supplemented catalog or the wildcard can still expose or forward CPA-native 5.1, so the change does not promise a universal 404.
- A single-target aggregate deliberately has no internal failover. The bot can fail over on 502 or 503. The deployed AISIX can still try its only candidate while it is cooling. If a correctly registered fallback later changes capability, the runtime can still surface a 400 until the separate classification problem is addressed.
