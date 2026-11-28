## 1. Inventory and verification

- [x] Inspect main and production; account for every image group and note unchanged groups.
- [x] Verify the exact APISIX, cloudflared, ZCode, PostgreSQL, and Redis candidates, including rollback and persistence gates where applicable.
- [x] Port CPA, Sub2API, and AISIX; publish verified candidate images without pushing AI-gateway main.
- [x] Run the combined compatibility ladder; promote only its generated, passing lock. Validate tracked template configuration and private scoped previews.

## 2. Promotion and rollout

- [x] Confirm the exact scope, interruption windows, verification traffic, and recovery steps; push the complete tested selection to `main` once (`477c917`).
- [x] Apply each candidate using its fresh guarded plan or reviewed coupled/manual window. Test readiness, adjacent identities, and approved end-to-end probes after each apply. Eight services passed; ZCode was rolled back and the operator approved retaining 4.9.0 for this round.
- [x] Compare final live and intended identities, record timings and any failed steps or rollbacks, and run `openspec validate --all --strict`. See [the production record](../../../../docs/records/component-update-2026-11-27.md).
