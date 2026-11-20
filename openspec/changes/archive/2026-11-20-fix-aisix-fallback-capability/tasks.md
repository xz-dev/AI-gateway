## 1. Audit and configuration

- [x] 1.1 Audit complete: 26 aggregates (not 25), 573 native IDs, eight missing secondary targets and two out-of-scope primary targets. No Congee sibling fallback precedent.
- [x] 1.2 Remove unsupported fallback references and all non-Claude XL direct declarations, including Luna. Migrate both Fable 5.1 declarations and their reference to 5.2 per the owner's 2026-11-20 decision. Preserve other declarations and normalize fallback budgets.

## 2. Validation and local acceptance

- [x] 2.1 Gate preview and apply on fresh native fallback capability evidence through the existing AISIX validator. Test missing and valid targets and unavailable evidence.
- [x] 2.2 Replay primary 503 and primary cooling against an isolated mock that returns 400 for the retired fallback. Verify the corrected config returns a failover-eligible 502 or 503, never 400, without calling XL; verify Sub2API still returns 5xx to the client.
- [x] 2.3 Complete independent review and required CI.

## 3. Deployment and closeout

- [x] 3.1 Review the guarded private diff and rollback snapshot; activate only AISIX resources and notify the supervisor.
- [x] 3.2 Verify effective resources, only Claude in the explicit XL inventory, Fable 5.2 routing, native registration for all secondary targets, unaffected neighbours and zero drift with zero-token readbacks.
- [x] 3.3 Record the separate status-classification proposal for owner decision without implementing it.
- [x] 3.4 Synchronize, archive, validate all OpenSpec items strictly, push scoped commits and report completion. Archive `e86bb5e` was verified on `origin/main` after all 21 remaining items passed strict validation.
