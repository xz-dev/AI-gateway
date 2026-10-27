# CPA core upgrade — 2026-10-12

> Historical receipt: versions, verification, and recovery material below describe
> this rollout's closeout. See the [2026-10-15 receipt](component-upgrade-2026-10-15.md)
> for later upgrades and production cleanup. The on-host backups and rollback
> references listed below are no longer available.

## Delivered scope

The user explicitly requested upgrading CPA after the two known Responses
interrupt timing risks were disclosed. Only `cli-proxy-api` was recreated.
No credential, routing, quota configuration, or other component was changed.
No Git commit or push was performed.

- Before: `v8.0.22-pr6189-c718f4d`.
- After: `v8.0.24-pr6189-pe8f773dd3e43`.
- Upstream source: `091ff27f31285ec4fd1dd5823b44f7f51fb8f48b`.
- Existing production PR6189 patch retained without unrelated code changes.
- Production image ID: `sha256:fecb741b40c0c4a7deb4c5ed729c80c5865434c041c5f92a2b1a02dd5093565e`.
- Binary SHA256: `8c16d6d372af3d556f3f4aabfaf13e1317bbd4eea16ba9b1954d7bae79e75acb`.
- Desired image is pinned by image ID in `ansible/private-config/.env`.
- Deployment used Ansible drift gates and a preloaded-image profile, with no
  registry pull and no full-stack restart.

## Verification

- CPA version confirmed from startup log; healthcheck `healthy`; restart count 0.
- 48 managed containers running; 11/11 configured healthchecks healthy.
- Container identities changed for `cli-proxy-api` only.
- `.env` changed for `CLI_PROXY_IMAGE` only; local and remote copies match.
- Both Compose files and current production CPA config stayed unchanged.
- Authenticated CPA `/v1/models`: HTTP 200, 325 models.
- Existing quota 0.19.6: registered and effectively enabled.
- Quota `image_quota_scopes_v1` marker remains `complete`.
- An offline, network-disabled new-core probe used copied production-layout
  configuration, the existing quota library, and a database copy. Registration
  passed; SQLite integrity was `ok`; all 1,267,954 copied usage rows remained.

## Tests and known limits

PR6189 xAI image tests passed 10 rounds. Original test output was retained.
The full selected handlers/executor test run did **not** report all green:
`TestClaudeExecutor_ToolSearchDisabledMatchesDevGolden` failed because its
fixture hardcodes 2026-10-11 while the generated reminder uses 2026-10-12.
An independent JSON comparison verified both failing cases differ only in
`messages/0/content/0/text/currentDate`; every other field is equal. Neither
source tests nor golden fixtures were edited to manufacture a pass.

The previously disclosed Responses interrupt races remain unresolved. Earlier
normal-concurrency checks of upstream 8.0.24 reproduced them; a later single
package run not reproducing them is not evidence of a fix.

No paid inference was sent. The checks do not establish full public inference,
SSE/WS regressions, long-term stability, or database restore success.

## Rollback material

Protected directory on the production host:
`/root/AI-gateway/.ai-ops-state/cpa-upgrade-20261012`.

- Baseline container identities and configuration checksums.
- Private configuration/auth/library archive (mode 0600).
- Consistent quota SQLite backup: 553,222,144 bytes, integrity `ok`,
  1,267,954 usage rows; repair marker `complete`.
- Old image retained as `rollback-20261012/cli-proxy-api:before`.
- Final sanitized receipt: `final-receipt.json`.

No database was restored and no restore rehearsal was performed. A core rollback
would change the CPA image pin and recreate CPA only; database restoration
requires a separate explicit decision.

Local build, test, transfer, and Ansible evidence is retained under
`/var/tmp/ai-gateway-upgrade-cpa-20261012/`. WAN transfer timeouts did not trigger
production rollback. The verified binary was transferred resumably and the
production candidate built against the already-cached old runtime.
