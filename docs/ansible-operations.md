# Ansible operations

Push-model deploy: edit local private config → review with `--check --diff` → apply
(upload + activate on the remote). Ansible never constructs configuration on the remote.

## Layout

```
ops/ansible/
├── ansible.cfg            inventory = private-config/inventories/production/hosts.yml, collections_path = ./collections
├── .ansible-lint          profile: production
├── requirements.txt       ansible-core, ansible-lint (pip)
├── requirements.yml       community.docker 5.4.0 + community.library_inventory_filtering_v1 1.1.5
├── collections/           (gitignored) installed from requirements.yml
├── inventories/example/   hosts.yml, group_vars/gateway.yml (every operator knob), host_vars/example.yml
├── playbooks/             deploy.yml rollback.yml deploy-config.yml rollback-config.yml recovery.yml unlock.yml
├── roles/                 ai_ops (shared preflight) + ai_ops_<concern>, each with meta/argument_specs.yml
└── private-config/        (gitignored) inventories/production/, compose*.yaml, .env, services/…, data/…
```

- `private-config/` holds the production inventory and the private desired state
  (`compose.yaml`, `compose.override.yaml`, `.env`, `services/routing/aisix/resources.yaml`,
  `services/cpa/cpa-model-sync/config.json`, `services/routing/model-catalog/config.yaml`,
  the optional `services/routing/model-catalog/catalog.lua`, `data/egress-proxy/policy.json`), adopted from production at baseline. Without the
  private inventory no host matches `gateway` and nothing runs.
- Roles: `ai_ops` (preflight, capacity gate, the shared Compose argv, the renderer pin),
  `ai_ops_managed_file` (drift gate, pre-deploy copy, baseline of one file),
  `ai_ops_component`, `ai_ops_lock`, `ai_ops_recovery`, `ai_ops_squid`, `ai_ops_aisix`,
  `ai_ops_model_sync`, `ai_ops_model_catalog`. Every operator variable has a default and a
  comment in `roles/<role>/defaults/main.yml`; `ansible-doc --type role -r roles <role>`
  shows the argument specs.

Setup on a controller (task-scoped, nothing system-wide):

```bash
cd ops/ansible
python3 -m venv /var/tmp/ai-ops-venv && /var/tmp/ai-ops-venv/bin/pip install -r requirements.txt
ansible-galaxy collection install -r requirements.yml -p collections
mkdir -p private-config/inventories && cp -r inventories/example private-config/inventories/production
# then set the real host in private-config/inventories/production/hosts.yml
```

## Usage

`--check --diff` is the read-only preview; a run without `--check` applies. Each
operation is one playbook; `-e component=<name>` selects the component and is
validated against the declared choices before any connection.

```bash
# Component (image and/or compose/.env change): preview, then apply with the printed digest.
ansible-playbook playbooks/deploy.yml -e component=cli-proxy-api --check --diff
ansible-playbook playbooks/deploy.yml -e component=cli-proxy-api \
  -e ai_ops_component_approved_digest='<reviewed-plan-digest>'

# Rollback needs its own fresh preview and approval, not the deploy digest.
ansible-playbook playbooks/rollback.yml -e component=cli-proxy-api --check --diff
ansible-playbook playbooks/rollback.yml -e component=cli-proxy-api \
  -e ai_ops_component_approved_digest='<reviewed-rollback-digest>'

# Squid policy (local render + pinned `-k parse` validation, then upload + restart).
ansible-playbook playbooks/deploy-config.yml -e component=egress-proxy --check --diff
ansible-playbook playbooks/deploy-config.yml -e component=egress-proxy

# Repair only the stored source policy, without rendering/uploading the tree or restarting.
# Requires an intact deployed manifest and matching source hash in deployed squid.conf.
ansible-playbook playbooks/deploy-config.yml -e component=egress-proxy -e ai_ops_squid_source_only=true

# AISIX routes (orphan candidates are REPORTED only; delete locally then deploy).
ansible-playbook playbooks/deploy-config.yml -e component=aisix \
  -e '{"ai_ops_aisix_adopted_direct_models": ["direct/a"]}'

# Model-sync policy: the preview reads live CPA but never PATCHes/recreates/uploads.
# Review every addition/removal and copy the printed digest into apply.
ansible-playbook playbooks/deploy-config.yml -e component=cpa-model-sync --check --diff
ansible-playbook playbooks/deploy-config.yml -e component=cpa-model-sync \
  -e ai_ops_model_sync_approved_digest='<approved-digest>'

# Model-catalog profile (config.yaml, plus catalog.lua when the private file exists): validate
# with the deployed binary, recreate only model-catalog once, verify both digests + readiness.
ansible-playbook playbooks/deploy-config.yml -e component=model-catalog --check --diff
ansible-playbook playbooks/deploy-config.yml -e component=model-catalog

# Guarded config rollbacks restore the protected pre-deploy copy and prove recovery.
ansible-playbook playbooks/rollback-config.yml -e component=cpa-model-sync
ansible-playbook playbooks/rollback-config.yml -e component=model-catalog

# Bounded host application recovery (Docker only); see gateway-health-and-recovery.md.
ansible-playbook playbooks/recovery.yml --check --diff
ansible-playbook playbooks/recovery.yml

# Stale component lock (see below).
ansible-playbook playbooks/unlock.yml --check
```

