## Context

See `proposal.md` for motivation and the delta specs for required behavior.

`cpa-model-sync` loads its JSON at startup, discovers the five managed CPA key-channel kinds, fetches source inventories, filters each inventory, PATCHes changed CPA model arrays, prints per-channel status objects plus one aggregate summary, and then sleeps. A syntactically valid but mistaken `include` or `exclude` expression can therefore complete with zero failures while removing legitimate models. Container health and the aggregate summary prove execution, not operator intent.

The production service is already isolated and bounded: no listener, one private CPA network, read-only filesystem, non-root user, and no dependency beyond CPA readiness. Its policy is bind-mounted at `./cpa-model-sync/config.json`. The Ansible tooling already provides local/remote/baseline checksums, plan diffs, protected pre-deploy copies, uploads, activation rescue, and guarded rollback through `deploy-file.yml`.

The safe operation must bind three different states:

```text
local candidate policy
        |
        v
read-only preview against fresh CPA/source state
        |
        v
approved per-channel additions/removals + digest
        |
        v
upload + recreate + normal immediate sync
        |
        v
CPA read-back == approved desired model sets
```

## Goals / Non-Goals

**Goals:**
- Manage the private model-sync policy without interactive server edits.
- Reuse the Rust parser, regex engine, discovery, inventory, and filtering logic for a read-only preview.
- Show complete per-channel model additions and removals before approval.
- Bind apply to the exact policy, image, current CPA state, source inventories, and desired model sets that were reviewed.
- Verify actual CPA model arrays after the normal sidecar synchronization round.
- Restore the old policy and prove recovery against a pre-apply inventory snapshot when activation or verification fails.
- Preserve no-op idempotence and bounded, sanitized evidence.

**Non-Goals:**
- Change filter semantics, managed channel kinds, retries, scheduling, or CPA write APIs.
- Add hot reload, an HTTP server, a second synchronization service, or billable inference probes.
- Track the private production policy in Git or synthesize it from Ansible variables.
- Guarantee that future upstream inventory changes cannot alter later periodic rounds; this operation approves and verifies the deployment-time transition.
- Restart CPA, rebuild images on production, or overwrite CPA arrays directly during recovery.

## Decisions

### 1. Add one sidecar-native read-only preview mode

`cpa-model-sync` will gain a mode equivalent to:

```text
cpa-model-sync --preview -
```

`-` reads candidate JSON from stdin so Ansible need not upload a temporary file. Preview will:

1. deserialize the same `Config` with `deny_unknown_fields`;
2. validate request and interval limits;
3. compile every Rust `regex` include/exclude expression;
4. discover managed CPA channel identities;
5. read current CPA model arrays and upstream source inventories;
6. calculate desired sets through the same filtering function used by synchronization;
7. emit bounded JSON and exit without calling PATCH.

The filtering calculation will be factored out of the write path rather than independently reimplemented. Tests will prove that preview desired sets equal the sets supplied to synchronization for the same fixtures.

A separate Python validator was rejected because Python and Rust regex semantics differ and duplicated schema rules would drift. A validation-only mode without CPA reads was also insufficient: it catches malformed policy but cannot reveal a valid expression that removes unintended models. `--once` was rejected for planning because it performs the mutation under review.

### 2. Execute preview inside the currently running sidecar container

For a changed local policy, Ansible will stream candidate bytes to the executable inside the existing `cpa-model-sync` container with `compose exec -T`. This starts no new container, writes no remote candidate file, reuses the existing private network and management credential, and exercises the deployed binary. Preview output is read-only CPA/source traffic.

The current container image identity is captured and included in approval. Apply verifies that the service definition and running image identity still match before upload and that the recreated container uses the same approved image. The feature image containing `--preview` must therefore be built, published, pinned, and deployed before the first managed policy operation; production builds remain forbidden.

Running a local validation image was rejected because a private `localhost/...` image may not exist on the operator host and would not prove parity with production. Starting a temporary remote Compose container was rejected because it adds network/IP collision and cleanup concerns.

### 3. Preview output has a human diff and a deterministic approval digest

For each `(kind, prefix)` preview emits sorted model IDs for:

- current CPA set;
- desired set;
- additions;
- removals;
- unchanged count;
- configured-skip state.

It also emits digests for the candidate policy bytes, running image identity, current sets, fetched source inventories, desired sets, and a versioned global approval digest over canonical JSON. Ordering is deterministic. Credentials, authorization headers, request bodies other than model IDs, and raw management records are never emitted.

Plan mode displays the per-channel additions/removals and global digest. Model IDs are operational review data and may appear in the interactive plan, but receipts retain only counts and digests. Apply requires an explicit `ai_ops_model_sync_approved_digest` matching a fresh preview. If policy bytes, image, current CPA sets, discovered identities, or source inventories changed, apply stops before upload and requests a new plan.

A checksum of the JSON file alone was rejected because it does not bind the dynamic inputs that determine the actual model arrays.

### 4. Use a dedicated `deploy-model-sync` tag

The operation uses:

- local desired file: `{{ ai_ops_config_dir }}/cpa-model-sync/config.json`;
- remote bind mount: `{{ ai_ops_deploy_root }}/cpa-model-sync/config.json`;
- service: `cpa-model-sync`;
- explicit approval input: `ai_ops_model_sync_approved_digest`.

