# AI-gateway Ansible operations

Push-model deploy: edit local private config → review locally → upload + activate on remote.
Ansible never constructs configuration on the remote.

## Layout

- `ops.yml` — the single entry playbook. Select the operation with `--tags` (see Usage).
- `inventory/hosts.ini` — gateway hosts.
- `inventory/group_vars/gateway.yml` / `inventory/host_vars/<host>.yml` — operator
  configuration (YAML). `examples/vars.yml` is the documented template to copy from.
- `roles/ai_ops/defaults/main.yml` — every operator-tunable variable, with default +
  comment. Override from inventory; never edit role files for local settings.
- `roles/ai_ops/tasks/` — shared preflight and component file operations.
- `private-config/` — **gitignored** operator's private config directory
  (`compose.yaml`, `compose.override.yaml`, `.env`, `aisix/resources.yaml`,
  `cpa-model-sync/config.json`, `data/egress-proxy/policy.json`), adopted from
  production at baseline.

## Usage

```bash
# plan (read-only: shows diff, no upload, no restart)
ansible-playbook ops.yml --tags deploy -e service=cli-proxy-api -e ai_ops_mode=plan

# After reviewing the plan digest; applies may interrupt in-flight streams.
# Registry profiles pull by immutable ref; preload-only profiles never pull.
ansible-playbook ops.yml --tags deploy -e service=cli-proxy-api \
  -e ai_ops_mode=apply -e ai_ops_front_approved_digest='<reviewed-plan-digest>'

# Squid policy (local render + pinned `-k parse` validation, then upload + restart)
ansible-playbook ops.yml --tags deploy-squid -e ai_ops_mode=plan
ansible-playbook ops.yml --tags deploy-squid

# Repair only the stored source policy, without rendering/uploading the tree or restarting.
# Requires an intact deployed manifest and matching source hash in deployed squid.conf.
ansible-playbook ops.yml --tags sync-squid-source -e ai_ops_mode=plan
ansible-playbook ops.yml --tags sync-squid-source

# AISIX routes (orphan candidates are REPORTED only; delete locally then deploy)
ansible-playbook ops.yml --tags deploy-aisix -e ai_ops_adopted_direct_models='["direct/a"]'

# Model-sync policy: preview reads live CPA but never PATCHes/recreates/uploads.
# Review every addition/removal and copy the printed digest into apply.
ansible-playbook ops.yml --tags deploy-model-sync -e ai_ops_mode=plan
ansible-playbook ops.yml --tags deploy-model-sync \
  -e ai_ops_model_sync_approved_digest='<approved-digest>'

# Guarded model-sync rollback re-runs the old policy and proves CPA read-back.
ansible-playbook ops.yml --tags rollback-model-sync

# Models-enricher config: validate candidate with the deployed binary, then
# force-recreate only models-enricher and verify digest + readiness.
ansible-playbook ops.yml --tags deploy-models-enricher-config -e ai_ops_mode=plan
ansible-playbook ops.yml --tags deploy-models-enricher-config

# Guarded models-enricher rollback restores the protected YAML and proves readiness.
ansible-playbook ops.yml --tags rollback-models-enricher-config

# Bounded host application recovery (Docker only): deploy local APISIX health
# isolation first. This copies the guard/units, verifies probes, enables the timer;
# it does not deploy application routes or recreate containers.
ansible-playbook ops.yml --tags deploy-recovery -e ai_ops_mode=plan
ansible-playbook ops.yml --tags deploy-recovery -e ai_ops_mode=apply

# Rollback needs its own fresh plan and approval, not the deploy digest.
ansible-playbook ops.yml --tags rollback -e service=cli-proxy-api -e ai_ops_mode=plan
ansible-playbook ops.yml --tags rollback -e service=cli-proxy-api \
  -e ai_ops_mode=apply -e ai_ops_front_approved_digest='<reviewed-rollback-digest>'

# discover the operation surface
ansible-playbook ops.yml --list-tags
```

