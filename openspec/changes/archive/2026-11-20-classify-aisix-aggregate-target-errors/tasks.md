## 1. Reproduce and repair on the AISIX fork

- [x] 1.1 Run local HTTP mocks on `67343036`: record ordered attempts, primary 503 then missing backup, and confirmed cooling primary then missing backup. Establish the failing 400s and capture the unchanged public unknown-model, policy 400/403, and all-unavailable controls.
- [x] 1.2 Implement aggregate-only classification on the fork. Run the same cases on the fix, verify safe 502 or native unavailable 503 as applicable, and compare policy status and envelope. Verify ordinary unrelated 400 and a successful fallback remain unchanged.
- [x] 1.3 Disable just the new classification in a disposable mutant; show both routing-failure tests fail for the expected reason. Restore the candidate and rerun the focused suite green.

## 2. Publish and admit the image

- [x] 2.1 Verify the fork push remote, submit the tested revision to its existing CI/publisher, and verify immutable image digest, platform and source revision at the registry.
- [x] 2.2 Exercise the published image against the HTTP mocks and unchanged compatibility ladder. Generate the lock from the passing run; verify only the AISIX image selection changes and unrelated pins/limits are preserved.
- [x] 2.3 Obtain independent review of the exact source SHA, digest, lock and acceptance/mutant evidence. Record a passing gateway CI run before deployment.

## 3. Deploy and close

- [x] 3.1 Capture the protected baseline and obtain a fresh AISIX-only guarded preview. Verify preserved routes/resources (`671fc1e6…`), old digest, 47 neighbours and rollback readiness before apply.
- [x] 3.2 Apply only that preview and verify image revision, readiness, zero-token status/readback, unchanged neighbours/configuration and clean follow-up preview. Record timing and failure/rollback disposition without private data.
- [x] 3.3 Validate strictly, archive the change after green CI, and verify the pushed archive and synced routing spec on the remote. Verified archive `b47a3c8` on `origin/main`; all five scenarios synced and all 21 remaining OpenSpec items passed strict validation.
