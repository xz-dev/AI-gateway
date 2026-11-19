## 1. Baseline and contract

- [ ] 1.1 Strictly validate and commit these planning artifacts before implementation; record the commit and owner-approved scope without changing the three unrelated open changes.
- [ ] 1.2 Reconfirm the exact deployed fork/image and clean isolated source baseline; inspect all retry, failover, side-effect and Responses/Chat writer call sites against that revision and record the finite classifier contract.

## 2. Shared classification and mock acceptance

- [ ] 2.1 Add failing synthetic HTTP-mock coverage for policy 400/403 × Responses/Chat; assert exact safe code/type and fixed message, one upstream attempt despite available retries/alternate accounts, and zero account-health/cooldown/disable side effects.
- [ ] 2.2 Implement the shared finite structured classifier and protocol output before retries and account penalties; verify the mock matrix passes without broad 403 passthrough or raw upstream error text.
- [ ] 2.3 Cover code-absent policy types, missing/unsafe types, unknown/conflicting codes, malformed/plain-text errors and credential/endpoint 403; verify safe fallbacks and unchanged non-policy behavior with meaningful negative controls.
- [ ] 2.4 Verify existing cyber-policy, capacity and credential regressions, plus committed-stream terminal policy errors for the selected protocols; run focused race tests, relevant backend suites, vet and lint, recording unavailable checks honestly.

## 3. Publication, compatibility and review

- [ ] 3.1 Commit/push only the scoped fork changes and publish through existing fork CI/image workflows; verify all required checks and exact source/digest/platform/runtime identity before accepting a candidate.
- [ ] 3.2 Run the established compatibility ladder with harmless mock upstreams, produce a passing receipt and update only the reviewed Sub2API lock/image inputs; verify no unrelated component pin or capacity behavior changes.
- [ ] 3.3 Obtain fresh-context independent review of the exact fork diff, mock assertions and gateway integration; resolve blocking findings and record the accepted revision and limits before deployment.

## 4. Guarded rollout and closeout

- [ ] 4.1 Read a fresh Sub2API-only guarded preview, verify the candidate is image-only with no unexplained drift, preserve protected rollback state and use the reviewed approved digest for apply.
- [ ] 4.2 Verify only the intended application identity changes, readiness/image/business-setting invariants and zero-token observations; preserve public APISIX 401/404 opacity and perform no real policy-content inference, temporary route or key creation.
- [ ] 4.3 Record source/CI/image/lock/review/deployment evidence, actual interruption, rollback and limits; distinguish mock policy acceptance from production zero-token checks, and notify the supervisor of deployment completion.
- [ ] 4.4 Validate implementation against this change, finish only evidenced tasks, synchronize this delta and archive; run `openspec validate --all --strict`, commit/push the scoped integration and notify archive completion. Decide on an upstream PR only after downstream acceptance, without making it a deployment prerequisite.
