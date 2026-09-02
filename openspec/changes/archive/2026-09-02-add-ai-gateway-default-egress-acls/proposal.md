## Why

Fresh AI-gateway installs enable several CPA and Sub2API control-plane refreshers while copying an egress policy that denies every destination. This mismatch breaks built-in maintenance flows such as the CPA Management Center download chain, model and pricing refresh, and Codex client version synchronization, and encourages unsafe ad-hoc GitHub wildcard rules.

## What Changes

- Replace the empty fresh-install egress policy with a minimal, fail-closed control-plane baseline derived from the repository's generated application configuration and its digest-pinned CPA and Sub2API source versions.
- Allow only exact `GET` requests over TLS bump for required automatic metadata and asset flows, plus narrowly scoped read-only CPA and Sub2API version checks.
- Model the complete CPA Management Center download chain: release metadata, repository-specific `management.html` download, and the repository-ID-scoped GitHub release asset redirect.
- Keep provider inference, OAuth, plugin stores and plugin assets, unverified fallback mirrors, Sub2API binary update or rollback assets, and user-defined destinations outside the default policy.
- Document default control-plane rules separately from optional management rules and provider- or deployment-specific rules.
- Preserve existing runtime policy ownership: initialization continues to copy the example only when no runtime policy exists, and repository upgrades do not merge into or overwrite `data/egress-proxy/policy.json`.
- Add validation that the tracked default policy renders successfully, contains the required exact baseline, and does not introduce broad GitHub repository or release-path wildcards.

## Capabilities

### New Capabilities

- `default-egress-policy`: Defines the fail-closed fresh-install outbound policy, exact built-in control-plane allowances, optional-rule boundaries, and preservation of existing runtime policy.

### Modified Capabilities

None. This repository has no existing OpenSpec capability for outbound egress defaults.

## Impact

- Planned implementation files: `egress-proxy/policy.example.json`, `README.md`, and `scripts/validate.sh`.
- New installations receive the exact control-plane baseline when `scripts/init-egress-proxy.sh` first creates the runtime policy.
- Existing installations and production runtime policy remain unchanged until an operator explicitly merges approved rules.
- No provider endpoint, private override, local ZCode service, dynamic plugin repository, new dependency, image change, or production deployment is included.
