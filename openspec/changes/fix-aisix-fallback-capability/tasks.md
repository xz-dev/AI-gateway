## 1. Audit and configuration

- [ ] 1.1 Compare all explicit fallback targets with native CPA capability and corroborate missing IDs. List all mismatches and record the removal decisions.
- [ ] 1.2 Remove unsupported fallback references and the `xl/gpt-6.2-sol` declaration; preserve other declarations and normalize fallback budgets.

## 2. Validation and local acceptance

- [ ] 2.1 Gate preview and apply on fresh native fallback capability evidence through the existing AISIX validator. Test missing and valid targets and unavailable evidence.
- [ ] 2.2 Replay primary 503 and primary cooling against an isolated mock that returns 400 for the retired fallback. Verify the corrected config returns 503 without calling XL.
- [ ] 2.3 Complete independent review and required CI.

## 3. Deployment and closeout

- [ ] 3.1 Review the guarded private diff and rollback snapshot; activate only AISIX resources and notify the supervisor.
- [ ] 3.2 Verify effective resources, unaffected neighbours and zero drift with zero-token readbacks.
- [ ] 3.3 Record the separate status-classification proposal for owner decision without implementing it.
- [ ] 3.4 Synchronize, archive, validate all OpenSpec items strictly, push scoped commits and report completion.
