# Prune dangling AISIX targets — 2026-10-28

## Result

Removed the approved 15 AISIX direct models and their references from nine logical routes. No route became empty; all 23 logical routes remain. CPA, Sub2API, caller/provider keys, Compose and images were unchanged. Only the private AISIX resources file was activated.

| Capture | Before | After |
|---|---:|---:|
| AISIX inventory | 91 | 76 |
| AISIX direct models | 68 | 53 |
| Internal standard / Codex catalog | 358 | 344 |
| Public standard / Sub2API list | 358 | 344 |
| Public Codex list | 349 | 335 |
| Availability-derived passthrough IDs | 46 | 47 |

Exactly 14 visible IDs left each catalog/public list: every ID in the removal table below except `xl/grok-imagine-image-2.0`, which was already hidden by `include_models`. No ID was added. All retained standard and Codex rows compare equal on every field except Codex `priority`; internal lists retain bare-first code-point order and priorities 1…344. No newly unregistered direct model or route target appeared.

## Fresh baseline and native-availability recovery

The fresh pre-capture was sampled twice before activation. Since the union record, upstream registration added `anyrouter/claude-fable-5-1-reversed` and its bare alias. Also, `codex/gpt-6.1-sol` was temporarily absent from the native available list, though AISIX still contributed it to the catalog. Recent AISIX logs contained upstream failures for this target and later successful calls. No setting was changed to cause or repair this.

These facts explain 358 pre-capture catalog members rather than the historical 357. The supervisor approved using this fresh baseline, preserving the exact 14-removal acceptance gate.

During activation `codex/gpt-6.1-sol` recovered native availability. The union helper derives passthrough IDs from current native availability intersected with OAuth registrations, so its diagnostic set changed 46→47 solely by this recovery. The same model's catalog row was equal before and after modulo priority. The supervisor explicitly approved this one diagnosed availability-only difference and the corresponding task 3.1 Verify wording; no other passthrough change was permitted. The underlying manifest comparison remained exact modulo priority for every retained row.

## Removed IDs and restore positions

Target positions are 1-based in the pre-activation resources file. Standalone means no logical route referenced the direct model.

| Removed direct ID | Previous route target position |
|---|---|
| `codex/gpt-5.3-codex-spark` | standalone |
| `zakk/gpt-6-astra` | `gpt-6-astra` #3 |
| `zakk/gpt-5.6-luna` | `gpt-5.6-luna` #2 |
| `aihub/gpt-6.1-sol` | `gpt-6.1-sol` #4 |
| `aihub/gpt-6-sol` | standalone |
| `zakk/gpt-5.6-sol` | `gpt-5.6-sol` #3 |
| `zakk/gpt-6.1-sol` | `gpt-6.1-sol` #3 |
| `zakk/gpt-6-sol` | standalone |
| `zakk/gpt-5.6-terra` | `gpt-5.6-terra` #3 |
| `zakk/gpt-6-luna` | `gpt-6-luna` #2 |
| `kimi-cn-api/kimi-k3` | `kimi-k3` #1; `kimi-k3-500k` #1 |
| `xl/gpt-5.6-sol` | standalone |
| `xl/gpt-6-sol` | standalone |
| `xl/gpt-6-astra` | standalone |
| `xl/grok-imagine-image-2.0` | `grok-imagine-image-2.0` #2 |

All 15 IDs were absent from freshly sampled native and OAuth-registered inventories before activation. The removed seven unregistered and eight disabled-channel entries match the approved classification. The now-inert `!xl/grok-imagine-image-2.0` hide remains unchanged.

## Axis diagnosis and user decision

**`axis/*` kept intentionally (user, 2026-10-28).** No further Axis investigation or CPA edit was performed after this decision.

