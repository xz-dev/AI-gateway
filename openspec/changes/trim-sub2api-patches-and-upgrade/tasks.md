> Historical plan, superseded for desired runtime on 2026-11-09 by the
> [final deployment/cleanup record](../../../docs/records/maintenance-capacity-closeout-2026-11-09.md).
> Preserve the original acceptance history. This plan is not an instruction to
> redeploy its old candidate or rebuild an existing immutable AISIX tag.
> Tasks 5.1–6.2 remain unchecked: the old candidate was not verified as the
> final deployed release; later v0.2.19-based capacity releases do not prove those steps.

## 1. Baseline and authorization

- [x] 1.1 Pin upstream v0.2.19 to `dece26153621959ccd7635814f4b2d08304e681a` and inspect the complete migration tree; verify the old migration blobs are unchanged and the only new SQL files are 246/247/248.
- [x] 1.2 Read the production image, account WS/affinity flags and error rules through supported read-only interfaces; verify the baseline Sub2API preview reports `changed=0 failed=0` and record the limited group-inventory scope.
- [x] 1.3 Obtain explicit approval for official migrations 246/247/248 after isolated validation; record migration 246's exact permission effect and preserve the no-additional-business-edit boundary.

## 2. Source candidate

- [x] 2.1 Create an isolated source maintenance branch at the exact upstream commit and apply the eight retained production commits without the two WS keepalive commits; verify the source diff preserves upstream changes and contains no unrelated edits.
- [x] 2.2 Run existing backend rerank, affinity, structured-error and route/billing tests, plus relevant frontend tests/typecheck/build; verify meaningful negative controls fail for the intended missing behavior and the restored candidate passes.
- [x] 2.3 Independently review conflict resolutions, behavior coverage, runtime packaging and publication inputs; resolve all blocking findings and record the accepted source revision and actual checks.
- [x] 2.4 Apply the separately approved minimum frontend security dependency updates; verify the existing audit-exception checker passes without new waivers and frontend translation behavior, tests, typecheck/build remain compatible.
- [x] 2.5 Independently review only the security-remediation delta and rerun its relevant checks; verify the approved backend patch set and publication guard remain unchanged.

## 3. Publication

- [x] 3.1 Commit and push only reviewed source/publication files to the owned fork; publish through its existing CI with an immutable readable version and verify exact-revision CI success plus image source/digest/platform identity.
- [x] 3.2 Inspect and execute the published image's entrypoint, binary/version, resources, health and PostgreSQL backup clients/libraries; verify the complete runtime contract before any production activation.

## 4. Isolated migration gate

- [x] 4.1 Take protected consistent production/config backups and retain the old immutable image; verify backup hashes/readability and protected controller copies without changing live business settings.
- [x] 4.2 Restore the dump into a no-egress/no-published-port PostgreSQL/Redis/candidate fixture; verify only loopback routes/interfaces are available before candidate startup.
- [x] 4.3 Run candidate startup and verify migration history adds exactly approved 246/247/248 while every old migration identity remains unchanged; verify business data retains its fingerprints except the exact approved group-flag/default-field delta.
- [x] 4.4 Verify candidate health and representative API behavior in the fixture, and characterize old-image compatibility with the migrated schema; record any rollback limitation rather than restoring a live database.

The rehearsal preserved all authoritative user/account/group/API-key/raw-usage/settings fingerprints. Of 100 existing table fingerprints, 81 were unchanged; 19 runtime-maintained monitoring, aggregate, watermark and consumed-outbox tables advanced through their existing aggregation/TTL/heartbeat/consumption writers. This is not a claim of whole-running-database byte identity. Final image `60dbe85` also passed supported admin login, group/account/error-rule reads and frontend delivery; old `ab6b00e` passed startup/health with all 299 migration rows retained.

## 5. Gateway rollout

- [ ] 5.1 Update only tracked Sub2API image pins and the ignored private desired image; verify template/repository checks, `openspec validate --all --strict` and a fresh Sub2API-only guarded preview with no unexplained drift.
- [ ] 5.2 Select a low-traffic backed-up window and apply the reviewed preview digest; verify only the Sub2API application identity changes, the exact image is running and readiness succeeds.
- [ ] 5.3 Verify production migration/data deltas, unchanged account/rule settings and bounded rerank/HTTP/SSE/front-WS continuation behavior; verify temporary-key DELETE plus not-found and unrelated-container invariants.

## 6. Closeout

- [ ] 6.1 Write a sanitized receipt with source/CI/image identities, migration approval/results, observed interruption, acceptance, rollback and known limitations; verify no private configuration or temporary artifacts enter tracked changes.
- [ ] 6.2 Commit/push the scoped gateway integration and documentation changes, complete relevant independent review/CI and final drift checks; verify all required tasks have evidence and leave unrelated incident work explicitly separate.
