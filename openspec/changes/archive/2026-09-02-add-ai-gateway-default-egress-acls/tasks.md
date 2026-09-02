## 1. Default Policy Contract

- [x] 1.1 Replace the empty CPA and Sub2API destination lists in `egress-proxy/policy.example.json` with the exact service/domain/TLS/method/path maps from `design.md`, then verify `python3 scripts/render-egress-policy.py egress-proxy/policy.example.json <temporary-output-directory>` succeeds.
- [x] 1.2 Extend `scripts/validate.sh` to render the tracked example and compare its canonical service/domain/TLS/method/path map with the required baseline, including rejection of missing, extra, or broadened GitHub rules; verify the validation fails against a temporary broadened in-memory policy fixture and passes against the tracked example without network access.

## 2. Operator Documentation

- [x] 2.1 Update README installation and egress-policy wording from an empty deny-all default to a control-plane-only default, list every enabled automatic and read-only version rule, and verify each documented domain and path exactly matches `egress-proxy/policy.example.json` without including release-asset signed queries.
- [x] 2.2 Document optional management, plugin, fallback-mirror, provider, private-sidecar, and Sub2API rollback boundaries plus the manual merge procedure for existing runtime policies; verify the README does not claim repository upgrades modify `data/egress-proxy/policy.json` and directs Sub2API upgrades through pinned images rather than in-container assets.

## 3. Verification and Scope Control

- [x] 3.1 Run `./scripts/validate.sh` and verify the default example, synthetic security policy, generated Compose configuration, and repository safety checks all pass.
- [x] 3.2 Run `./scripts/test-egress-proxy.sh` and verify existing host, method, path, DNS, private-address, and fail-closed proxy scenarios still pass without adding live GitHub probes.
- [x] 3.3 Run `openspec validate add-ai-gateway-default-egress-acls --strict`, inspect `git diff --check` and `git diff --name-only`, and verify implementation changes are limited to `egress-proxy/policy.example.json`, `README.md`, and `scripts/validate.sh` with no edits to runtime `data/`, initialization merge behavior, Compose topology, provider rules, private overrides, or production state.
