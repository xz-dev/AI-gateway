> Historical plan, superseded for desired runtime on 2026-11-09 by the
> [final deployment/cleanup record](../../../docs/records/maintenance-capacity-closeout-2026-11-09.md).
> Preserve the original acceptance history. This plan is not an instruction to
> redeploy its old candidate or rebuild an existing immutable AISIX tag.
> Tasks 5.1–6.2 remain unchecked: the old candidate was not verified as the
> final deployed release; later v0.2.19-based capacity releases do not prove those steps.

## Context

See `proposal.md` for motivation and scope. Production uses upstream v0.2.16 plus ten downstream commits, ending at `ab6b00ed674d9011811bc197cf5932926de7e275`. The selected official baseline is v0.2.19, peeled commit `dece26153621959ccd7635814f4b2d08304e681a`.

The front keeper and WS-to-SSE bridge terminate Responses WebSocket before Sub2API. A read-only API capture found all nine OpenAI API-key accounts in WS mode `off`; eight enable explicit-session forwarding and one does not. Preserve those individual settings. Both error-passthrough rules remain enabled with body preservation. The API returned eight OpenAI groups; this does not inventory soft-deleted groups.

The operator authorized owned-repository release and Sub2API-only deployment, then separately approved official migrations 246/247/248 after isolated verification. Existing session-affinity, client-error and front-bridge contracts are unchanged, so this maintenance uses `skip_specs: true`.

## Goals / Non-Goals

**Goals:**
- Carry the smallest proven-compatible downstream set onto the exact official release.
- Make source, CI, image, migration and production evidence independently traceable.
- Preserve persistent records and unrelated runtime identities through a recoverable application-only rollout.

**Non-Goals:**
- Repairing Codex HTTP/2 resets, changing CPA cooldown/protocol behavior, or tuning memory limits.
- New account permissions, rerank features, routing logic, retry policy, dependencies or network topology.
- Rewriting upstream migrations, bypassing deployment gates or restoring a database without a separate recovery decision.

## Decisions

### Retain eight existing commits; omit only two obsolete keepalive commits

Replay the six production rerank commits `e5874647e6`, `e7a317f013`, `4eccc37971`, `14ceb1b0e4`, `5902f68eac`, `297669f211`, followed by explicit-session propagation `2bc1fc0664` and error-field preservation `ab6b00ed67`. Omit keepalive commits `60c4d5c16b` and `63e761b816`.

Upstream v0.2.19 still lacks rerank, strips or rewrites identity on affected conversational paths, and drops structured fields when error rules match. Removing those fixes would change existing behavior. Do not delete WS-named files wholesale or subdivide the affinity patch merely to reduce its file count. Resolve overlap by retaining upstream changes and applying only the existing downstream behavior.

Use an isolated source worktree outside the gateway edit root. The gateway consumes its reviewed release; it does not own edits to the Sub2API repository. Reuse the existing source tests and owned-fork publication workflow rather than adding a release framework.

### Preserve the complete runtime and publish before touching production

Build and test on the controller/CI, never on the small production host. Preserve the upstream entrypoint, resources, non-root execution, health endpoint and PostgreSQL 18 backup clients/libraries. Verify the actual built artifact, not only its Dockerfile. Use a readable immutable version, source revision and digest; do not reuse a published tag or deploy a locally relabeled binary.

Run relevant rerank/provider/usage/UI, cross-protocol affinity and structured-error tests. Use a controlled negative check to establish that relevant assertions detect a missing behavior; restore the candidate afterward. An independent reviewer checks conflict resolutions, tests and runtime/publication scope before commit/push/publication.

### Apply the approved minimum frontend security updates

The first exact-source CI found three inherited high-severity advisories in the unchanged upstream lockfile: GHSA-83rm-6c8r-g728, GHSA-qr28-fpjm-wq7f and GHSA-mrwp-f3q8-3g9p. The operator explicitly approved repairing them before continuing publication/deployment. Use Vue I18n 11.4.14 and matching Intlify packages, decode-uri-component 0.5.1 and follow-redirects 1.16.2. Vue I18n 9.x has no patched release, so verify existing translation behavior, frontend tests, typecheck and production build across the major-version update. Keep audit policy/exceptions unchanged and require the existing security scan to pass; do not replace remediation with a waiver. This is the only approved dependency expansion beyond the original transplant.

### Use original migrations and an exact data-delta oracle

The complete migration-tree comparison, not GitHub compare's capped 300-file list, identifies three new SQL migrations:
- `246_enable_gemini_image_generation_groups.sql`: enable image generation for matching Gemini/Antigravity groups, including any matching historical rows.
- `247_scoped_admin_api_keys.sql`: create the scoped administrator key table and its deletion marker.
- `248_group_image_count_billing.sql`: add `groups.image_count_billing`, default `false`.

Keep their upstream bytes and existing migration identities unchanged. The operator explicitly approved these effects. Stop for any additional migration or business-state change.

Restore a consistent production dump into an isolated network-disabled PostgreSQL/Redis/candidate fixture with no published ports. Compare every old migration tuple and business-record fingerprints. Allow only those three migration-history additions, the new schema/default field, and migration 246's exact predicate-driven group-flag changes. Accounts, users, client keys and usage records must not disappear or otherwise change. Verify startup, runtime files and representative HTTP behavior in this fixture.

### Keep the production transaction image-only

Only `SUB2API_IMAGE` may change in effective production configuration. Preserve account objects, flags, rules and other image/environment fields. Use a fresh read-only Ansible preview, review its bound digest, then apply the existing Sub2API component transaction. Keep the owner namespace, relays, databases and all other applications unchanged.

Back up configuration and PostgreSQL before activation; retain the old immutable image and both on-host and controller recovery material. Sample active connections before selecting a low-traffic window. Verify exact image/runtime identity, readiness, migration history, business-state deltas, and unchanged neighbouring container identities after activation. Any temporary client key must have deletion and subsequent not-found evidence.

## Risks / Trade-offs

- Upstream overlaps eighteen patched files → review real candidate diffs and run existing behavior tests; do not equate clean patch application with compatibility.
- Migration 246 changes a permission flag → use the approved exact predicate as the oracle, including historical rows, rather than claiming all group records remain identical.
- Memory pressure can distort live checks → keep fixture work off production and use bounded probes; record existing upstream failures separately without waiving required acceptance.
- Runtime packaging can omit resources or backup tooling → inspect and execute the actual published image before any serving disruption.
- Reverting code does not undo migrations → rehearse old-image compatibility with the migrated fixture where feasible; do not silently restore an old dump over newer requests.

## Migration Plan

1. Complete source tests and independent review; publish and verify the immutable candidate and CI receipts.
2. Take protected consistent backups and validate restore plus the exact approved migration/data delta in isolation.
3. Update tracked template pins and the private desired image only; validate OpenSpec, repository checks and the fresh guarded preview.
4. In a low-traffic window, activate only Sub2API and run readiness, data, rerank, HTTP/SSE and front-WS continuation acceptance with bounded requests.
5. Roll back the image for change-caused failures using a fresh guarded rollback plan. Database restoration requires a separate explicit decision and loss accounting.
6. Publish a sanitized receipt, preserve recovery material, finish CI/review and record any unresolved external incident separately.
