## Why

The union-catalog-membership rollout (record `docs/records/union-catalog-membership-2026-10-28.md`, "Pre-existing findings") found 22 AISIX direct models whose target ID CPA does not register. Each of them is still a catalog member, because membership is the union of all AISIX IDs. So clients see models that cannot be served, and some logical routes carry failover targets that can never answer. The user decided on 2026-10-28 to remove the 15 entries whose channel is disabled or whose ID is unregistered. The 7 `axis/*` entries stay until we know why their enabled channel does not serve them.

## What Changes

- Remove these 15 direct models from the private AISIX resources file, together with every logical-route target that names them:
  - disabled channel: `kimi-cn-api/kimi-k3`, `zakk/gpt-5.6-luna`, `zakk/gpt-5.6-sol`, `zakk/gpt-5.6-terra`, `zakk/gpt-6-astra`, `zakk/gpt-6-luna`, `zakk/gpt-6-sol`, `zakk/gpt-6.1-sol`;
  - unregistered: `aihub/gpt-6-sol`, `aihub/gpt-6.1-sol`, `codex/gpt-5.3-codex-spark`, `xl/gpt-5.6-sol`, `xl/gpt-6-astra`, `xl/gpt-6-sol`, `xl/grok-imagine-image-2.0`.
- Logical routes are kept. Nine routes lose one or two targets, and none becomes empty. The existing deploy preflight sets `max_fallbacks` to `len(targets) - 1`.
- Keep the 7 `axis/*` direct models and their route targets. Diagnose read-only why CPA does not list them. If the cause is not transient, stop and ask the user whether to remove them.
- Deploy through the existing `deploy-config.yml -e component=aisix` path: `--check --diff` first, then apply. Then verify with a pre/post capture that the public catalog loses exactly the IDs these entries alone contributed.
- New audit rule: a direct model whose target CPA does not register is classified before anyone deletes it, as disabled channel, unregistered, or enabled-but-unavailable. Only an approved deletion set is removed.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `aisix-route-lifecycle`: adds the classification-before-pruning rule for direct models whose target is absent from CPA registration. The enabled-but-unavailable class requires a diagnosis before anyone can propose it for deletion.

## Impact

- Private config: `ops/ansible/private-config/services/routing/aisix/resources.yaml` loses 15 direct models (91 → 76 AISIX IDs). The routes `gpt-5.6-luna`, `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-6-astra`, `gpt-6-luna`, `gpt-6.1-sol`, `grok-imagine-image-2.0`, `kimi-k3` and `kimi-k3-500k` lose the listed targets.
- Catalog: internal and public standard lists lose 14 members (357 → 343 at the last capture). `xl/grok-imagine-image-2.0` is already hidden by `metadata.include_models`, so it causes no visible change. The public Codex list loses whichever of the 14 it shows at pre-capture. Passthrough (47) is unchanged.
- Code, images, Compose, CPA, Sub2API and the model-catalog config: unchanged. The `!xl/grok-imagine-image-2.0` hide in the model-catalog config becomes inert, and stays.
- Production: one AISIX config activation, a short record under `docs/records/`.
- Independent of union-catalog-membership: no shared requirement. It only uses that change's record as its source list.
