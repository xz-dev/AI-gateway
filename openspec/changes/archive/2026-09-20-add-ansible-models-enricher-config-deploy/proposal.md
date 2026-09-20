## Why

`models-enricher` reads `models-enricher/config.yaml` only at startup, but operators currently must edit that bind-mounted file directly on the production host. This bypasses the repository's local desired-state, drift detection, review, recovery, and receipt workflow used for other managed production configuration.

## What Changes

- Add a dedicated Ansible operation for deploying the `models-enricher` YAML configuration from the gitignored local private configuration directory to the existing remote bind-mount path.
- Make plan mode validate the local candidate and show its effective local-versus-remote diff without uploading or recreating containers.
- Reuse the shared file drift gate, protected pre-deploy copy, baseline, guarded rollback, and receipt conventions.
- On an approved content change, recreate only `models-enricher` without dependencies or image builds, then verify the recreated service loaded the expected configuration and became ready.
- Keep an unchanged, baselined configuration as a no-op.
- Document adoption, plan/apply, rollback, paths, activation scope, and verification.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `production-ops-tooling`: expose models-enricher configuration deployment and rollback through the existing tagged Ansible entry playbook and local private-config surfaces.
- `production-operations-control`: define drift-safe, validated, minimally disruptive activation, verification, receipt, and recovery guarantees for models-enricher configuration.

## Impact

- Affected areas: `ansible/ops.yml`, `ansible/roles/ai_ops/`, Ansible documentation/examples, focused Ansible rehearsal scripts, and the production operations specs.
- Local desired path: under `ai_ops_config_dir`, mirroring `models-enricher/config.yaml`.
- Remote target: the existing `{{ ai_ops_deploy_root }}/models-enricher/config.yaml` bind mount.
- Runtime impact: only `models-enricher` is force-recreated when configuration content changes; its namespace owner, relays, AISIX, CPA, catalog sidecar, and unrelated services remain running.
- No external API, model-enrichment semantics, image, dependency, or secret-custody changes.
