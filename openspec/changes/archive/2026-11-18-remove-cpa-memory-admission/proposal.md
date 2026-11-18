## Why

CPA again rejected ordinary Pi requests with `503 insufficient_memory` after the GC-target mitigation. The operator explicitly requests removing this application-level CPA memory gate rather than further tuning its thresholds.

## What Changes

- **BREAKING**: CPA no longer rejects requests because of cgroup memory headroom, outstanding memory charges or a 32 MiB application margin.
- Keep request memory accounting solely for balanced release and post-request reclamation.
- Stop requiring readable cgroup memory files to start CPA body spooling.
- Preserve container RAM/swap/CPU limits, current Go target, body/inline/fallback size limits, disk checks, ticket checks and every other hop's admission behaviour.
- Build a fixed replacement image and deploy only CPA through the guarded component rollout. After GHCR refused the existing write scope, the operator explicitly approved offline archive transfer with SHA-256/image-ID verification and the existing preload-only profile.

This request supersedes the CPA memory-admission portion of `large-request-capacity`; it does not cancel that change's remaining work or relax other hops.

## Capabilities

### New Capabilities

- `cpa-request-admission`: CPA accepts otherwise valid requests without an application-level memory-pressure refusal, while retaining existing non-memory rejection contracts.

### Modified Capabilities

None.

## Impact

CPA fork `internal/bodyspool/charge.go`, runtime startup and regression tests; CPA image references and private deployment inputs; operational verification and rollback evidence. There are no new dependencies or account/group changes. The container can now reach its hard OOM limit instead of rejecting early.