The enabled `codex-api-key` channel declares model names but excludes `*`. Official CPA v8.0.45 `sdk/cliproxy/service_models.go` applies `ExcludedModels` to configured Codex-key models before registration (lines 159–167 and 657–690). This is a permanent configuration exclusion, not a quota cooldown. Native available and OAuth-registration lists contained no Axis-prefixed models, and the sampled preceding six hours had no Axis-prefixed CPA log lines. No upstream request was needed to explain the exclusion.

The seven per-ID classifications share this cause: `axis/gpt-5.6-luna`, `axis/gpt-5.6-sol`, `axis/gpt-5.6-terra`, `axis/gpt-6-astra`, `axis/gpt-6-luna`, `axis/gpt-6-sol`, and `axis/gpt-6.1-sol`. Their direct models and logical-route references are untouched.

The user also chose to keep the three routes with one working target: `gpt-5.6-luna` → `codex/gpt-5.6-luna`, `gpt-6.1-sol` → `codex/gpt-6.1-sol`, and `grok-imagine-image-2.0` → `supergrok/grok-imagine-image-2.0`. The first two retain intentional Axis fallback entries, which remain unregistered. Removing already-unusable targets did not reduce working capacity.

## Activation and checks

Private evidence is under `/var/tmp/ai-prune-20261028/` on the controller and deployment host, including pre/post captures, comparison output, plan/apply/drift logs, route responses and AISIX selection logs. The local rollback copy `resources-pre.yaml` is mode 0600. No private values are included in this record.

1. Parsed comparison against the backup proved only the 15 model-object removals and associated route targets changed. `check-aisix-orphans.py --adopted '[]'` reported no orphan candidates and no empty route.
2. `deploy-config.yml -e component=aisix --check --diff` passed. Its diff contained exactly the removals and the existing normalizer's `max_fallbacks` changes: `gpt-5.6-luna` 2→1; `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-6-astra`, `gpt-6-luna` 3→2; `gpt-6.1-sol` 3→1; `grok-imagine-image-2.0` 1→0; `kimi-k3` and `kimi-k3-500k` 2→1. Normalized parsed comparison proved every other field unchanged.
3. Apply passed (`failed=0`), including Admin API effective-route read-back. AISIX stayed running with restarts=0 and served its inventory and traffic; it has no Docker HEALTHCHECK. Model-catalog was healthy with restarts=0.
4. Forced `/models-table/refresh`, then captured the refreshed internal/public lists. The exact ID delta, non-priority row equality, output order, priority and registration-delta assertions passed.
5. Three public-chain requests returned 200. AISIX access logs identify each first working target:

   | Public bare route | Endpoint | Served by |
   |---|---|---|
   | `gpt-6-astra` | Responses | `codex/gpt-6-astra` |
   | `kimi-k3` | Responses | `sensenova/kimi-k3` |
   | `grok-imagine-image-2.0` | Images generations | `supergrok/grok-imagine-image-2.0` |

6. Fresh temporary normal-client key 36 was deleted at **08:55:12 UTC**: DELETE **200**, following GET **404**, local credential file absent.
7. Final `--check --diff` runs for `deploy-config.yml -e component=aisix`, `deploy-config.yml -e component=model-catalog`, and `deploy.yml -e component=aisix` each reported **changed=0 failed=0**.

## Rollback

`rollback-config.yml` has no AISIX path. To restore this activation, copy the mode-0600 controller backup `/var/tmp/ai-prune-20261028/resources-pre.yaml` over the private AISIX resources file. Run `deploy-config.yml -e component=aisix --check --diff`; require exactly the reverse removals and normalization changes. Apply with `deploy-config.yml -e component=aisix`, then repeat inventory, catalog and route verification against the pre-capture. If activation itself fails, the managed-file role restores and reactivates its host backup automatically.

## Residual risks

Intentional Axis targets still cannot serve because their CPA channel excludes all models. The three single-working-target routes have no currently usable alternative. Clients requesting a removed direct ID now receive an unknown-model response rather than the former upstream failure. No unrelated target or dependency cleanup was performed.