## Variables

Every operator-facing variable is declared with a default and a comment in
`roles/ai_ops/defaults/main.yml`, and summarized in `examples/vars.yml`. All use
the `ai_ops_` prefix. Set them in `inventory/group_vars/gateway.yml` (group-wide)
or `inventory/host_vars/<host>.yml` (per-host); inventory always wins over role
defaults. Key ones:

| Variable | Default | Meaning |
|---|---|---|
| `ai_ops_config_dir` | `ansible/private-config/` | private config source dir (gitignored) |
| `ai_ops_deploy_root` | `/root/AI-gateway` | remote deployment root |
| `ai_ops_compose_project` | `ai-gateway` | compose project name |
| `ai_ops_compose_cmd` | auto-detect | compose provider (`docker compose` / `podman compose` / `podman-compose`) |
| `ai_ops_mode` | `apply` | `plan` = read-only diff |
| `ai_ops_local_compose_cmd` | `docker compose` | Docker Compose v2 controller command for offline component rendering; not podman-compose |
| `ai_ops_front_approved_digest` | `""` | exact reviewed plan digest for **every component**, including generic deploy/rollback |
| `ai_ops_front_verify_retries` / `ai_ops_front_verify_delay_seconds` | `30` / `2` | bounded component readiness wait |
| `ai_ops_component_command_timeout` | `600` | seconds per async Docker pull/recreate; controller timeout must be at least 1800 seconds |
| `ai_ops_lock_transaction` / `ai_ops_lock_min_age_seconds` | `""` / `900` | exact owned transaction and minimum age for guarded stale-lock recovery; age cannot be below 900 |
| `ai_ops_min_mem_mb` / `ai_ops_min_disk_mb` | `200` / `1024` | capacity gate thresholds |
| `ai_ops_squid_image` | `ai-gateway-squid:6.13-2-deb13u3-r1` | pinned image for offline parse validation |
| `ai_ops_model_sync_approved_digest` | `""` | exact digest printed by the reviewed model-sync plan; required when the policy changes |
| `ai_ops_model_sync_verify_retries` / `ai_ops_model_sync_verify_delay_seconds` | `30` / `2` | bounded wait for the recreated sidecar's immediate round |
| `ai_ops_model_sync_log_tail` | `200` | maximum current-container log lines parsed for the round summary |
| `ai_ops_models_enricher_verify_retries` / `ai_ops_models_enricher_verify_delay_seconds` | `30` / `2` | bounded wait for the recreated models-enricher readiness |
| `ai_ops_adopted_direct_models` / `ai_ops_standalone_retained` | `[]` | AISIX orphan-report approval/exclusion lists |

## Worker-owned CPA configuration

`data/cpa/conf/config.yaml` belongs to CPA and cpa-model-sync. Scheduled model
updates legitimately change it. A private-config copy is an optional initial
seed, not desired state. Only the `cli-proxy-api` and `cpa-model-sync` component
profiles check that the **live** file exists and parses as a YAML mapping, without
printing secrets or enforcing a schema. CPA upgrades/UI edits cannot block
unrelated profiles such as keeper. Preflight never writes, repairs or bootstraps
this file. It is not an input to file deploy/rollback operations.

Ansible component operations manage only `compose.yaml`, `compose.override.yaml`
and `.env`. CPA runtime config was never in that three-file CAS/baseline set.
The 2026-10-24 "13-file global gate" was a manual `exercise.py` precheck, not
Ansible behavior; CPA model drift did not itself block Ansible component plans.
Exclude this worker-owned file from such external equality gates. Historical
`data__cpa__conf__config.yaml.baseline` files are not authoritative: do not advance
or restore them, and do not repeatedly adopt scheduled model changes into a seed.

For a new host, initialize this file explicitly **before** planning a component.
After confirming it is missing, prepare its parent directory privately (0700)
and review the private seed. From `ansible/`, this one-time copy refuses to replace
an existing live file, including one created after the missing-file check:

```bash
ansible gateway -l rainyun-la -m ansible.builtin.copy \
  -a 'src=private-config/data/cpa/conf/config.yaml dest=/root/AI-gateway/data/cpa/conf/config.yaml force=false mode=0600' \
  --extra-vars '{"ansible_diff_always": false}'
```

Keep command output private and do not use `--diff`; the file contains credentials.
Read back the file and run the normal read-only component plan to validate it.
This is initialization, not permission to overwrite a running CPA configuration.

For the generic cpa-model-sync component profile, `ready` uses the existing
completed-cycle progress signal (`worker_health.ok`), together with running,
unpaused, non-restarting process state. A stalled/missing progress signal is not
healthy. `upstream_ready` reports the existing dependency-ready signal separately;
rejected upstream credentials do not force worker activation. This does not weaken
the separate `deploy-model-sync` policy-change preview/readback contract below.

## Drift gate

Component `deploy`/`rollback` use the grouped three-file and runtime CAS described
below. Scoped file operations (`deploy-aisix`, `deploy-model-sync`) checksum each
managed remote file against `.ai-ops-state/<file>.baseline`; a mismatch stops the
deploy before upload. Baselines advance only after activation and readback, not
during a multi-file upload. A file-only preparation defers its baseline to the
caller's final verification. A reviewed, already-matching single-file adoption
still establishes its baseline after readback without recreating. The grouped
component path leaves historical baseline drift for explicit reconciliation. Squid policy deploys (`--tags deploy-squid`)
upload one tarball + a `sha256sum` manifest over all generated files; the drift
gate verifies the whole remote tree against the last deployed manifest
(`sha256sum -c`), which keeps the same guarantee at ~2 SSH round trips instead of
per-file checks (177 policy files would cost ~900 connections). If the remote was
edited by hand: review the shown diff/failing files, adopt the change into the
local config, and re-run. Rollback likewise refuses to overwrite a remote file
that changed independently after the deploy.

## Guarded component rollout

All `ai_ops_profiles` entries, including keeper and bridge, share one grouped
`deploy`/`rollback` transaction. Generic profiles no longer upload three files
and advance their baselines before pulling or recreating. A rollback requires a
snapshot from this guarded path; legacy unowned `.predeploy` files are not
silently treated as a reviewed transaction.
The scoped `deploy-standard-cache-path`, `restart-openai-api-cache-keeper` and
`rollback-standard-cache-path` tags remain retired, like the older
`deploy-response-keeper`, `deploy-responses-path` and `restart-response-keeper`.
Preflight rejects all six before runtime detection or managed writes.

Use Docker Compose v2 on both host and controller. Rendering needs no local
Docker daemon. `podman-compose` does not provide the required
`--no-path-resolution --format json` contract. A task-local standalone Compose
binary can be selected with `ai_ops_local_compose_cmd`; do not install global
packages for a one-off plan.

```bash
# From ansible/, with private-config adopted from production, not the public example.
ansible-playbook ops.yml --tags deploy -e service=openai-api-cache-keeper -e ai_ops_mode=plan
ansible-playbook ops.yml --tags deploy -e service=responses-ws-sse-bridge -e ai_ops_mode=plan

# After explicit approval of the selected plan and interruption window:
ansible-playbook ops.yml --tags deploy -e service=openai-api-cache-keeper \
  -e ai_ops_mode=apply -e ai_ops_front_approved_digest='<reviewed-plan-digest>'

# Explicit rollback also needs a fresh plan and its own approval digest.
ansible-playbook ops.yml --tags rollback -e service=openai-api-cache-keeper -e ai_ops_mode=plan
ansible-playbook ops.yml --tags rollback -e service=openai-api-cache-keeper \
  -e ai_ops_mode=apply -e ai_ops_front_approved_digest='<reviewed-rollback-digest>'
```