It remains in the single `ops.yml` entry point beside `deploy-squid` and `deploy-aisix`, using the shared `_compose` prefix. A dedicated tag avoids uploading Compose files or `.env` when only filters change. Repository `config.example.json` remains an example, never production desired state.

If local, remote, and baseline policy checksums already match, the operation is a no-op and neither preview nor recreation runs. If remote equals a reviewed local file but no baseline exists, first adoption establishes the baseline without synchronization.

### 5. Force-recreate only the sidecar after fresh approval

After apply recomputes and accepts the approved digest, `deploy-file` uploads the policy and activates with the equivalent of:

```text
compose up -d --force-recreate --no-deps --no-build cpa-model-sync
```

`--force-recreate` is required because bind-mounted bytes do not change Compose's service hash and the process keeps startup-loaded configuration. `--no-deps` protects CPA and `--no-build` prevents production-host builds.

Adding only a generic component profile was rejected: generic deploy neither owns the policy nor force-recreates on file-only change, and its current verification proves only Compose presence.

### 6. Success requires execution evidence and CPA read-back

Before upload, Ansible preserves the fresh preview's current per-channel sets as a protected pre-apply snapshot. After recreation it:

1. identifies the new container and its image;
2. waits within a bounded timeout for that container's first aggregate summary;
3. requires zero `failed` and zero `unconfirmed` channels;
4. requires the service to remain running;
5. runs read-only preview/inspection again against the deployed policy;
6. compares each actual current CPA set with the approved desired set captured before mutation.

The post-apply preview may observe a newer upstream source inventory, but acceptance compares its current CPA sets to the already approved desired sets, not to newly calculated desired sets. This proves the transition that was authorized without pretending to freeze future periodic source changes.

A successful summary without set equality is failure. `compose ps` alone and a duplicate `--once` probe were rejected because neither proves the approved arrays are present, and the duplicate probe can obscure which process performed the mutation.

### 7. Recovery is compensating reconciliation plus proof

Model-sync verification must finish before the candidate policy becomes the recorded baseline. The shared deploy lifecycle will be minimally extended so model-sync can run post-activation verification inside the same rescue boundary and provide distinct candidate and recovery verification inputs. Existing callers remain unchanged.

On candidate failure the operation restores the protected old policy and force-recreates only `cpa-model-sync`. The restored process performs its normal immediate round. Read-back is then compared with the captured pre-apply current sets. Equality proves recovery. Any difference, timeout, failed round, or unconfirmed round is reported as recovery-required and the old baseline is retained.

Restoring only the file without re-running old reconciliation was rejected because candidate execution may already have changed CPA arrays. Directly PATCHing the snapshot from Ansible was rejected because it would duplicate the sidecar's writer and identity safeguards.

### 8. Evidence is split between review, protected state, and receipt

- Interactive plan: complete model additions/removals plus counts and approval digest.
- Protected `0600` operation state: pre-apply current sets and approved desired sets needed for verification/recovery.
- Sanitized receipt: policy/image/container identities, approval digest, freshness result, mutation status, aggregate synchronization counts, per-channel desired/observed counts and digests, recovery result, and skipped checks.

The management key, environment, raw CPA records, unbounded logs, and complete model lists are excluded from receipts.

The project continues using its deliberate hand-rolled `ai_ops_mode`, command-based Docker/Podman-compatible Compose invocation, and checksum/baseline state machine. This change extends those decisions rather than introducing a Docker-specific Ansible module or native check-mode rewrite.

## Risks / Trade-offs

- [Preview exposes operational model IDs in plan output] -> Keep output local to the operator, never include credentials or raw account records, and store only counts/digests in receipts.
- [Upstream inventory changes between plan and apply] -> Fresh apply preview changes the digest and blocks before upload.
- [Upstream inventory changes immediately after apply starts] -> Acceptance remains bound to the approved desired sets; later periodic convergence is normal sidecar behavior and outside this transaction.
- [Candidate sync partially changes CPA before failing] -> Restore old policy, run its normal reconciliation, and prove equality with the captured pre-apply sets; otherwise report recovery-required.
- [Old policy cannot recreate the exact snapshot because upstream changed] -> Do not claim recovery; retain old policy/baseline and surface the exact mismatching channel digests for manual repair.
- [Preview output contract changes] -> Version its canonical schema and digest input, fail closed on unknown versions, and cover parity/canonicalization with Rust tests.
- [Large inventories make plan output noisy] -> Show complete additions/removals, not complete unchanged sets; retain counts and digests for unchanged data.
- [Running image lacks preview mode] -> Treat the versioned preview-capable image rollout as an explicit prerequisite and stop without fallback or production build.

## Migration Plan

1. Refactor desired-set calculation and add/test the read-only preview mode without changing daemon or `--once` semantics.
2. Build, publish, and pin a preview-capable versioned sidecar image; deploy that image through the existing approved component flow before enabling policy management.
3. Add the dedicated Ansible operation, approval variable, verification/recovery integration, documentation, and fixtures.
4. Adopt the current remote policy into the ignored local private path; verify local bytes match remote and establish the first baseline without recreation.
5. Change the local include/exclude policy and run plan mode. Review every per-channel addition/removal and record the digest.
6. Apply with the approved digest. Require a fresh matching preview before upload, then recreate only `cpa-model-sync`.
7. Verify the first normal synchronization summary and CPA read-back against the approved desired sets; confirm CPA and unrelated container identities did not change.
8. Rehearse a bounded failure/recovery case and verify read-back returns to the captured pre-apply sets before declaring production acceptance.
