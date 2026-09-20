## 1. Sidecar Configuration Validation

- [x] 1.1 Add `models-enricher validate-config <path|->` using the existing `loadConfig` path, returning versioned bounded JSON with the exact input SHA-256 and no network/listener side effects; verify focused Go tests cover stdin, file input, valid config, malformed YAML, missing required fields, invalid source tokens, invalid mappings, and invalid regexes.
- [x] 1.2 Preserve existing server and `healthcheck` command behavior while adding validation dispatch; verify existing `models-enricher` tests pass and a validation invocation performs no CPA/AISIX/source requests.
- [x] 1.3 Add tests proving validation output/errors never contain environment credentials or YAML content beyond bounded field paths/messages, and repeated validation of identical bytes returns an identical digest/result.

## 2. Ansible Operation Surface

- [x] 2.1 Add dedicated `deploy-models-enricher-config` and `rollback-models-enricher-config` tags using `{{ ai_ops_config_dir }}/models-enricher/config.yaml` and `{{ ai_ops_deploy_root }}/models-enricher/config.yaml`; verify `ansible-playbook ansible/ops.yml --list-tags` and syntax check expose both operations.
- [x] 2.2 Run the deployed sidecar's `validate-config -` against changed local bytes before apply, including plan mode, and fail closed when the running image lacks the command or rejects the candidate; verify a fixture plan uploads nothing, recreates nothing, and displays the effective local/remote diff.
- [x] 2.3 Capture pre-activation models-enricher container/image identity and protected namespace/relay identities, then deploy through the shared `deploy-file` role with mode `0644` and `--force-recreate --no-deps --no-build models-enricher`; verify a changed fixture recreates only the target service and a matching local/remote/baseline fixture remains a no-op.
- [x] 2.4 Declare bounded readiness retry/delay variables in role defaults and document inventory override examples; verify every new tunable uses the `ai_ops_` prefix and appears once with its default.

## 3. Verification, Recovery, and Rollback

- [x] 3.1 Add focused post-activation verification that requires a fresh target container on the same image, validates `/app/config.yaml` through the sidecar, matches its digest to the candidate and remote file, waits for the existing healthcheck, and proves protected container identities unchanged; verify success plus wrong-digest, reused-container, changed-image, changed-protected-container, exit, and timeout fixtures.
- [x] 3.2 Wire candidate and recovery verification into the shared deploy rescue boundary so baseline advances only after success; verify failed candidate activation restores the protected YAML, recreates only models-enricher, and reports recovery complete only after old digest and readiness are proven.
- [x] 3.3 Implement guarded manual rollback through `rollback-file.yml` with the same recreation and verification contract; verify independent remote drift blocks rollback and incomplete restored readiness reports recovery-required.
- [x] 3.4 Write bounded deploy, failure/recovery, and rollback receipts containing only digests, image/container identities, mutation scope, readiness/protected-identity results, and outcome; verify fixtures reject YAML content, credentials, environment dumps, raw inventories, and unbounded logs in receipts.

## 4. Documentation and Rehearsal

- [x] 4.1 Update Ansible usage comments, README, private-config inventory, role defaults, and operator examples for image prerequisite, live-config adoption, local editing, plan/apply, no-op behavior, activation scope, rollback, and recovery; verify commands and paths match the playbook.
- [x] 4.2 Add a focused local Ansible rehearsal covering missing validator, invalid YAML/regex, first adoption, unchanged no-op, successful changed deploy, runtime-readiness failure with recovery, protected-container drift, guarded rollback, and recovery-required outcome; verify the script exits successfully and inspects exact Compose calls.
- [x] 4.3 Run focused Go tests, Ansible syntax/list-tag checks, the new rehearsal, repository validation, and `openspec validate add-ansible-models-enricher-config-deploy --strict`; verify all pass with no unrelated Compose, routing, model-enrichment, dependency, or credential changes.

## 5. Production Adoption and Acceptance

- [x] 5.1 Build/publish or preload and pin a validation-capable models-enricher image through the existing approved image workflow; verify ordinary manifest, table, routing-index, and readiness behavior remains unchanged before enabling config deployment.
- [x] 5.2 Fetch the reviewed live YAML into ignored `ansible/private-config/models-enricher/config.yaml`, prove local/remote equality, and establish the baseline without recreation; verify `git check-ignore` covers the private file and the target container identity is unchanged.
- [x] 5.3 Make the intended local YAML edit, run plan mode, review validation and complete diff, then apply; verify only models-enricher receives a new container ID, mounted and remote digests match, readiness passes, protected identities remain unchanged, and the receipt is sanitized.
- [x] 5.4 Rehearse one bounded candidate-readiness failure and verify automatic restoration of the previous digest and readiness; record any unproven restoration as recovery-required before declaring production acceptance.
