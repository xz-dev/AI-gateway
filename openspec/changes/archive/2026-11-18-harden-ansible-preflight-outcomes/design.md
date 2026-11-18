## Context

See proposal.md for motivation. Shared detection currently emits a provider string, and runtime derivation takes its first token. The standalone provider therefore becomes an engine name. Existing preflight fixtures force explicit Docker inputs and cannot override those same keyword arguments. AISIX already marks rc127 unchecked, but prints the same failure message for every result.

## Goals / Non-Goals

**Goals:** Retain provider selection and override semantics, distinguish canonical engine identity, fail clearly before empty argv construction, and describe optional command outcomes without leaking their content.

**Non-Goals:** New Podman transaction support, automatic host dependency installation, credential provisioning, route/model equivalence validation or production activation.

## Decisions

- Keep the existing detector and shared argv owner rather than uploading a second runtime library. Normalize the standalone provider to the same canonical engine used by the house shell detector; validate effective command tokens before using `first`. Resolve the provider into an internal fact because empty extra-vars cannot be overwritten by `set_fact`; shared argv, version checks and the existing rollback-renderer prefix consume that fact.
- Keep the Admin execution target and nonfatal receipt contract. Derive diagnostics only from return status, not raw stdout/stderr. Replacing it with another authenticated reader or adding response semantics would require a separate operational contract.
- Extend the existing real-role fixture harness. Controlled executable directories contain every detector command needed by each case, so absent providers cannot fall through to a real host runtime. Use existing collection shims and pinned fake renderer rather than mocking role results.

## Risks / Trade-offs

- Historical normal-auto empty-sequence failure has no isolated reproduction yet → distinguish new demonstrated cases from that private-context evidence gap.
- A successful optional command is weaker than route verification → retain independent effective-route acceptance and say command probe explicitly.
- Fixture PATH isolation can accidentally omit ordinary prerequisites → provide a small explicit fixture tool path rather than falling back to real runtime binaries.

## Observed Offline Findings

- Baseline normal automatic Docker selection passes. The historical private-context normal-auto empty-sequence failure remains unproven.
- Real-role fixtures reproduce a noncanonical standalone engine, an empty-sequence error for explicit whitespace with an empty runtime, and a bare `version --short` execution after empty extra-vars shadow the resolved provider. These are distinct demonstrated source/input cases, not proof of the historical cause.
- The baseline Admin command returns zero and already records checked=true, but reports skipped/failed. Corrected fixtures cover zero, 127 and another nonzero status in apply/check mode; response canaries remain absent from output and receipts. Success still means command status only.

## Migration Plan

No configuration or live rollout is required for this controller-source change. Parent review and CI precede source integration. Reverting the source commits restores the previous behavior; production services, private configuration and credentials are untouched.
