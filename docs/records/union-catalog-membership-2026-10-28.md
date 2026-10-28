# Union catalog membership deployment — 2026-10-28

## Outcome

Production runs the ordered model-catalog image and official CPA v8.0.45.
`routing.force-model-prefix` is false in the Management API and persisted YAML;
`upstream.codex.stream-bootstrap-buffering` remains true. Both services are
healthy with zero restarts. Recovery timer is active and model-sync is unpaused.

**Production verification complete:** the successful Devin inference deferred
under tasks 3.3 and 3.4 passed after the daily reset at 08:06:46 UTC. The earlier
cooldown responses were not counted as success. The fresh probe and credential
identity evidence are recorded under "Devin success after reset" below.
Archiving remains a separate supervisor action.

## Immutable images and repository evidence

| Component | Image | Index digest |
|---|---|---|
| model-catalog | `ghcr.io/xz-dev/ai-gateway-model-catalog:v1.2.0-adfc8ee` | `sha256:07167e38627fc9e8c4681f8feb2906dceff1e03f6cac65f79e111cb51e9df18a` |
| CPA | `docker.io/eceasy/cli-proxy-api:v8.0.45` | `sha256:ce9583e43fb56e632a279b79e6203bbdedc34f09900a60fc0ced0e3f255bc63c` |
| CPA rollback | `ghcr.io/xz-dev/cli-proxy-api:v8.0.38-pr6760-47749f441ae5` | `sha256:89ea898082d5f38d5ad3a786635964cd54e14996ac359402fe2e5fc8ccd399e9` |
| catalog rollback | `ghcr.io/xz-dev/ai-gateway-model-catalog:v1.1.0-548d855` | `sha256:79659b983870744b1158cb51513185c9f1f37359a83daa610bb9e34fa452720d` |

- Implementation: `adfc8ee`; image pins: `4a59f44` (unsigned commits).
- Validate runs `40627466174` (implementation) and `40629013002` (pins) succeeded.
- Publish run `40627633983` succeeded; pulling by the pinned digest succeeded.
- `ops/scripts/validate.sh .env.example` passed with the local Compose override
  temporarily moved to `/var/tmp` and restored afterward.
- Workflow dispatch preceded implementation CI completion; deployment waited
  until both implementation and pin CI were green.
- Final `openspec validate --all --strict`: 21 passed, 0 failed.

## Timeline and counts

All times below are UTC on 2026-10-28. Private captures contain complete lists,
rows, registrations and container identities; the table reports counts only.

| Capture | Native CPA | AISIX | Internal catalog / Codex | Public standard / Sub2API | Public Codex | Passthrough |
|---|---:|---:|---:|---:|---:|---:|
| Refreshed pre, 04:17 | 332 | 92 | 367 | 367 | 358 | 47 |
| Batch A re-run, 04:27 | 332 | 92 | 367 | 367 | 358 | 47 |
| Batch B before AISIX removal, 04:44 | 322 | 92 | 358 | 358 / cached public 367 | 349 | 47 |
| Batch B stable, 04:54 | 322 | 91 | 357 | 357 | 348 | 47 |
| Batch C, 04:56 | 554 | 91 | 357 | 357 | 348 | 47 |

Batch A guarded apply ran 04:17:35–04:26:40; CPA batch B ran
04:31:07–04:41:04. AISIX config preview passed and its apply ran
04:44:34–04:48:06. The prefix setting was disabled at 04:55:47, followed by a
successful diagnostic force refresh. Public standard-list caching briefly kept
the prior count; its stable capture contains only the expected ledger changes.

The native list after C includes 157 IDs without `/`. Those extra CPA originals
are not leaked into catalog membership. Internal standard and Codex lists follow
bare-ID-first, case-sensitive Unicode code-point order. Every internal Codex row
has `priority` equal to its 1-based position. Public Codex intersection keeps that
relative order; entitlement removal can leave gaps in its priorities. The
unparameterized public standard list is answered by Sub2API and retains its order.

A re-run comparison proved:
- A: equal member and passthrough sets; every Codex field except `priority` equal.
- B: exact ledger changes below; passthrough set unchanged; every surviving row
  outside the temporary old-DeepSeek source switch equal except `priority`.
- C: exact member and passthrough sets and every non-priority field equal to the
  stable B baseline; output order and priorities satisfy D7.

## Source-proven Devin ledger

