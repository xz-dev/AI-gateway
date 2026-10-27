# Upstream-image migration evidence

Status: isolated state/interface checks completed below; **not production acceptance**. The user subsequently authorized the selected core versions and models-sidecar deployment, with routine technical decisions handled autonomously. That authorization and version-risk acceptance are not recorded as tests passed.

## Selected version tags

- CPA: `docker.io/eceasy/cli-proxy-api:v7.2.152` (binary reports `v7.2.152`, commit `c76dfd4`).
- Sub2API: `docker.io/weishaw/sub2api:0.2.1`.
- Published linux/amd64 and linux/arm64 tags were verified. Both images are cached locally in Podman and remotely in Docker.
- Compose defaults and `.env.example` render these exact version tags. No local override exists. No digest pinning was introduced.
- At this evidence checkpoint, production still has the original 47 running services; core services are healthy. CPA restart count is 0; Sub2API's historical count is 3, not a new candidate event.

## Synthetic CPA synchronization checks

A disposable local container used `--network=none`, a read-only root, dropped capabilities, no host ports, and synthetic credentials/configuration in tmpfs. No production data was mounted.

- `/bin/bash`, `/bin/sh`, the CPA binary and system CA bundle exist; working directory is `/CLIProxyAPI`.
- `cargo build --locked` and static Go `tests/real_cpa.go` probe build passed.
- The existing real-CPA probe passed in the upstream image: enabled forwarding; optional disabled transient forwarding without activation; five managed kinds' models-only update/readback; other-field/unmanaged preservation; repeat zero writes; manual-edit replacement; restart persistence; intentionally filtered-empty configuration.
- Test limits were 480 MiB memory/swap, 1.75 CPU and 128 MiB tmpfs. These are test limits, **not measured production resource acceptance**.

## Actual plugin/config/auth restoration checks

Fresh management inspection identified two enabled production libraries: `model-router-v0.4.2.so` and `cpa-quota-estimator-v0.5.2.so`. Upstream v7.2.152 explicitly supports dynamic-library plugins. An earlier anonymous old-fork GitHub comparison returned 404; it was not used as evidence of missing behavior or as a reason to stop other work.

Using private restored config/auth, consistent SQLite snapshots, unchanged libraries and the actual external entrypoint/mount/CA configuration, isolated old and upstream CPA instances both:

- started with no external network;
- registered both plugins with `effective_enabled=true`;
- served authenticated plain and parameterized native catalogs;
- returned 401 for invalid authentication;
- preserved configured and plugin-published identities;
- did not report container OOM kills.

The initial all-identities-equal assertion exposed a real **upstream native Codex registry update**, not a configured-channel loss: `gpt-5.4` and `gpt-5.4-mini` (bare and `codex/` forms) disappear; `gpt-6-astra` (bare and `codex/`) appears. Catalog size changes 133→131. Exact upstream `internal/registry/models/models.json` at v7.2.146 and v7.2.152 confirms those Codex plan changes. Configured `zakk/gpt-5.4` and `zakk/gpt-5.4-mini` remain. The test was changed to assert exactly this sourced native delta and reject every other membership change; no OAuth/config workaround was added.

Evidence: `/tmp/cpa-upstream-versions.ELRkmS/cpa-native-inventory-delta.json`, `cpa-upstream-plugins-result.json`, `test-cpa-plugins.py`, and `local-http-probe.go`.

## Sub2API database/interface/recovery checks

A consistent production database backup was restored into an isolated local PostgreSQL 18.6 container on an internal network. No live production volume was attached. Sub2API used its original 256 MiB limit; probes ran within its network namespace rather than relying on host publication of an internal-network container.

The following all passed:

1. Restore the PostgreSQL custom-format backup.
2. Start old `0.1.185`, then upgrade the copy to `0.2.1`.
3. Compare selected account/group/mapping/key identity and configuration columns: 6 accounts, 6 groups, 5 account-group rows and 10 API keys remain identical.
4. Both versions reject missing/invalid credentials with 401. The approved valid key gets 200: plain catalog 3 identities; parameterized catalog 92 identities with equal identity hashes. Native JSON bytes differ, so this is not a claim of byte-for-byte equality.
5. After reviewing the eight additive SQL changes, write a marker to the **sacrificial migrated copy**, start the old binary against that copy, and prove startup, the same directory/auth checks and preservation of the post-upgrade marker.
6. Separately recreate only the isolated database, restore the original backup, and verify the old binary and the same directory/auth/mapping checks again.

Exact Git trees contain 273→281 SQL files: eight additions, no changed old migration files and no removals. Added columns/indexes/defaults were inspected, including the positive constraint on the newly added reasoning multiplier. Source inspection alone did not count as restore or compatibility proof.

No inference replay was performed. These checks cover directory/auth/data compatibility, not successful upstream HTTP/WS inference or long-term load/OOM closure.

Evidence: `/tmp/cpa-upstream-versions.ELRkmS/sub2api-restore-result.json`, `test-sub2api-restore.py`. Private logs and cloned data remain under a mode-0700 task directory on disk, not in the repository.

## Backup and production recovery procedure

Initial private online backup: `/root/AI-gateway/.upstream-backup-20260906T153900Z`:

- PostgreSQL `pg_dump` custom archive: 74,580,509 bytes, restored successfully in isolation.
- Config/auth/application/library/CA archive: 120,127,987 bytes.
- Two SQLite online-backup snapshots, each `PRAGMA quick_check=ok`; restoration into the isolated CPA instances passed.
- Original `.env`, Compose/override, effective private configuration and sanitized 47-service lifecycle/cgroup baseline.

This online backup is not the final quiesced cutover recovery point. Before replacement, capture fresh state, briefly stop the selected writer/service when taking its final state backup, and verify no unreviewed configuration drift. Change only selected image-version references; do not copy the whole dirty local Compose over production.

For this tested version pair, an emergency Sub2API image rollback may retain the additive migrated database, preserving subsequent writes; that behavior was tested only on a disposable copy before production use. Keep original app configuration available. Do not automatically restore an old database over new writes. If the actual migration differs from the tested set or backward compatibility fails, stop the affected writer, preserve a fresh post-cutover dump and state, and do not destructively restore without an approved preservation plan. Recovery to an old custom CPA image is an emergency recovery, not successful upstream migration.

Before each production stage, monitor all related services, including Sub2API and CPA. Immediately validate health, actual images/plugins, authenticated catalog and authorization/resource boundaries; only then observe natural scheduler rounds. On unexpected OOM, restart, authorization or catalog regression, recover the affected stage rather than waiting another refresh round. Keep the version and sidecar stages separate.

## Remaining evidence

Task checkboxes remain authoritative. Successful upstream inference routing/WS behavior and full-chain candidate resource acceptance are not proved by these tests. White-list/source-chain implementation and deployment still need their own evidence. The historic Sub2API OOM cause and long-term memory behavior remain unresolved; neither user version confidence nor this upgrade claims to fix them.
