# Sub2API v0.2.19 patch maintenance — 2026-11-02

## Scope and current status

Keep rerank, explicit cross-protocol session identity, structured error fields and the existing passthrough error rules. Drop only the two obsolete Responses WS keepalive commits; keeper and the WS→HTTP bridge retain their existing transport roles. No CPA, account, pool, retry, memory-limit, OAuth or routing changes are part of this operation.

Source, image, isolated migration/runtime acceptance and a fresh protected backup are complete. The guarded production rollout and its post-deployment checks are pending; this record does not yet claim Sub2API is upgraded in production. AISIX #1387 is already deployed; see [its separate receipt](aisix-pr1387-2026-11-02.md).

## Accepted source and immutable image

- Upstream v0.2.19: `dece26153621959ccd7635814f4b2d08304e681a`.
- Owned source: `60dbe853f694b5ecb37b0672a98a7d2abf8bf72b`, branch `maintenance/gateway-v0.2.19-trim-ws` in `xz-dev/sub2api`.
- Retained rerank commits: `e5874647e6`, `e7a317f013`, `4eccc37971`, `14ceb1b0e4`, `5902f68eac`, `297669f211`; retained affinity `2bc1fc0664` and error fields `ab6b00ed67`.
- Omitted WS keepalive commits: `60c4d5c16b`, `63e761b816`.
- Image: `ghcr.io/xz-dev/sub2api:0.2.19-pr7157-pr7217-pr8191-60dbe85@sha256:fcf582f2bb50491f46d6e2818ba699f14e2cf001b719592b9c444421cc870336` (linux/amd64 manifest).
- OCI index: `sha256:e516648b6b3028d9b44f7601f70b1f0c192829cac40898e5d9faac789b480e83`.
- Exact-source CI: [CI 41429380679](https://github.com/xz-dev/sub2api/actions/runs/41429380679), [Security Scan 41429380755](https://github.com/xz-dev/sub2api/actions/runs/41429380755), [image 41429380704](https://github.com/xz-dev/sub2api/actions/runs/41429380704): all success, independently read back.

Two macOS ARM attempts never obtained a runner. The final one-line reviewed change uses official `macos-15-intel`, retaining macOS 15/BSD coverage and every original shell command. The Apple-container suite uses a fake CLI, not native ARM execution; all commands actually passed on Intel. No scanner, test or audit exception was removed. Application, SQL and runtime inputs are byte-equal to security revision `0185c92a8c5e94953d191ba37f091b78625fe7ff`.

The approved security delta pins Vue I18n/matching Intlify 11.4.14, decode-uri-component 0.5.1 and follow-redirects 1.16.2. Actual-module translation compatibility tests, frontend checks and independent review passed. The three target advisories are absent. Two pre-existing xlsx high advisories remain covered by unchanged existing exceptions; this is not a zero-vulnerability claim.

## Isolated migration and runtime evidence

The existing consistent dump was restored into PostgreSQL 18.6 with Redis sharing its network-none namespace. Only loopback existed before candidate credentials were loaded; no ports were published.

- Before: 100 business tables, 4,197,604 rows fingerprinted; 296 migration records.
- After: all old migration rows/checksums unchanged; exactly `246_enable_gemini_image_generation_groups.sql`, `247_scoped_admin_api_keys.sql`, `248_group_image_count_billing.sql` added, for 299 records.
- Migration 246 matched zero groups in the complete restored database, including soft-deleted records. It would enable image generation for matching Gemini/Antigravity groups; that effect was separately approved.
- New `scoped_admin_api_keys` table empty; every group has `image_count_billing=false`.
- Authoritative users/accounts/groups/API keys/raw usage/settings fingerprints unchanged: 7 users, 12 accounts, 12 groups, 36 keys, 1,579,595 raw usage rows and 285 settings.
- 81 of the 100 existing table fingerprints unchanged. Nineteen runtime-maintained monitoring, aggregation, watermark, heartbeat, log and consumed-outbox tables advanced. Their writers are the channel-monitor tiered TTL/recompute code, ops metrics/log writers, dashboard/group aggregate recomputation, and `schedulerOutboxRepository.DeleteConsumedUpTo`. These changes are recorded, not silently normalized away; whole-running-database byte identity is not claimed.
- Final `60dbe85` image executed its original entrypoint, reported the exact source/version, ran as uid 1000 under the unchanged 320 MiB limit, remained healthy without OOM, and provided working PostgreSQL 18.6 `pg_dump`/`psql`. Those are the upstream runtime tools; an extra `pg_restore` binary is not part of that image contract.
- Supported admin login and group/account/error-rule reads succeeded on the final image (8 active groups, 9 active accounts, 2 rules); frontend delivery succeeded. No production credential was forged and no external provider was contacted by the fixture.
- Old immutable production image `ab6b00ed674d9011811bc197cf5932926de7e275`, digest `77f4715f0aa7f66dc1364b162ec4ffd6eab7874b0916d5be4e7a4c6b7b389950`, also started with the migrated schema and passed health as uid 1000 with all 299 migration rows retained. This establishes startup/schema rollback compatibility, not a database downgrade or an exhaustive old-version feature test.

## Fresh backup and capacity

A new consistent custom-format dump contains 290,004,983 bytes, SHA-256 `5266145da633583df0aaedcf7f1862a69a2f1b6d6a3a22fa76fce7ad3b3c9f33`. Remote stream and protected controller-copy hashes match; `pg_restore --list` succeeded. Private files are mode 0600 under mode-0700 directories.

The local-dump helper's 2 GiB reserve refused an additional on-host dump. The existing Ansible SSH transport was therefore binary-fidelity checked and used to stream the fresh dump directly to the controller. Current configuration, the post-AISIX 48-container baseline and an old-image rollback alias were separately retained on the host and copied back. Existing backups and rollback images were not deleted. Approximately 1,969 MiB remained free, above the unchanged 1,024 MiB deployment gate; apply still requires its own fresh capacity/identity-bound preview.

## Production rollout and closeout

Status changed after preparation: the guarded Sub2API-only preview attempt failed before any mutation (SSH connection timeout to the production host during the read-only check run); no production change occurred. The owner then renewed the HTTP/2 incident investigation, so the Sub2API production upgrade is **paused and not deployed**; production still runs `ab6b00ed674d9011811bc197cf5932926de7e275` and CPA remains unchanged. The first upstream reset's cause remains unproven; see the recurrence investigation notes. Pending (unchanged gates): current-controller candidate with only `SUB2API_IMAGE` changed, fresh guarded Sub2API-only preview/digest, low-traffic apply, exact image/version/namespace and migration readback, bounded rerank/HTTP/SSE/front-WS and structured-error acceptance, temporary-key DELETE 200 / GET 404 / local-secret removal, unchanged protected neighbours/business settings, final zero-drift preview, and scoped integration review/CI.

Evidence already gathered for those gates: pre-deploy scope review 64e31cb4 approved the public pin diff (`APPROVED_FOR_SUB2API_GUARDED_APPLY`, report at `subagent-artifacts/outputs/05c99ec4-754d-45ba-a234-769127b7d388/reports/predeploy-review.md`), and the full public template validation passed on identical pin bytes (exit 0, ~122 s, log at `/var/tmp/ai-gateway-sub2api-cleanup-20261102/template-validation/validate-template.log`). A fresh protected streamed backup exists (290,004,983 bytes, SHA-256 `5266145da633…`, TOC valid).

HTTP/2 causal comparison remains deferred except as renewed by the owner; neither this maintenance nor the completed AISIX rollout establishes the original reset's cause.
