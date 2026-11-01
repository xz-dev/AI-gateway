## Context

See proposal.md for motivation. The live/controller hashes match before editing. Old Terra uses Codex → AIHub failover. CPA currently registers new Terra under Codex, XL and Congee, not AIHub. The owner selected Codex → XL and accepted the short recreation window.

## Goals / Non-Goals

**Goals:** Add the new alias using existing policy and publish correct model information.

**Non-Goals:** No old route changes, Congee aggregate membership, new metadata code, credential/account edits, image rollout or global refresh-policy changes.

## Decisions

- Copy old Terra direct-model cooldown policy (60s default, 300s maximum, existing status set) and logical routing fields. Replace target IDs only; retain zero retries, one fallback and ordered failover.
- Reuse bundled model metadata through the existing chain. Add only a static 500000/500000 context override, as for old Terra; do not copy older model descriptions/capabilities.
- Keep private desired state private. Change the tracked catalog example only for the public alias and overrides.
- Use documented `deploy-config.yml` operations with explicit runtime selectors established in the prior Axis removal record. No deployment bypass or manual container start.

## Risks / Trade-offs

- [Recreation interrupts in-flight requests] → Owner approval obtained; sample active connections before the window, recreate only selected services, preserve rollback copies.
- [Registration is not inference proof] → Verify exact effective routing and catalog metadata; report paid-generation coverage separately.
- [Metadata inventories can change during checks] → Compare canonical membership and route fields, isolate unrelated metadata churn rather than silently accepting membership loss.

## Migration Plan

1. Keep protected controller backups and a before snapshot of live hashes, catalogs and container identities.
2. Apply additive local edits; assert the full parsed candidate is unchanged except the three new AISIX models and one static catalog entry. Validate configuration and OpenSpec.
3. Review read-only previews for AISIX and model-catalog. Require no unrelated drift.
4. Deploy AISIX then model-catalog. Verify effective Admin routing, exact list additions, 500k context, model information and untouched unrelated container identities.
5. Rollback: restore the protected controller resources copy and preview/apply AISIX; use guarded model-catalog config rollback for its operation. Do not restore over intervening drift. Record UTC timing, results and unmeasured interruption.
