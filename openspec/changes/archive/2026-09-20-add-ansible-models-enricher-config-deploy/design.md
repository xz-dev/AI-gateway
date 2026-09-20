## Context

See `proposal.md` for motivation and the delta specs for required behavior.

`models-enricher` reads `CONFIG_PATH` once during process startup. Production Compose bind-mounts `./models-enricher/config.yaml` to `/app/config.yaml`, so changing the host file without recreating the application leaves the old in-memory configuration active. The application shares the network namespace owned by `models-enricher-netns`; its CPA, Squid, AISIX, and APISIX-facing connectivity also involves adjacent relay containers that must not be recreated for a YAML-only change.

The tracked `models-enricher/config.yaml` is an image/build default. Production desired state follows the existing Ansible convention: reviewed live configuration is adopted into gitignored `ansible/private-config/` (or overridden `ai_ops_config_dir`), while repository defaults never silently replace it.

The existing `deploy-file.yml` and `rollback-file.yml` already provide local/remote/baseline checksums, drift refusal, protected pre-deploy copies, upload, activation rescue, delayed baseline update, and guarded rollback. The sidecar already exposes `/readyz` and a `healthcheck` CLI command, but it has no bounded configuration-only validation command.

## Goals / Non-Goals

**Goals:**
- Make a local private YAML file the operational source of truth.
- Validate candidate syntax and static semantics before any upload.
- Reuse the shared file transaction instead of creating another deployment state machine.
- Recreate only `models-enricher` when bytes change.
- Verify fresh container identity, expected mounted bytes, readiness, unchanged adjacent infrastructure, and bounded recovery.
- Keep first adoption and unchanged configuration non-disruptive.

**Non-Goals:**
- Change enrichment, filtering, metadata-source, routing, or snapshot semantics.
- Add hot reload or a configuration API.
- Move credentials into YAML or print private YAML in receipts.
- Manage the nginx `model-catalog-sidecar` or models-table publication services.
- Rebuild or pull an image during a configuration-only operation.
- Make the tracked repository YAML the production file automatically.

## Decisions

### 1. Use dedicated deploy and rollback tags

The single `ansible/ops.yml` entry point will expose:

```text
--tags deploy-models-enricher-config
--tags rollback-models-enricher-config
```

The operation uses:

- local desired file: `{{ ai_ops_config_dir }}/models-enricher/config.yaml`;
- remote bind mount: `{{ ai_ops_deploy_root }}/models-enricher/config.yaml`;
- service: `models-enricher`.

Dedicated tags avoid coupling a policy-only edit to generic Compose/.env deployment or image preparation. Adding the service only to `ai_ops_profiles` was rejected because generic component deploy does not own this file, does not validate it, and plain `up -d` may not recreate a service whose bind-mounted bytes changed.

### 2. Add one configuration-only validation mode to the existing binary

The existing executable will gain a bounded command equivalent to:

```text
/models-enricher validate-config -
```

`-` reads YAML from stdin. The command runs the same `loadConfig` path used at startup, including YAML decoding, required fields, provider-prefix parsing, source-token rules, and regular-expression compilation. It emits only a versioned success result and SHA-256 digest of the candidate bytes; errors remain bounded and contain no credential values. It performs no network requests and starts no listener.

Ansible streams the candidate to this command through `compose exec -T models-enricher`, exercising the currently deployed binary without uploading a temporary file or starting another container. Both plan and apply run this validation before mutation. After recreation, the same command validates `/app/config.yaml` and reports its digest for equality with the candidate.

This static command intentionally does not run `validateRuntime`, which requires live CPA channel discovery and a native manifest. Runtime compatibility is instead proven by the replacement process reaching its existing readiness contract. Duplicating Go configuration rules in Python or Ansible was rejected because those validators would drift. Starting a temporary production container was rejected because plan must remain non-mutating and production builds are forbidden.

A validation-capable image is a prerequisite for enabling the operation. Rollout of that image remains an ordinary reviewed image deployment, separate from later YAML-only changes.

### 3. Reuse the shared file transaction

`prepare-file.yml` supplies plan diff and drift checks. `deploy-file.yml` receives:

- label `models-enricher/config.yaml`;
- local and remote paths above;
- explicit remote mode `0644`, required by the non-root container user;
- activation command;
- a focused verification task file;
- candidate and recovery digests/identities.

No extra approval-token variable is added. Unlike model-sync, this change has no dynamic source inventory whose meaning can change between plan and apply. Apply revalidates the current local bytes, repeats the fresh drift gate, and is itself the explicit mutation command. A second digest-copy-paste ceremony would add operator friction without binding additional state.

### 4. Force-recreate only models-enricher