The same `component` value means different things in two playbooks: `deploy.yml -e
component=aisix` deploys the AISIX container (image, Compose, `.env`), while
`deploy-config.yml -e component=aisix` deploys `resources.yaml`.

That AISIX operation also reports `config_diff` for the **unmanaged** `services/routing/aisix/config.yaml`, in preview and apply. It compares the live file's SHA-256 with the controller's private file at the same path under `ai_ops_config_dir`; it never uploads this file, advances a baseline or restarts AISIX for it. Missing or unreadable checksums are not a match (`source_exists` / `remote_exists` distinguish missing files). No config contents or credentials are printed, and a difference is reported, not reconciled. `ops/scripts/init.sh` originally creates the private file from the tracked `services/routing/aisix/config.example.yaml` by replacing the admin-key placeholder; Ansible does not render it. Review differences privately and use a separate manual window for any config change.

Tags only filter tasks inside one operation: `validate`, `upload`, `activate`, `verify`.
Gates (argument validation, preflight, drift, approval) are tagged `always`, so
`--tags`/`--skip-tags` cannot skip them; `--skip-tags activate` uploads without
activating, and the baseline then does not advance.

### Migration from `ops.yml`

| Old invocation | New |
|---|---|
| `ops.yml --tags deploy -e service=X` | `playbooks/deploy.yml -e component=X` |
| `ops.yml --tags rollback -e service=X` | `playbooks/rollback.yml -e component=X` |
| `--tags deploy-aisix` / `deploy-squid` / `deploy-model-sync` / `deploy-model-catalog-config` | `playbooks/deploy-config.yml -e component=aisix` / `egress-proxy` / `cpa-model-sync` / `model-catalog` |
| `--tags sync-squid-source` | `playbooks/deploy-config.yml -e component=egress-proxy -e ai_ops_squid_source_only=true` |
| `--tags rollback-model-sync` / `rollback-model-catalog-config` | `playbooks/rollback-config.yml -e component=cpa-model-sync` / `model-catalog` |
| `--tags deploy-recovery` | `playbooks/recovery.yml` |
| `--tags recover-stale-lock` | `playbooks/unlock.yml` |
| `-e ai_ops_mode=plan` | `--check --diff` |
| `--tags deploy-ws-keepalive` | removed: `playbooks/deploy.yml -e component=ai-sse-keepalive-proxy` (the optional OCI source-revision check is dropped) |
| `deploy-response-keeper`, `deploy-responses-path`, `restart-response-keeper` | removed (already retired) |

