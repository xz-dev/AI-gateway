## Context

- **Source list.** `docs/records/union-catalog-membership-2026-10-28.md`, "Pre-existing findings", lists 22 AISIX direct targets that are absent from CPA's available-model list and OAuth registrations. They were the same before and after batches B and C. This change uses that classification.
- **Private resources file.** `ops/ansible/private-config/services/routing/aisix/resources.yaml`, read on 2026-10-28:
  - 91 models: 68 direct models and 23 logical routes;
  - every direct model has `display_name == model_name`;
  - both caller keys (`sub2api`, `model-catalog`) use `allowed_models: ['*']`, so no caller key names a removed ID.
- **Deploy path.** `deploy-config.yml -e component=aisix` runs the `ai_ops_aisix` role:
  - normalizes `max_fallbacks` to `len(targets) - 1`;
  - validates the fallback-status contract;
  - rejects an unverified `retries: -1`;
  - runs `check-aisix-orphans.py`, which fails on unresolved references and on a retained route left with zero targets;
  - uploads the file and reads the effective routes back through the Admin API.
- **Catalog membership** (union rule) is AISIX IDs ∪ CPA-prefixed CPA IDs ∪ `membership.cpa_include` (`devin/*`) ∪ static models. None of the 15 IDs is registered by CPA, matched by `devin/*`, or a static model. Of the 15, only `xl/grok-imagine-image-2.0` appears in the model-catalog config, as the hide `!xl/grok-imagine-image-2.0` in `metadata.include_models`.

### Route impact (model IDs and route names only)

| Removed direct model | Class | Routes that lose it | Remaining targets, in order |
|---|---|---|---|
| `zakk/gpt-5.6-luna` | disabled | `gpt-5.6-luna` | `codex/gpt-5.6-luna`, `axis/gpt-5.6-luna` |
| `zakk/gpt-5.6-sol` | disabled | `gpt-5.6-sol` | `codex/gpt-5.6-sol`, `axis/gpt-5.6-sol`, `aihub/gpt-5.6-sol` |
| `zakk/gpt-5.6-terra` | disabled | `gpt-5.6-terra` | `codex/gpt-5.6-terra`, `axis/gpt-5.6-terra`, `aihub/gpt-5.6-terra` |
| `zakk/gpt-6-astra` | disabled | `gpt-6-astra` | `codex/gpt-6-astra`, `axis/gpt-6-astra`, `aihub/gpt-6-astra` |
| `zakk/gpt-6-luna` | disabled | `gpt-6-luna` | `codex/gpt-6-luna`, `xl/gpt-6-luna`, `axis/gpt-6-luna` |
| `zakk/gpt-6.1-sol` | disabled | `gpt-6.1-sol` | `codex/gpt-6.1-sol`, `axis/gpt-6.1-sol` (also loses `aihub/gpt-6.1-sol`) |
| `aihub/gpt-6.1-sol` | unregistered | `gpt-6.1-sol` | see the row above |
| `kimi-cn-api/kimi-k3` | disabled | `kimi-k3`, `kimi-k3-500k` | `sensenova/kimi-k3`, `kimi-coding/kimi-k3` (both routes) |
| `xl/grok-imagine-image-2.0` | unregistered | `grok-imagine-image-2.0` | `supergrok/grok-imagine-image-2.0` |
| `zakk/gpt-6-sol` | disabled | none (standalone) | — |
| `aihub/gpt-6-sol` | unregistered | none (standalone) | — |
| `codex/gpt-5.3-codex-spark` | unregistered | none (standalone) | — |
| `xl/gpt-5.6-sol` | unregistered | none (standalone) | — |
| `xl/gpt-6-astra` | unregistered | none (standalone) | — |
| `xl/gpt-6-sol` | unregistered | none (standalone) | — |

Notes on the table:
- **No route becomes empty**, so no route is deleted.
- **One working target left.** `axis/*` is in the enabled-but-unavailable class, so these routes keep a single target that CPA currently serves: `gpt-5.6-luna` (`codex/gpt-5.6-luna`), `gpt-6.1-sol` (`codex/gpt-6.1-sol`) and `grok-imagine-image-2.0` (`supergrok/grok-imagine-image-2.0`).
- **No working target is lost.** The removed targets could never answer, so failover coverage does not drop.

### Expected catalog delta

| List | Last capture (union batch C) | After |
|---|---:|---:|
| AISIX IDs | 91 | 76 (−15) |
| Internal catalog / public standard (Sub2API) | 357 | 343 (−14) |
| Public Codex | 348 | 348 minus those of the 14 present at pre-capture |
| Passthrough | 47 | 47 |

The table is the historical union baseline, not a fixed absolute-count gate. On activation day the fresh baseline was 358 internal/public standard and 349 public Codex members, so the exact 14-removal delta yielded 344 and 335. The availability-derived passthrough diagnostic changed 46→47 only because the pre-diagnosed `codex/gpt-6.1-sol` recovered natively; every retained manifest row was equal modulo priority. These explicit gate adjustments were supervisor-approved and are recorded in the deployment record.

The 14 visible removals are all of the 15 IDs except `xl/grok-imagine-image-2.0`, which `include_models` already hides. Every other member keeps all its fields. List order and Codex `priority` shift by position under the union output-order rule.

## Goals / Non-Goals

**Goals:**
- Remove the approved 15 entries and their route targets in one reviewed AISIX activation.
- Prove the catalog delta matches the table exactly.
- Record a diagnosis of the 7 `axis/*` entries, and the decision that follows from it.