Plan reads configuration hashes, effective Compose and live container identities;
it never uploads, pulls or recreates. Secret-bearing Compose/environment output
is suppressed. `config_diff: false` means the three source files equal live bytes,
not that an apply is needed or that apply was tested. Plan executes the **same
remote argv prefix** with `config --quiet`, prints the exact validation/pull/up
argv, and verifies the candidate is present by `docker image inspect` or pullable
by read-only `docker manifest inspect`. Registry failure is not bypassed. Plan
also shows the image proof, readiness source, writer-lock owner, approved
non-secret environment fields, namespace owner, protected-container count and
digest.
The digest binds the source location, all three candidate/live SHA256 hashes,
selected container identity/state and every protected project-container identity/state.
A stale digest refuses apply before mutation. All component applies share one
exclusive lock; planning never acquires it. The lock records the transaction,
UTC epoch start, original controller host/PID and approval digest. The PID is
the outermost Ansible controller, not a short-lived task worker. Durable
`transaction-<id>.json` progress records survive controller death; guarded recovery
is below. Never delete a lock merely because a controller timed out.

Only the selected service's **image/environment** may change. All other effective
Compose settings, mounts, resource limits, dependencies, networks and services
must remain identical. Registry images require a SHA256 digest; keeper and bridge
also require a 40-character source revision. Explicit preload-only profiles may
use their existing local tag, with its image ID bound into the approval.
`cloudflared`, `cpa-model-sync` and `egress-proxy` are preload-only (`pull: false`).
Production cloudflared uses a `rollback-20261015/...` archive alias: its local
image ID is not the public registry image's config ID, even though the alias
carries the public index digest. Do not replace it with a registry ref without
proving identical local image identity in a separately reviewed migration.
The image preflight rejects `rollback-*` and `localhost/` aliases in any
pull-enabled profile, even when a local digest matches; a local match does not
prove that a registry can resolve the alias. Missing preload images fail closed
without contacting a registry. Keeper stays `UPSTREAM_URL=http://127.0.0.1:8086` with
`IGNORE_CLIENT_STORE_FALSE=true`; bridge stays loopback `:8086` with `PORT=8086`.
Neither profile can add provider credentials or routing. Compose runs with a
clean process environment so ambient shell exports cannot override `.env`.

Apply calls `docker pull <immutable-ref>` directly, independent of Compose's
`pull_policy: never`; preload-only and rollback branches never pull. Before any
upload/recreate, `docker image inspect <ref>` must prove the local ID/RepoDigest
matches the pin (multiarch index digest need not equal the config ID). A skipped
pull or wrong digest fails before activation. It checks fresh hashes/identities
again and creates a private grouped snapshot (`0700` directory, `0600` files) under
`.ai-ops-state/front-<service>-*`. It checks source/snapshot/live hashes again,
uploads the reviewed three files, checks effective configuration, then runs only
`up -d --force-recreate --no-deps --no-build --pull never <service>`. Readback
requires the exact image ID/reference, **all configured environment values**,
readiness, correct namespace owner/inode where shared, and every adjacent
container's ID/image/StartedAt/state unchanged. Readiness uses Docker
`health=healthy` when available. Without a HEALTHCHECK it calls existing
`gateway-recovery.py` `OBSERVE_HTTP`/`namespace_probe` code (cloudflared, AISIX,
aisix-status, model-catalog-sidecar), or the existing completed-cycle readiness
check for cpa-model-sync. It never invents a passing probe or starts a worker
sync; profiles with no known probe fail closed. Only after these checks and a
fresh group readback are all three baselines committed. The private snapshot contains a sanitized success receipt; the guarded
rollback reference is `.ai-ops-state/front-<service>-last.json`.