| Old variable | New |
|---|---|
| `service` (extra var) | `component` |
| `ai_ops_mode` | removed: `--check` |
| `ai_ops_profiles` | `ai_ops_component_profiles` |
| `ai_ops_front_approved_digest` | `ai_ops_component_approved_digest` |
| `ai_ops_front_verify_retries` / `ai_ops_front_verify_delay_seconds` | `ai_ops_component_verify_retries` / `ai_ops_component_verify_delay_seconds` |
| `ai_ops_adopted_direct_models` / `ai_ops_standalone_retained` | `ai_ops_aisix_adopted_direct_models` / `ai_ops_aisix_standalone_retained` |
| `ai_ops_local_compose_cmd` | removed: the pinned renderer (`ai_ops_compose_renderer_*`) |
| `ai_ops_ws_keepalive_*` | removed |
| (new) | `ai_ops_squid_source_only`, `ai_ops_scripts_dir`, `ai_ops_compose_project_src`, `ai_ops_compose_files`, `ai_ops_compose_env_files`, `ai_ops_recovery_unit_dir` |

Remote state is unchanged: `.ai-ops-state` names depend on file labels and component
names, not on tags, so existing baselines, snapshots and locks are read as they are.
The old `apisix-models__*` and ws-keepalive baselines stay as unused recovery material.

## Variables

Set them in `private-config/inventories/production/group_vars/gateway.yml` (group-wide)
or `host_vars/<host>.yml` (per-host); inventory always wins over role defaults.
`inventories/example/group_vars/gateway.yml` lists every knob. Key ones:

| Variable | Default | Meaning |
|---|---|---|
| `ai_ops_config_dir` | `ops/ansible/private-config/` | private config source dir (gitignored) |
| `ai_ops_deploy_root` | `/root/AI-gateway` | remote deployment root |
| `ai_ops_compose_project` | `ai-gateway` | compose project name |
| `ai_ops_compose_cmd` | auto-detect | compose provider (`docker compose` / `podman compose` / `podman-compose`) |
| `ai_ops_compose_renderer_version` / `_sha256` / `_dir` | `2.40.3`, per-arch SHA-256, `/var/tmp/ai-gateway-ops/compose-2.40.3` | pinned controller renderer for the offline candidate render; must equal the host's Compose version |
| `ai_ops_component_approved_digest` | `""` | exact reviewed plan digest for **every component** deploy/rollback |
| `ai_ops_component_verify_retries` / `ai_ops_component_verify_delay_seconds` | `30` / `2` | bounded component readiness wait |
| `ai_ops_component_command_timeout` | `600` | seconds per async Docker pull/recreate; controller timeout must be at least 1800 seconds |
| `ai_ops_lock_transaction` / `ai_ops_lock_min_age_seconds` | `""` / `900` | exact owned transaction and minimum age for guarded stale-lock recovery; age cannot be below 900 |
| `ai_ops_min_mem_mb` / `ai_ops_min_disk_mb` | `200` / `1024` | capacity gate thresholds |
| `ai_ops_squid_image` | `ai-gateway-squid:6.13-2-deb13u3-r1` | pinned image for offline parse validation |
| `ai_ops_squid_source_only` | `false` | only resynchronize `data/egress-proxy/policy.json` |
| `ai_ops_model_sync_approved_digest` | `""` | exact digest printed by the reviewed model-sync preview; required when the policy changes |
| `ai_ops_model_sync_verify_retries` / `ai_ops_model_sync_verify_delay_seconds` | `30` / `2` | bounded wait for the recreated sidecar's immediate round |
| `ai_ops_model_sync_log_tail` | `200` | maximum current-container log lines parsed for the round summary |
| `ai_ops_model_catalog_verify_retries` / `ai_ops_model_catalog_verify_delay_seconds` | `30` / `2` | bounded wait for the recreated model-catalog readiness |
| `ai_ops_aisix_adopted_direct_models` / `ai_ops_aisix_standalone_retained` | `[]` | AISIX orphan-report approval/exclusion lists |

### Preflight prerequisites

Every playbook runs the `ai_ops` preflight first. It installs nothing and stops with
the missing item:

- Docker CLI with the Compose plugin 2.18+ and Python `requests` for the remote
  interpreter (the `community.docker` modules; the Docker SDK for Python is not needed);
