# Add GPT-6.1 Terra aggregate — 2026-11-01

## Result

Added `gpt-6.1-terra` with owner-approved ordered failover:

1. `codex/gpt-6.1-terra`
2. `xl/gpt-6.1-terra`

Both exact target IDs were present in the current CPA native manifest before editing. Congee was not selected for the aggregate; AIHub did not register this version. No existing Terra or other route changed.

The new direct declarations copy old Terra's enabled cooldown: 60s default, 300s maximum, honor `Retry-After`, and the same trigger statuses. The logical route copies ordered failover with zero retries, one fallback, `retry_on_429: true`, and fallback statuses 400/401/402/403/404.

The tracked catalog example and private profile each add only the new static slug with `context_window: 500000` and `max_context_window: 500000`. Existing bundled source metadata supplies the new-version model information; no old model metadata was copied and no application code changed.

## Verification

| Measurement | Before | After |
|---|---:|---:|
| Effective AISIX models | 70 | 73 |
| Logical routes | 23 | 24 |
| Standard catalog members | 340 | 341 |
| Codex catalog members | 340 | 341 |

- Controller/live resources and catalog configuration SHA-256 values matched before editing. Protected backups, snapshots, command logs and assert-based checks are in the private controller directory `/var/tmp/ai-add-terra-20261101/`.
- Parsed candidate assertions proved that the only AISIX additions are the alias and its two direct models, with exact old-Terra routing/cooldown policies. Every old value, including provider/caller configuration, remained unchanged. The private catalog delta is exactly one static entry.
- Resource-policy and orphan/reference validation passed; no orphan deletion was performed. The three focused catalog suites passed all 15 tests, including tracked example validation.
- Both `deploy-config.yml --check --diff` previews passed and showed only the approved additions. The unmanaged AISIX config reported `config_diff: false`.
- Three established AISIX connection samples, ten seconds apart, each counted 3. The owner explicitly accepted short service interruption. Actual request downtime and interrupted-request count were not measured.
- AISIX apply ran **15:01:34–15:04:23 UTC**; model-catalog apply ran **15:04:23–15:10:05 UTC**. Both returned `failed=0`. These are controller operation durations, not request downtime.
- Authenticated host-side Admin API read-back showed exactly 73 models. The new effective route and direct cooldown fields matched the candidate, including the existing pool's resolved provider-key identity. All 70 previous effective model values were unchanged.
- Both live files matched their controller candidate SHA-256 values. Model-catalog's validated config digest was `efea5e8dfd9038b0c3fc77b8ce7f47739f51dd2c12f949439874e05c854f5fad`.
- Standard and Codex catalogs each gained only bare `gpt-6.1-terra`, lost no IDs, and preserved old metadata apart from positional Codex priorities. The two direct target IDs were already catalog members.
- New Codex metadata read-back: `GPT-6.1 Terra`, context/max context 500000, output limit 128000, inputs text/image/PDF, output text, reasoning levels none/low/medium/high/xhigh/max.
- Only AISIX and model-catalog container identities changed. Both retained their previous image, were running with zero restarts, and the other 46 running-container snapshots were unchanged.

### Existing tooling limitations

The playbook's optional AISIX Admin probe again reported `rc=127`. Acceptance instead used a separate successful authenticated Admin API read and complete before/after effective-model comparison. No gate was bypassed. As in the prior Axis removal operation, both playbooks used the documented explicit runtime selectors `ai_ops_compose_cmd: docker compose` and `ai_ops_runtime: docker`.

## Rollback

For AISIX, restore private `resources-pre.yaml` from the protected controller directory to `ops/ansible/private-config/services/routing/aisix/resources.yaml`, run the AISIX configuration preview, require precisely the reverse additions, then apply and verify effective inventory and catalogs. `rollback-config.yml` has no AISIX operation.

For model-catalog, use the guarded `playbooks/rollback-config.yml -e component=model-catalog` operation with the same explicit runtime selectors, after reviewing its preview. The controller's `catalog-pre.yaml` also preserves the original desired profile. Do not overwrite intervening configuration drift. Remove the tracked example entry if reverting the source change.

## Coverage limits

No paid generation, forced-failure/failover injection, public authenticated client-entitlement probe, or SSE/WS continuity test was performed. Registration, effective routing and catalog metadata are verified, not end-to-end inference success. No temporary caller key was created. No account, entitlement, credential, image, Compose or dependency changed. The completed configuration-only OpenSpec change was archived at `openspec/changes/archive/2026-11-01-add-gpt-6-1-terra/` without main-spec changes.