Source: `router-for-me/CLIProxyAPI`,
`internal/registry/models/devin_models.json`, tags v8.0.38 (`0db19025`) and
v8.0.45 (`ad8bdd3f`). Normalizing namespace/case and adding the built-in
`devin/swe-1-6-slow` yields 57 old and 47 new IDs: exactly +7/-17.
Observed native differences equal these sets, with no extra native change.

Added:
- `devin/claude-haiku-5-5`
- `devin/claude-sonnet-5-5`
- `devin/deepseek-v4-1-flash-1017`
- `devin/gemini-4-argon`
- `devin/gpt-6-1-sol`
- `devin/muse-wonder-1-0`
- `devin/swe-2-lightning`

Removed:
- `devin/deepseek-v4-1-flash`
- `devin/gemini-3-1-pro`
- `devin/gpt-5-3-codex`
- `devin/grok-4-5`
- `devin/inkling`
- `devin/model_chat_gpt_4_1_2025_04_14`
- `devin/model_claude_4_5_opus`
- `devin/model_google_gemini_3_0_flash`
- `devin/model_gpt_5_2`
- `devin/model_private_11`
- `devin/model_private_12`
- `devin/model_private_13`
- `devin/model_private_14`
- `devin/model_private_15`
- `devin/model_private_2`
- `devin/model_private_3`
- `devin/swe-1-7-lightning`

Before AISIX activation, its old DeepSeek direct model retained that member,
so catalog changed +7/-16 with its expected CPA-to-AISIX base-row switch.
AISIX activation removed that direct model and its target from
`deepseek-v4.1-flash`; the route retains five targets and `max_fallbacks: 4`.
The final catalog delta is exactly +7/-17. No replacement alias was added.

## Routing, plugins and streaming gates

- CPA-routed `zcode/glm-5.3-flash`: 200 before and after C. Private log-derived
  credential hashes are equal across the switch.
- Bare `glm-5.3-flash`: 200 and logged by AISIX after C.
- Bare `deepseek-v4.1-flash` logical route: 200 after the target removal.
- `supergrok/grok-imagine-image` on CPA's operator image-generation endpoint:
  200, with no `unknown provider` or `not supported` error. One image requested.
- Codex SSE on the operator endpoint: 200, 11 valid frames; on the public
  hostname: 200, 10 valid frames. Every event begins at a blank-line boundary;
  every frame's JSON `type` equals its `event`. Raw captures retained privately.
- Both `cpa-quota-estimator` 0.21.3 and `codex-auto-reset` 0.1.13 logged
  `plugin loaded`. No plugin reload or version-key change occurred.
- Worker-owned CPA YAML differs from the pre-B backup only at
  `routing.force-model-prefix`; buffering and plugin configuration are unchanged.

### Earlier pending Devin success and retry deadline

The sole enabled Devin credential remains registered with the same identity
before and after C. Probes for `devin/glm-5-3-flash` and `devin/kimi-k2-7` returned
429 `model_cooldown`, carrying the upstream Devin error:
`failed_precondition: Your daily usage quota has been exhausted`.
The account status and that upstream error identify the Devin credential path;
these responses are **not** successful inference.

CPA Management reports credential-quota and model-quota
`retry_at: 2026-10-28T08:00:00.002354673Z` (08:00 UTC). This is CPA's advertised
retry deadline, not independent proof that upstream quota has reset. Quota/reset
settings and accounts were not changed or rotated.

The first B attempt on the same official v8.0.45 digest successfully served
`devin/glm-5-3-flash` (200): private evidence
`/var/tmp/ai-union-20261028/batch-b/route-results.json` and its matching response
and credential-selection logs. A fresh successful 200 probe after the retry
deadline was required for tasks 3.3 and 3.4; it completed as recorded below.

### Devin success after reset

At 08:05 UTC, a read-only Management API check showed the sole enabled Devin
credential active, available and without cooldown signals. The persisted
prefix setting still read false. No production configuration, quota/reset
setting or account was changed.

At **2026-10-28 08:06:46 UTC**, one minimal public-chain Responses request for
`devin/kimi-k2-7` returned **HTTP 200** with three characters of output text.
CPA's `session-affinity` selector log names this model and selects the same
credential as the pre-B, B and C captures; its private identity hash matches
all those captures. This verifies successful routing on official v8.0.45 with
force-model-prefix=false and closes the deferred checks in 3.3 and 3.4.

