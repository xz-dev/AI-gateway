# Remove Axis models from AISIX — 2026-10-30

> Historical state: counts, approvals, timings and verification below describe only the 2026-10-30 operation. This record is not a new deployment or certification of today's resources/profile; rollback requires fresh drift review. See [current routing](../model-routing.md) and [current operations](../ansible-operations.md).

## Result

After explicit owner approval, removed all seven `axis/*` direct models and their six logical-route target entries from the private AISIX resources file. This supersedes the owner's 2026-10-28 decision to retain those entries. The operation follows the existing `aisix-route-lifecycle` and `aisix-model-routing` requirements; no routing feature or account policy changed.

| Measurement | Before | After |
|---|---:|---:|
| Effective AISIX models | 77 | 70 |
| Logical routes | 23 | 23 |
| Standard catalog members | 344 | 337 |
| Codex catalog members | 344 | 337 |
| Axis direct models | 7 | 0 |
| Axis route targets | 6 | 0 |

Only AISIX was recreated, on the same image. The other 47 Compose containers retained their IDs, images, start times, running states, restart counts and health states. CPA, Sub2API, provider/caller keys, model-sync policy, model-catalog configuration, Compose and images were not changed.

## Removed models and retained route targets

Each affected route keeps `strategy: failover`, `retries: 0`, existing error policy and the exact relative order and fields of the remaining targets. `max_fallbacks` follows the existing target-count-minus-one policy.

| Removed direct model | Retained logical-route targets, in order | `max_fallbacks` |
|---|---|---|
| `axis/gpt-6-astra` | `gpt-6-astra`: `codex/gpt-6-astra`, `aihub/gpt-6-astra` | 2 → 1 |
| `axis/gpt-5.6-sol` | `gpt-5.6-sol`: `codex/gpt-5.6-sol`, `aihub/gpt-5.6-sol` | 2 → 1 |
| `axis/gpt-6.1-sol` | `gpt-6.1-sol`: `codex/gpt-6.1-sol` | 1 → 0 |
| `axis/gpt-6-sol` | Standalone declaration; no route referenced it | — |
| `axis/gpt-5.6-terra` | `gpt-5.6-terra`: `codex/gpt-5.6-terra`, `aihub/gpt-5.6-terra` | 2 → 1 |
| `axis/gpt-5.6-luna` | `gpt-5.6-luna`: `codex/gpt-5.6-luna` | 1 → 0 |
| `axis/gpt-6-luna` | `gpt-6-luna`: `codex/gpt-6-luna`, `xl/gpt-6-luna` | 2 → 1 |

## Activation and verification

Private controller evidence and the mode-0600 rollback copy are under `/var/tmp/ai-prune-axis-20261030/` in a mode-0700 directory. No deployment addresses or credentials are included here.

1. The live resources SHA-256 matched the reviewed controller backup before editing. Authenticated Admin API and freshly refreshed standard/Codex catalogs were captured before and after activation.
2. An assert-based parsed comparison proved the complete candidate equals the baseline with only the seven declarations, six memberships and six derived fallback limits changed. Every other field, including credentials and target properties, remained equal. Existing resource-policy and orphan/reference validators passed; no route became empty.
3. Before activation, three samples ten seconds apart found 5, 3 and 3 established AISIX request-port connections. The owner had approved interruption of in-flight requests. Request-level downtime and actual interrupted-request count were not measured.
4. The reviewed `deploy-config.yml -e component=aisix --check --diff` passed. Apply ran from **09:05:18 to 09:08:20 UTC**, with `failed=0`; this is controller elapsed time, not downtime. The new AISIX container reports a start time of **09:07:35 UTC**.
5. The fresh post-capture at **09:08:49 UTC** showed AISIX running with restart count zero and the same image. Live resources matched the controller candidate; hashes of the sampled unrelated configuration files were unchanged.
6. Independent authenticated Admin API read-back matched all 70 candidate model values and all effective routing fields, including resolved provider-key IDs. No Axis declaration or target remained. The loader represents provider aliases as stable UUIDv5 `provider_key_id` values; those identities were preserved.
7. Forced catalog refresh and exact pre/post comparison proved that standard and Codex lists each lost precisely the seven Axis IDs, added no IDs, and retained every remaining row unchanged apart from Codex `priority`.
8. The final AISIX `--check --diff`, with the same explicit runtime selectors, reported **changed=0, failed=0** and `config_diff: false`. `openspec validate --all --strict` passed all **19** items; `git diff --check` passed.
9. A separate read-only reviewer independently derived the deletion projection, compared the effective Admin/catalog/container snapshots, and approved the operation without findings. The reviewer made no production calls.

### Tooling deviations

The first read-only preview failed during container-runtime autodetection (`No first item, sequence was empty`), before any production mutation. Direct read-only Ansible command and shell probes proved Docker Compose available at the required version. Preview and apply then used the documented explicit selectors:

```bash
-e '{"ai_ops_compose_cmd":"docker compose","ai_ops_runtime":"docker"}'
```

No gate was disabled and no controller, runtime or inventory setting was persistently changed. Autodetection itself was not repaired as part of this removal.

The playbook's optional Admin API probe returned `rc=127`. This was not used as acceptance evidence: separate authenticated host-network-namespace Admin reads succeeded before and after activation, and the full effective model/routing comparison passed.

Later source follow-up [`7643216`](https://github.com/xz-dev/AI-gateway/commit/7643216bb10d2001abd9eb8b243c5d7eeaa2822c) fixed the proved standalone-engine, explicit-empty and whitespace command cases, plus optional-command diagnostics. It did not reproduce or prove a fix for this original private-context normal-autodetection incident. The explicit selectors above remain valid. Current [optional Admin command reporting](../ansible-operations.md#operational-notes) still does not prove effective routing/model equivalence; the separate historical Admin comparison is the acceptance evidence here.

## Rollback

Restore `/var/tmp/ai-prune-axis-20261030/resources-pre.yaml` to the ignored controller file `ops/ansible/private-config/services/routing/aisix/resources.yaml`. From `ops/ansible/`, run the same AISIX configuration preview with the explicit runtime selectors above. Require exactly the reverse model/target removals and fallback-limit changes, then apply the reviewed configuration and repeat effective-route, catalog and container read-back. `rollback-config.yml` does not have an AISIX path. The managed-file role also retains its protected host pre-deploy copy; never restore over intervening unreviewed drift.

## Residual risks and exclusions

`gpt-6.1-sol` and `gpt-5.6-luna` now each have one configured target. Their removed Axis fallbacks were already unavailable under the previously recorded CPA wildcard exclusion; this operation does not create another working fallback. Removed direct IDs no longer have AISIX declarations; no gateway-wide prohibition on CPA passthrough is claimed.

No paid generation, SSE/WS continuity probe, CPA account edit or Sub2API entitlement cleanup was performed. No temporary caller key was created. Public authenticated endpoint behavior was not separately probed; exact list verification used the internal catalog seam.

## Supersession — 2026-11-09

This is a historical receipt, not the final deployed image or current admission
guarantee. Preserve its failed/stopped attempts and original evidence limits.
The [final deployment, cleanup and three residual capacity checks](maintenance-capacity-closeout-2026-11-09.md)
supersede its current-state/pending-deployment statements. No earlier unverified
rollout task is completed retroactively by this reconciliation.
