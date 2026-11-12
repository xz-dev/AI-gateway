## Context

See proposal.md for the requested behaviour change. CPA currently tests cgroup headroom before each growth charge. The operator's repeated failures all involve a 9 MiB step. The previously deployed `GOMEMLIMIT=384MiB` mitigation did not prevent recurrence.

## Goals / Non-Goals

**Goals:** Remove the CPA-specific memory refusal at the shared accounting seam, preserve release/reclamation and all non-memory protections, and ship a reversible CPA-only replacement.

**Non-Goals:** Remove keeper/AISIX/Sub2API gates, increase container limits, add another switch, change routing/accounts/plugins, or claim that available physical memory is unlimited.

## Decisions

1. Make CPA memory charging accounting-only. Keep its callback signature and atomic counters so existing ingest/fallback callers and cleanup continue unchanged. Delete headroom checks and the refusal constructor rather than returning success from an unused alternate path.
2. Remove startup cgroup-file validation. Retain the legacy configuration identity for reload compatibility without reading or trusting it as admission authority.
3. Replace obsolete refusal tests with regression cases using the exact reported available-byte values, concurrent accounting/release, and missing cgroup files. Keep disk, inline, ticket and spool tests unchanged.
4. Reuse the deployed immutable image as runtime base and replace only the compiled binary. Keep the toolchain/build mode compatible and label the source provenance. No unrelated image refresh.
5. GHCR rejected the existing token's write scope. The operator explicitly approved offline deployment instead: export a Docker image archive, verify controller/host SHA-256 and the exact loaded image ID, and use the existing guarded preload-only component profile. Do not retry publication with another credential or replace registry authentication.

## Risks / Trade-offs

- [OOM or latency under real overload] → Keep container hard limits and Go target unchanged; verify healthy operation and OOM counters after rollout. Do not reinterpret an OOM as a passing memory test.
- [Stale planning expectations] → This change explicitly overrides only CPA's application memory gate in `large-request-capacity`; retain the old document as historical rationale and reconcile the exception before archival.
- [Broken request cleanup] → Preserve accounting and reclamation, and test concurrent release and existing request lifecycle integrations.

## Migration Plan

Reproduce the reported refusal in a failing regression, apply the narrow deletion, run unit/integration/build checks, transfer the verified fixed image through the operator-approved offline preload path, preview and apply only CPA, then verify image/environment/resources, neighbours, original-path requests and OOM state. Rollback uses a fresh guarded preview of the saved CPA snapshot. A rollback restores the old gate and therefore can restore these 503s; use it only for change-caused operational failure.