A failed activation stops with a private `front-<service>-failure.json` receipt;
it does **not** blindly restore over an unknown live state. `last.json` alone is
not rollback authority: the newest transaction receipt for that profile must
name that snapshot. A newer failed/aborted attempt without its own snapshot
refuses rollback instead of silently undoing an older deployment. The live
container ID/image/StartedAt/state must exactly match the snapshot's recorded
**after** identity (successful readback, or fresh failure readback bound to that
transaction). Missing/unknown failure readback, including legacy failed receipts,
fails closed for manual reconciliation; there is no override flag. Refusals show
live/snapshot identities without credentials. Existing old/candidate file-hash,
protected-container and intact-snapshot checks still apply. Rollback never pulls
or builds and must read back the original image ID. Another intervening operation
requires manual reconciliation.
After any emergency edit or rollback, adopt the resulting files back into local
`private-config` immediately. The host Git checkout is never a deployment source.

Coordinate one production writer and a maintenance window. **Recreating keeper
or bridge interrupts in-flight streams**. The named volume survives, but keeper
intentionally wipes its alias/history buckets on startup; restart does not
restore cached continuations. This path does not change Sub2API accounts, restart dependencies,
repair a failed whole-stack boot, or prove provider continuation. Follow the
[WS decision](../docs/websocket-termination.md) and
[routing checklist](../docs/model-routing.md#checklist-before-pushing-or-deploying);
real SSE/WS terminal and reconnect acceptance is a separate authorized check.

### Controller deadline, logs and stale-lock recovery

Repository `ansible.cfg` disables SSH PTY allocation (`usetty=false`). Tailscale
SSH opens a su/PAM session; with a PTY, OSC 3008 context markers can prefix async
wrapper JSON on the same line and break Ansible decoding. Keep automation
non-interactive rather than editing host PAM or shell profiles.

Allow **at least 1800 seconds** for the whole controller invocation. Docker
pull/recreate each have a 600-second remote async bound; that is not the whole
playbook's duration. Do not put a 300-second `subprocess.run(timeout=...)` around
apply or discard partial stdout on timeout. Keep a private streamed log:

```bash
umask 077
mkdir -p /var/tmp/ai-gateway-ops-logs
set -o pipefail
timeout --signal=TERM --kill-after=30s 30m \
  ansible-playbook ops.yml --tags deploy -e service=models-enricher \
  -e ai_ops_mode=apply -e ai_ops_front_approved_digest='<reviewed-plan-digest>' \
  2>&1 | tee /var/tmp/ai-gateway-ops-logs/models-enricher-apply.log
```

The durable progress receipt records locked → preparing-image → image-ready →
uploading-config → activating → verifying → committing-baselines → complete.
Each update is atomic, mode 0600 and fsynced. A killed controller can leave a
stage marked *in-progress*: it does **not** prove that stage's action failed or
that activation did not happen. Inspect the current image/env/namespace, all
protected containers, three file hashes, baselines and snapshot before deciding
to complete or roll back. No automatic rollback is claimed. Ordinary Ansible
failure writes a failed receipt before releasing only its owned lock.

Recovery is a separate operation, not a deployment retry:

```bash
ansible-playbook ops.yml --tags recover-stale-lock -e ai_ops_mode=plan
# Review owner.json and matching transaction-<id>.json first.
ansible-playbook ops.yml --tags recover-stale-lock -e ai_ops_mode=apply \
  -e ai_ops_lock_transaction='<exact-32-hex-transaction>'
```

Apply requires a lock at least 900 seconds old, matching owner/progress receipt,
the original controller host with its recorded PID gone, and no other
Ansible/AnsiballZ/Docker Compose/pull writer locally or remotely (excluding only
this recovery command's own ancestry). Any unknown/mismatch leaves the lock.
Legacy empty/unowned locks require manual investigation, not this recovery tag.
Recovery records `recovery-required`, releases ownership, and changes **no live
configuration, baseline or container**. Plan shows the owner without creating
state. This also preserves the experiment monitor's empty writer lock: it cannot
be mistaken for a stale Ansible transaction.

Offline lifecycle verification (fake Docker, no production apply):

```bash
python3 scripts/test-component-deploy.py
python3 scripts/test-component-apply.py --evidence /var/tmp/component-apply-tests
```

## Model-sync policy safety

`deploy-model-sync` requires a running preview-capable sidecar image before the
first managed policy change. First adopt the reviewed live
`cpa-model-sync/config.json` into the ignored private path; when local and remote
bytes match, readback establishes the reviewed baseline without recreation.

For a changed policy, plan mode streams local JSON to `cpa-model-sync --preview -`
inside the existing container. Preview performs only CPA/source reads and prints
complete per-channel additions/removals plus an approval digest. Apply recomputes
that digest and stops before upload if the image, policy, channel identities,
current model sets, or source inventories changed. After sidecar-only recreation,
Ansible requires a successful immediate round and CPA read-back whose current-set
digest equals the approved desired-set digest. Failure restores the old policy,
recreates the sidecar, and proves recovery against the pre-apply current-set
digest; an unproven recovery is reported as failure, never success.

## Models-enricher config safety

`deploy-models-enricher-config` requires a running validation-capable
models-enricher image (one that provides `validate-config`) before the first
managed config change. First adopt the reviewed live
`models-enricher/config.yaml` into the ignored private path
`ansible/private-config/models-enricher/config.yaml`; when local and remote
bytes match, readback establishes the reviewed baseline without recreation.

For a changed config, plan mode streams local YAML to
`/models-enricher validate-config -` inside the running container — the same
config parser used at startup (YAML, required fields, the shared `metadata` chain
schema, globs, rejected Go-era legacy fields) — and shows the effective file diff.
The TypeScript image prints `{"version":1,"config_digest":"<sha256>"}` on success;
the digest is what the post-recreate check compares. Since the TypeScript rewrite
the config uses the openai-api-pi-extension chain schema (`metadata.chain`,
`metadata.models`, `metadata.include_models`); a Go-era file fails validation with a
migration hint. Apply revalidates,
uploads, and force-recreates only `models-enricher` (`--no-deps --no-build`),
then requires a fresh container on the same image whose mounted `/app/config.yaml`
digest matches the candidate, whose `healthcheck` becomes ready within bounded
retries, and whose namespace owner and adjacent relay container IDs stayed
unchanged. Failure restores the protected pre-deploy YAML, recreates the
sidecar, and proves the restored digest plus readiness; an unproven recovery is
reported as recovery-required, never success.

## Operational notes

- **AISIX loads `resources.yaml` only at startup.** Because it is a bind mount, a
  file-only change does not alter the container config hash and plain
  `up -d` no-ops. `deploy-aisix` therefore activates with `--force-recreate`.
  Fixture tests miss this (fixture containers are freshly created); caught live
  on rainyun-la 2026-09-19.
- **models-enricher loads `config.yaml` only at startup**, so config deploys
  use the same `--force-recreate` activation; the tracked
  `models-enricher/config.yaml` is an image/build default — production desired
  state lives only in the gitignored private config dir.
- **The post-deploy Admin API route check is optional** (`failed_when: false`):
  the internal relay path currently returns 401, so effective-route verification
  is pending a working credential path. Until then, verify via
  `docker logs ai-gateway-aisix-1 | grep 'resources loaded'` and live traffic.

## Deliberate deviations from Ansible conventions

These are accepted design decisions, not defects (see the
`normalize-ansible-structure` change's design.md):

- **Hand-rolled `ai_ops_mode=plan|apply` instead of `--check`**: check-mode support
  across `command`/`shell`/`copy` is partial, and plan must show real remote diffs.
- **`command`/`shell` compose invocation instead of the `docker_compose_v2` module**:
  the drift gate already performs change detection; the module would add a
  collection dependency without changing guarantees.
- **Custom drift-gate/baseline/receipt state machine**: remote hand-edits are a real
  incident source for this deployment; Ansible has no native equivalent.

Secrets live only in gitignored local files (`private-config/`); receipts under
`.ai-ops-state/` are sanitized (mode 0600, secret-bearing tasks use `no_log`).
