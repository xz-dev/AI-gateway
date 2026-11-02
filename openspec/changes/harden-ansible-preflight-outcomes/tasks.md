## 1. Reproduction and fixture safety

- [x] 1.1 Allow normal variable overrides in the preflight fixture and add isolated fake-provider cases; retain real-role red logs for canonical standalone engine and empty-provider diagnostics before role edits.
- [x] 1.2 Extend the existing AISIX fixture with controlled optional-probe statuses and secret canaries; retain a red success-diagnostic case before role edits.

## 2. Minimal controller-source fixes

- [x] 2.1 Correct canonical engine derivation and named empty-provider refusal; verify the Docker/Podman/override/missing/check-mode cases in test-ansible-preflight.py pass without real runtime calls or target mutation.
- [x] 2.2 Correct sanitized optional Admin diagnostics without changing its command, nonfatal receipt or check-mode contract; verify test-ansible-aisix-mode.py cases pass and no canary reaches output.

## 3. Candidate validation

- [x] 3.1 Run both complete affected fixture scripts, pinned production lint, playbook syntax checks and openspec validate --all --strict; retain complete logs and real exit codes.
- [x] 3.2 Confirm the uncommitted diff contains only scoped source, tests and change artifacts, with no staged files; report the historical reproduction limit, independent review and pending CI without claiming a live rollout.
- [x] 3.3 Remove the existing tag-filter fixture's skipped-log dependency and close its HTTP server socket; rerun all preflight methods under both CI-equivalent and explicit production configuration without ResourceWarning.
