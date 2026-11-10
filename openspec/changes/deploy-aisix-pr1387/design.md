> Historical plan, superseded for desired runtime on 2026-11-09 by the
> [final deployment/cleanup record](../../../docs/records/maintenance-capacity-closeout-2026-11-09.md).
> Preserve the original acceptance history. This plan is not an instruction to
> redeploy its old candidate or rebuild an existing immutable AISIX tag.

## Context

See proposal.md for motivation. Production currently uses the official `1.8.0-rc.1` image at index `sha256:ebcd2bbba9b7a3cc7cfb1318e4d3ba64cf9f93cbf2b19b8486ead6093da1ba5a`, source `fa89849e6ce0f60bba15a998f1757e481f74ad7a`. PR #1387 is pinned to `06e8075266119380844aa4e9fcb9fb80468715eb`.

## Goals / Non-Goals

**Goals:** deliver that allocation fix through an owned, source-identifiable runtime image and an AISIX-only guarded image replacement.

**Non-Goals:** HTTP/2 causality experiments, transport/cooldown changes, route/account/pool changes, memory tuning, database migrations or restarting neighbouring services. The existing Sub2API change remains separate and resumes after this rollout.

## Decisions

- The fork Actions workflow was unregistered and dispatch returned 404. Use the already-active AI-gateway `publish-aisix-images.yml` on the owned `maintenance/aisix-pr1387-deploy` branch instead. Its builder fetches and verifies the exact PR head and invokes that source's unchanged `scripts/build-image-binary.sh thin`. Runtime packaging retains upstream Debian trixie, certificates, tini, file capability, numeric uid 10001 and entrypoint. No application/dependency/source edits, instrumented binary or production compilation.
- Build only production's native linux/amd64 architecture. Publish `ghcr.io/xz-dev/ai-gateway-aisix:1.8.0-pr1387-06e8075-r2` and the existing per-publisher-SHA tag; deployment pins its accepted SHA256 digest. Keep AISIX PR/main, releases, `latest`, `dev` and upstream version tags untouched.
- Check version-tag absence after authenticated GHCR login and before publication. Permit only complete, reference-bound known missing-manifest diagnostics with exit 1. Existing tags and every unknown/auth/transport error stop publication. A tested exact allowlist replaces the old gateway workflow's fail-open lookup.
- Reuse existing relevant functional CI on the unchanged PR head; independently review the three publication files and verify the actual image's source label, exact binary version, uid and network-none standalone `/livez`. The smoke fixture includes a valid resources source. Independently inspect startup with private production configuration before activation. Do not rerun the entire functional workspace for packaging-only changes.
- Prepare a protected configuration copy adopted from live bytes, not public defaults. Change only `AISIX_IMAGE`; use a fresh `component=aisix --check --diff` plan and its approval digest for activation.

## Risks / Trade-offs

- **AISIX recreation interrupts its active upstream streams** → sample the existing connection/readiness signals, use a low-traffic window, and leave keeper, bridge and namespace owners running.
- **A new image may fail with existing private configuration** → validate startup in a controller fixture with no external network or published ports before the live switch; retain the exact old image and configuration.
- **Registry, architecture or publication identity ambiguity** → fail closed; require a green exact-revision image workflow, accepted index/amd64 identities, source labels and binary version. Do not overwrite tags or bypass authentication.
- **Low production disk headroom** → check capacity again before pulling/activation; no automatic pruning of data or rollback images.
- **Concurrent/pending controller state** → use a separate private candidate configuration directory and preserve Sub2API work. Adopt only the reviewed resulting AISIX pin back into the main controller configuration.

## Migration Plan

1. Save mode-0600 live configuration and all-container identities under a mode-0700 operation snapshot, retain the old image reference/ID and rollback tag, fetch a controller copy and verify hashes.
2. Publish/review the candidate, verify its runtime identity and isolated startup, then update only the candidate's AISIX image pin.
3. Obtain a fresh guarded preview and capacity check. Recreate only AISIX through the normal component operation. Verify image/environment/readiness/namespace and unchanged neighbouring identities; use the normal bounded acceptance signals without large production payloads.
4. On a change-caused failure, use the guarded rollback only after a fresh rollback preview proves transaction ownership and current identities. Unknown state stops for reconciliation rather than blind restoration.
5. Adopt the accepted image pin, write the sanitized receipt, validate OpenSpec and resume the existing Sub2API maintenance. No database migration is part of this AISIX change.