- the Docker API through `community.docker.docker_host_info`;
- the pinned controller renderer: preflight fetches the release asset into
  `ai_ops_compose_renderer_dir` with its SHA-256 (or reuses a file with that checksum,
  refusing any other), then requires both the renderer and the host's
  `docker compose version --short` (the part before `+`) to equal
  `ai_ops_compose_renderer_version`. A host Compose upgrade therefore fails closed until
  the pin and both checksums are bumped. The renderer needs no Docker daemon, so the
  candidate render works from a Podman workstation.

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
and review the private seed. From `ops/ansible/`, this one-time copy refuses to replace
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
the separate `deploy-config.yml -e component=cpa-model-sync` policy-change
preview/readback contract below.

## Drift gate

Component `deploy`/`rollback` use the grouped three-file and runtime CAS described
below. Scoped file operations (`deploy-config.yml` for `aisix`, `cpa-model-sync`,
`model-catalog`) checksum each
managed remote file against `.ai-ops-state/<file>.baseline`; a mismatch stops the
deploy before upload. Baselines advance only after activation and readback, not
during a multi-file upload. A file-only preparation defers its baseline to the
caller's final verification. A reviewed, already-matching single-file adoption
still establishes its baseline after readback without recreating. Only a real upload
replaces the `.predeploy` copy, so a no-op deploy reports no change and keeps the last
rollback point. The grouped
component path leaves historical baseline drift for explicit reconciliation. Squid policy deploys (`-e component=egress-proxy`)
upload one tarball + a `sha256sum` manifest over all generated files; the drift
gate verifies the whole remote tree against the last deployed manifest
(`sha256sum -c`), which keeps the same guarantee at ~2 SSH round trips instead of
per-file checks (177 policy files would cost ~900 connections). If the remote was
edited by hand: review the shown diff/failing files, adopt the change into the
local config, and re-run. Rollback likewise refuses to overwrite a remote file
that changed independently after the deploy.

## Guarded component rollout

All `ai_ops_component_profiles` entries, including keeper and bridge, share one grouped
`deploy.yml`/`rollback.yml` transaction. Generic profiles no longer upload three files
and advance their baselines before pulling or recreating. A rollback requires a
snapshot from this guarded path; legacy unowned `.predeploy` files are not
silently treated as a reviewed transaction.
The scoped standard-cache and response-keeper operations stay retired; they have
no playbook.

The host needs Docker Compose v2 (`podman-compose` does not provide the required
`--no-path-resolution --format json` contract). The controller renders the candidate
with the pinned renderer from preflight, which needs no Docker daemon.

```bash
# From ops/ansible/, with private-config adopted from production, not the public example.
ansible-playbook playbooks/deploy.yml -e component=openai-api-cache-keeper --check --diff
ansible-playbook playbooks/deploy.yml -e component=responses-ws-sse-bridge --check --diff

# After explicit approval of the selected plan and interruption window:
ansible-playbook playbooks/deploy.yml -e component=openai-api-cache-keeper \
  -e ai_ops_component_approved_digest='<reviewed-plan-digest>'

# Explicit rollback also needs a fresh plan and its own approval digest.
ansible-playbook playbooks/rollback.yml -e component=openai-api-cache-keeper --check --diff
ansible-playbook playbooks/rollback.yml -e component=openai-api-cache-keeper \
  -e ai_ops_component_approved_digest='<reviewed-rollback-digest>'
```

The check run reads configuration hashes, effective Compose and live container
identities; it never uploads, pulls or recreates. Secret-bearing Compose/environment output
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
Neither profile can add provider credentials or routing. Every Compose call of the
transaction runs as `env -i PATH=… docker compose …` (`ai_ops_compose_argv`), so
ambient shell exports cannot override `.env`; this is why these calls stay `command`
instead of the `community.docker` modules.