**Non-Goals:**
- Removing the `axis/*` entries in this change without a diagnosis and, if needed, a user decision.
- Editing CPA channels, credentials, Sub2API mappings or entitlements, provider keys or caller keys.
- Deleting the now-inert `!xl/grok-imagine-image-2.0` hide in the model-catalog config. `lua-catalog-membership` replaces `include_models` anyway.
- Changing route strategy, retries or fallback statuses. Only `max_fallbacks` follows target count, through the existing normalizer.

## Decisions

- **D1: keep every logical route; remove only targets.** No route becomes empty, so the spec's "reject a route left without targets" never triggers. A route that keeps only one working target stays, because removing a target that never answers loses no capacity. Alternative rejected: deleting the single-working-target routes, since that would remove client-facing IDs that work today.
- **D2: delete the six standalone entries too.** `zakk/gpt-6-sol`, `aihub/gpt-6-sol`, `codex/gpt-5.3-codex-spark`, `xl/gpt-5.6-sol`, `xl/gpt-6-astra` and `xl/gpt-6-sol` have no route reference. The user's approved set names them explicitly, so they are approved deletions, not orphan candidates to re-ask about.
- **D3: edit by hand in the private file, in place.** Use the same method as D9 of union-catalog-membership:
  - delete each direct-model object and each `- model: <id>` target line;
  - leave every other field byte-identical;
  - let the role's normalizer rewrite `max_fallbacks`.
  
  Expected `max_fallbacks`: `gpt-5.6-luna` 2→1, `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-6-astra` and `gpt-6-luna` 3→2, `gpt-6.1-sol` 3→1, `grok-imagine-image-2.0` 1→0, `kimi-k3` and `kimi-k3-500k` 2→1. The `--check --diff` output must show exactly these lines plus the removals.
- **D4: verify the catalog delta from the union rule.** Do not compare raw counts only. Compute the expected removal set:
  - start from pre-capture AISIX IDs minus the 15;
  - for each of the 15, keep it out only if it is not in native CPA, not matched by `devin/*` and not a static model;
  - apply `include_models`.
  
  Compare that set with the post-capture member set. Passthrough rows must be byte-equal apart from `priority`. Other compiled rows must be equal apart from `priority`.
- **D5: `axis/*` diagnosis is read-only, with a decision point.** Resolved: `axis/*` kept intentionally (user, 2026-10-28), after diagnosis showed the configured wildcard exclusion. No CPA configuration change or further investigation is in scope. Gather:
  - the channel's CPA config entry (enabled flag and model list, names only);
  - CPA auth/registration state for that channel;
  - recent CPA log lines for that prefix (errors, cooldowns, model-list fetch failures);
  - one models-list read of the upstream through CPA management, if the management API offers one.

  Then decide:
  - **Transient** (cooldown, quota, a temporary upstream failure with a recovery time): record it and keep the entries.
  - **Permanent or unknown** (auth failure, the upstream no longer offers these IDs, the channel misconfigured): stop and ask the user whether to remove them. Do not remove them in this change without that answer. If the user approves, a removal follows the same D3/D4 procedure in a second activation.
- **D6: independent of union-catalog-membership.** This change adds one requirement to `aisix-route-lifecycle`, which union does not touch. Union's `cpa-first-catalog-routing` scenario only defers these pre-existing targets to a separate change, which is this one. So there is no gate task. The only operational constraint is that no other production window runs at the same time; the Ansible lock enforces that.

## Risks / Trade-offs

- [A client still requests a removed direct ID] → it gets AISIX's unknown-model error instead of an upstream failure. The IDs could not be served before, so no working traffic is lost. The record lists them.
- [A disabled channel is re-enabled later] → re-adding a direct model is a normal reviewed AISIX edit. The record keeps the removed IDs and their old route positions so they can be restored.
- [CPA registration changes between pre-capture and apply] → re-sample native and registered models just before apply. If any of the 15 is now registered, drop it from the set and report it, not delete it.
- [`axis/*` stay dangling longer] → accepted by the user, and bounded by the D5 decision point.
- [The single-working-target routes have no failover] → that is already the case today, because the removed targets never answered. The record flags these three routes as a follow-up for the user.

## Migration Plan

1. Pre-capture, read-only, to `/var/tmp`:
   - AISIX effective inventory;
   - native and registered CPA models;
   - internal catalog, Codex manifest, passthrough set;
   - public standard and Codex lists (with a temporary normal-client key);
   - AISIX health.
2. Save a mode-0600 backup of the private resources file under `/var/tmp/prune-aisix-<date>/`, then edit the file per D3. Run `check-aisix-orphans.py` and the role's preflight locally through `--check --diff`.
3. `ansible-playbook playbooks/deploy-config.yml -e component=aisix --check --diff`. Review that the diff contains exactly the 15 removals, the target lines and the `max_fallbacks` changes. Then apply.
4. Force a catalog refresh, wait for the public cache to drain, then post-capture. Verify per D4, check health, and confirm no new dangling target.
5. Run `--check --diff` again for `aisix` and `model-catalog`: changed=0.
6. Delete the temporary key with proof. Write the record.

Rollback. `rollback-config.yml` accepts only `cpa-model-sync` and `model-catalog`, so it has no `aisix` path. Instead:
1. Before step 2, save the private file as a mode-0600 copy under `/var/tmp/prune-aisix-<date>/`.
2. To roll back, copy that backup over the private `resources.yaml`.
3. Run `deploy-config.yml -e component=aisix`: `--check --diff` first, then apply. The diff must show exactly the reverse of the step 3 diff.
4. Re-capture and compare with the pre-capture.

If activation itself fails, the `ai_ops_managed_file` rescue already restores and re-activates the host's pre-deploy copy.