Activation uses the equivalent of:

```text
compose up -d --force-recreate --no-deps --no-build models-enricher
```

`--force-recreate` is required because the process loads YAML only at startup and bind-mounted content does not reliably alter Compose's service configuration hash. `--no-deps` preserves `models-enricher-netns`, AISIX, relay containers, CPA, and APISIX. `--no-build` prevents accidental production builds.

Before activation, the operation captures the current application container/image identity and the container identities of the namespace owner plus adjacent relays. A changed deployment requires a new `models-enricher` container using the same image while those protected identities remain unchanged.

### 5. Verify digest, fresh identity, and readiness inside the rescue boundary

A focused `verify-models-enricher-config.yml` task will:

1. require a non-empty replacement container ID distinct from the pre-activation ID;
2. require the replacement image identity to match the pre-activation image;
3. run `validate-config /app/config.yaml` in the replacement and require its digest to equal the candidate digest;
4. retry the existing `/models-enricher healthcheck` command for a bounded count/delay until `/readyz` succeeds;
5. re-read the remote file checksum and require it to remain equal to the candidate;
6. require captured namespace-owner and relay container IDs to remain unchanged.

The verification hook runs before `deploy-file.yml` records the candidate baseline. `compose ps` presence alone was rejected because it does not prove the startup-loaded configuration is usable.

New tunables are limited to readiness retry count and delay, declared under the `ai_ops_` namespace in role defaults. Paths and protected service names remain operation constants because they are part of the Compose contract, not operator choices.

### 6. Recovery restores the old YAML and proves readiness

If activation or verification fails, the existing rescue path restores the protected pre-deploy file and force-recreates only `models-enricher`. Recovery verification requires:

- restored `/app/config.yaml` digest equals the old remote checksum;
- a fresh application container uses the expected image;
- the existing healthcheck becomes ready within the same bounded policy;
- protected adjacent container identities remain unchanged.

Only then may the operation report candidate failure with completed recovery. Otherwise it writes a sanitized recovery-required receipt and leaves the previous baseline authoritative. Manual rollback uses `rollback-file.yml` with the same activation and verification task.

### 7. Evidence remains bounded and private

Interactive plan output includes the existing unified local-versus-remote file diff. This YAML contains operational channel/model policy but no credentials by contract; receipts still exclude its content.

Deploy and rollback receipts record only:

- candidate/observed configuration digests;
- old/new container and image identities;
- whether recreation occurred;
- readiness result and attempts;
- protected-service identity result;
- recovery result and final outcome.

Environment dumps, raw YAML, credentials, full model inventories, and unbounded logs are excluded.

The operation continues using the project's accepted hand-rolled `ai_ops_mode`, command-based Docker/Podman-compatible Compose prefix, and checksum/baseline state machine rather than adding a Docker-specific Ansible collection or separate deployment framework.

## Risks / Trade-offs

- [Running image lacks `validate-config`] -> Treat validation-capable image rollout as an explicit prerequisite and fail before upload; never fall back to an approximate parser.
- [Static validation cannot see live channel-prefix conflicts] -> Require the replacement to pass existing runtime initialization/readiness; failure enters automatic file restoration and verified recreation.
- [Upstream CPA/AISIX outage keeps `/readyz` unavailable] -> Use bounded retries, restore the old file on failure, and report recovery-required if old configuration also cannot regain readiness rather than claiming success.
- [Plan diff exposes private operational policy] -> Keep it in the operator session; receipts store digests only and YAML continues to forbid credentials.
- [Container runtime health representations differ] -> Use the sidecar's existing `healthcheck` command through the shared Compose invocation instead of parsing provider-specific health JSON.
- [A concurrent manual file edit occurs during activation] -> Post-activation remote and in-container digest checks fail closed; shared drift/baseline rules govern the next attempt.

## Migration Plan

1. Add and test `validate-config` without changing normal server or healthcheck behavior.
2. Build, publish/preload, pin, and deploy a validation-capable models-enricher image through the existing reviewed image process.
3. Add the dedicated Ansible tags, verification task, bounded readiness defaults, receipts, documentation, and fixture rehearsal.
4. Copy the reviewed live remote YAML into ignored `ansible/private-config/models-enricher/config.yaml`; verify Git ignores it.
5. Run plan mode. When local and remote bytes match, apply establishes the baseline without recreation.
6. Edit the local private YAML, rerun plan, review validation and diff, then apply.
7. Confirm only `models-enricher` receives a new container ID, readiness passes, adjacent container IDs remain unchanged, and the receipt contains no YAML or secrets.
8. Rehearse a candidate readiness failure and prove old-file restoration and bounded recovery before declaring production acceptance.