Apply pulls `<immutable-ref>` with `community.docker.docker_image_pull` (`pull: always`),
independent of Compose's `pull_policy: never`; preload-only and rollback branches never pull. Before any
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
aisix-status), or the existing completed-cycle readiness
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
[WS decision](websocket-termination.md) and
[routing checklist](model-routing.md#checklist-before-pushing-or-deploying);
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
  ansible-playbook playbooks/deploy.yml -e component=model-catalog \
  -e ai_ops_component_approved_digest='<reviewed-plan-digest>' \
  2>&1 | tee /var/tmp/ai-gateway-ops-logs/model-catalog-apply.log
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
ansible-playbook playbooks/unlock.yml --check
# Review owner.json and matching transaction-<id>.json first.
ansible-playbook playbooks/unlock.yml -e ai_ops_lock_transaction='<exact-32-hex-transaction>'
```

Apply requires a lock at least 900 seconds old, matching owner/progress receipt,
the original controller host with its recorded PID gone, and no other
Ansible/AnsiballZ/Docker Compose/pull writer locally or remotely (excluding only
this recovery command's own ancestry). Any unknown/mismatch leaves the lock.
Legacy empty/unowned locks require manual investigation, not this recovery playbook.
Recovery records `recovery-required`, releases ownership, and changes **no live
configuration, baseline or container**. The check run shows the owner without
creating state. This also preserves the experiment monitor's empty writer lock: it cannot
be mistaken for a stale Ansible transaction.

Offline lifecycle verification (fake Docker and `community.docker` shims, no
production apply):

```bash
python3 ops/scripts/tests/test-component-deploy.py
python3 ops/scripts/tests/test-component-apply.py --evidence /var/tmp/component-apply-tests
```

## Model-sync policy safety

`deploy-config.yml -e component=cpa-model-sync` requires a running preview-capable sidecar image before the
first managed policy change. First adopt the reviewed live
`services/cpa/cpa-model-sync/config.json` into the ignored private path; when local and remote
bytes match, readback establishes the reviewed baseline without recreation.

For a changed policy, the check run streams local JSON to `cpa-model-sync --preview -`
inside the existing container (`docker_compose_v2_exec`, read-only, so it also runs
in check mode). Preview performs only CPA/source reads and prints
complete per-channel additions/removals plus an approval digest. Apply recomputes
that digest and stops before upload if the image, policy, channel identities,
current model sets, or source inventories changed. After sidecar-only recreation,
Ansible requires a successful immediate round and CPA read-back whose current-set
digest equals the approved desired-set digest. Failure restores the old policy,
recreates the sidecar, and proves recovery against the pre-apply current-set
digest; an unproven recovery is reported as failure, never success.

## Model-catalog config safety

`deploy-config.yml -e component=model-catalog` requires a running validation-capable
model-catalog image (one that provides `validate-config`) before the first
managed config change. First adopt the reviewed live
`services/routing/model-catalog/config.yaml` into the ignored private path
`ops/ansible/private-config/services/routing/model-catalog/config.yaml`; when local and remote
bytes match, readback establishes the reviewed baseline without recreation.

For a changed config, the check run streams local YAML to
`/model-catalog validate-config -` inside the running container — the same
config parser used at startup (YAML, required fields, the shared `metadata` chain
schema, globs, rejected Go-era legacy fields) — and shows the effective file diff.
The TypeScript image prints `{"version":1,"config_digest":"<sha256>"}` on success;
the digest is what the post-recreate check compares. Since the TypeScript rewrite
the config uses the openai-api-pi-extension chain schema (`metadata.chain`,
`metadata.models`, `metadata.include_models`); a Go-era file fails validation with a
migration hint. Apply revalidates,
uploads, and force-recreates only `model-catalog` through
`community.docker.docker_compose_v2` (`recreate: always`, `dependencies: false`,
`build: never`, i.e. `--force-recreate --no-deps --no-build`),
then requires a fresh container on the same image whose mounted `/app/config.yaml`
digest matches the candidate, whose `healthcheck` becomes ready within bounded
retries, and whose namespace owner and adjacent relay container IDs stayed
unchanged. Failure restores the protected pre-deploy YAML, recreates
model-catalog, and proves the restored digest plus readiness; an unproven recovery is
reported as recovery-required, never success.

### The membership script (`catalog.lua`)

The model-catalog profile has a second file, the [membership script](model-catalog.md#membership-script)
`catalog.lua` (private path `ops/ansible/private-config/services/routing/model-catalog/catalog.lua`,
remote `services/routing/model-catalog/catalog.lua`, mounted at `/app/catalog.lua`).

- **Custom rule**: the script is custom when the private file exists, whatever its content. Only
  then does the operation manage the remote file. Without it the remote file (the tracked default)
  is left untouched and `metadata.include_models` stays allowed. Removing the private file does not
  delete the remote one: to return to the default, copy the tracked sample to the private path,
  deploy, then delete it.
- **Combination check**: a custom `catalog.lua` together with `metadata.include_models` in the
  private `config.yaml` fails before any comparison or upload, also under `--check`; the message
  names both files. Move the hides into the script (`return false, "reason"`). The gateway itself
  does not reject the combination.
- **One operation**: each file keeps its own drift gate and baseline
  (`.ai-ops-state/model-catalog__catalog.lua.baseline|.predeploy`). A changed script is validated
  with `/model-catalog validate-config /app/config.yaml --script -`; then the changed files upload
  and `model-catalog` is recreated once. Verification runs
  `validate-config /app/config.yaml --script /app/catalog.lua` in the new container and compares
  `config_digest` and `script_digest` and both remote checksums. A failed verification restores
  every file uploaded in the run and recreates once on the restored pair.
- **Rollback** (`rollback-config.yml -e component=model-catalog`) restores the pre-operation copy
  of both files (a deploy saves a copy of an unchanged file too) and recreates once.
- **Coupled changes**: a change that couples the image, the Compose definition and the profile
  (such as the first `catalog.lua` cutover) goes through a reviewed manual window, not this
  operation. Afterwards the operation adopts the deployed files as baselines.
- The verification needs an image with `validate-config --script` (v1.3.0 and later).

## Operational notes

- **AISIX loads `resources.yaml` only at startup.** Because it is a bind mount, a
  file-only change does not alter the container config hash and plain
  `up -d` no-ops. `deploy-config.yml -e component=aisix` therefore activates with
  `recreate: always` (`--force-recreate`).
  Fixture tests miss this (fixture containers are freshly created); caught live
  on rainyun-la 2026-09-19.
- **model-catalog loads `config.yaml` only at startup**, so config deploys
  use the same `--force-recreate` activation; the tracked
  `services/routing/model-catalog/config.yaml` is an image/build default — production desired
  state lives only in the gitignored private config dir.
- **The post-deploy Admin API route check is optional** (`failed_when: false`):
  the internal relay path currently returns 401, so effective-route verification
  is pending a working credential path. Until then, verify via
  `docker logs ai-gateway-aisix-1 | grep 'resources loaded'` and live traffic.

## Modules, check mode and recorded deviations

- **Check mode**: read-only reads and controller scratch (render, parse, tarball, diff,
  previews, lock info) set `check_mode: false` and `changed_when: false`; remote file
  writes use native check mode (would-change plus diff); secret-bearing writes and diffs
  use `diff: false`/`no_log`; every mutation (pull, recreate, restart, lock, PATCH,
  systemd) sits in an apply-only block. Approval digests are computed and printed in
  both modes and required only for apply.
- **`community.docker` modules** replace the Compose/Docker CLI where they keep every
  guarantee: `docker_container_info` (identities, `<project>-<svc>-1`), `docker_image_pull`
  (the approved pull), `docker_compose_v2` (config activations with `dependencies: false`,
  `build: never`, explicit project name/files/env files; the egress-proxy restart) and
  `docker_compose_v2_exec` (read-only execs). Every playbook sets identical
  `module_defaults` from `ai_ops_compose_project_src`, `ai_ops_compose_project`,
  `ai_ops_compose_files` and `ai_ops_compose_env_files`.
- **Kept `command` calls**: the component transaction's `env -i` Compose calls (the
  module cannot clear the process environment), the live and candidate renders, the
  model-sync log read (no logs module), the helper scripts (`component-lock.py`,
  `component-image.py`, the runtime identity probe) and the controller-only Squid
  parse. `test-ansible-operations.py` fails on any other CLI call.
- **Custom drift-gate/baseline/receipt state machine**: native `copy` diffs cannot detect
  remote hand-edits since the last deploy, nor produce the recovery copies.
- **Approval digests** are an addition to check mode: check mode alone cannot bind a
  later apply to the reviewed state.

Secrets live only in gitignored local files (`private-config/`); receipts under
`.ai-ops-state/` are sanitized (mode 0600, secret-bearing tasks use `no_log`).