Fresh temporary normal-client key **35** (group 15) was removed immediately:
DELETE **200**, subsequent GET **404**, credential file absent at 08:06:51 UTC.
Private evidence is retained in
`/var/tmp/ai-union-20261028/devin-after-reset/` (`result.json`, response, headers
and selector log) and the key lifecycle in `timeline.jsonl`.

## Pre-existing findings (not caused by this change)

The registration gate is explicitly a **delta**: a previously registered AISIX
target must remain registered except accepted ledger removals handled by D9;
no newly unregistered target is allowed. The supervisor approved this clarification
when the absolute wording exposed existing stale/disabled-channel targets.
No additional AISIX entries were deleted.

Of 68 remaining direct models, 22 targets are absent from the available-native
and OAuth-registration union, identically before and after B/C. Seven are still
configured on an enabled key channel, leaving 15 absent even when enabled
key-channel names count. Logical routes resolve to existing direct entries;
23 routes remain. The lists below contain model IDs only.

| Classification | Model IDs |
|---|---|
| Enabled configured channel, absent from available inventory | `axis/gpt-5.6-luna`, `axis/gpt-5.6-sol`, `axis/gpt-5.6-terra`, `axis/gpt-6-astra`, `axis/gpt-6-luna`, `axis/gpt-6-sol`, `axis/gpt-6.1-sol` |
| Disabled configured channel | `kimi-cn-api/kimi-k3`, `zakk/gpt-5.6-luna`, `zakk/gpt-5.6-sol`, `zakk/gpt-5.6-terra`, `zakk/gpt-6-astra`, `zakk/gpt-6-luna`, `zakk/gpt-6-sol`, `zakk/gpt-6.1-sol` |
| Stale/unregistered under the inspected enabled configuration | `aihub/gpt-6-sol`, `aihub/gpt-6.1-sol`, `codex/gpt-5.3-codex-spark`, `xl/gpt-5.6-sol`, `xl/gpt-6-astra`, `xl/gpt-6-sol`, `xl/grok-imagine-image-2.0` |

These are follow-up findings, not an assertion that every configured target is
currently routable. The enabled-channel availability cause was not established.

## Safety, earlier rollback and recovery commands

Before the CPA re-run, SQLite online backup completed with `integrity_check=ok`;
Compose, environment, AISIX resources and worker-owned CPA config were backed up
under `.ai-ops-state/union-catalog-20261028-rerun/` on the host.

The first B attempt was rolled back by the guarded CPA playbook because its old
"catalog unchanged" acceptance rejected the upstream Devin delta. It passed
plugins, health, buffering and SSE; later ordinary reads recovered all original
367 rows including priority. The transient differences came from availability
and caches, not random restart ranking. Full diagnosis remains at
`/var/tmp/ai-union-20261028/diagnosis.md`.

Reviewed rollback procedure from `ops/ansible/`:

```bash
# Undo C first if necessary; the Management API persists force-model-prefix=true.
# Restore B image through a fresh guarded preview and the digest it prints.
ansible-playbook playbooks/rollback.yml -e component=cli-proxy-api --check --diff
ansible-playbook playbooks/rollback.yml -e component=cli-proxy-api \
  -e ai_ops_component_approved_digest='<fresh-reviewed-rollback-digest>'
# Restore AISIX together with CPA if undoing D9: rollback-config.yml does not accept aisix,
# so restore the private resources.yaml from the pre-B backup, then redeploy it.
ansible-playbook playbooks/deploy-config.yml -e component=aisix --check --diff
ansible-playbook playbooks/deploy-config.yml -e component=aisix
# Catalog image rollback is separate, also previewed and digest-approved.
```

All four final read-only Ansible checks passed at 04:59–05:01 UTC:
`deploy` for CPA and model-catalog; `deploy-config` for model-catalog and AISIX.
Each reported `changed=0 failed=0`. Final health remained healthy/restarts=0;
recovery timer active and model-sync running/unpaused.

Temporary normal-client key 34 was deleted at 05:01:23 UTC: DELETE 200,
subsequent GET 404, credential file absent. Earlier attempt's key 33 had already
been deleted with the same proof. No temporary key remains for the pending probe.

Private controller evidence: `/var/tmp/ai-union-20261028/rerun-*.log`, publish/CI
logs and ledger. Private host evidence under the same directory: `rerun-pre/`,
`rerun-a/`, `rerun-b-before-aisix/`, `rerun-b-final/`, `rerun-c/`, credential route
captures, `rerun-xai/`, raw SSE and key-cleanup timeline. Secret-bearing container
snapshots and responses stay outside tracked files.
