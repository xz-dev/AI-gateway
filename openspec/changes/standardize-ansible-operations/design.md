## Context

See proposal.md for the motivation. Current state:
- **Entry point**: `ops/ansible/ops.yml` is one play with ~100 tasks. A task runs only when `--tags` names its operation.
  - Preflight blocks guard tag combinations, for example "one deploy or rollback operation" and a blocklist of retired tags.
  - Operations are reached through `include_role: ai_ops, tasks_from: …, apply: {tags: …}`.
- **Role**: one role, `ai_ops`, with 18 task files and one `defaults/main.yml`.
- **Modules**: only `ansible.builtin` modules are used. Compose and Docker run through `command`/`shell` (`ps -q`, `inspect`, `pull`, `exec`, `logs`, `up`, `restart`, `config`).
- **Compose environment**: the component transaction runs every Compose call as `env -i PATH=… docker compose …`, so no variable from the remote login environment can override an `.env` value during interpolation. The configuration operations use the plain `_compose` prefix without `env -i`.
- **Candidate render**: the drift gate and scope guard compare a render of the candidate files made on the controller (`ai_ops_local_compose_cmd … config --no-path-resolution --format json`) with a render of the live files made on the host. In the last production run, the controller's `docker` was a Podman shim that cannot render, and a hand-fetched Compose v5.6.1 serialized the same input differently from the host's v2.40.3, which tripped the scope guard.
- **Read-only preview**: `ai_ops_mode: plan|apply` (default `apply`). It is checked in ~97 places, usually as `when: ai_ops_mode != "plan"`.
- **Approval digests**: approval-bound operations print a digest in plan mode, and apply refuses to run without it. These are the component transaction (`ai_ops_front_approved_digest`), model-sync and ws-keepalive.
- **Activation**: activations run inside `block`/`rescue` with verification. A failed verification restores the pre-deploy copy in the same play.
- **Remote state keys**: they are file labels and component names, never tags:
  - `.ai-ops-state/<label with / → __>.baseline|.predeploy`, for example `model-catalog__config.yaml.baseline`;
  - `front-<component>-last.json` and its snapshots;
  - `model-sync-transition.json`;
  - receipts.

  The component lock owner records `operation: deploy|rollback` and `service`.
- **Script paths**: tasks find host scripts as `{{ playbook_dir }}/../scripts/...`. The controller staging directory is `~/.cache/ai-gateway-ops/squid-staging`.
- **Tests**: `ops/scripts/tests/` run real `ansible-playbook` against a local fixture inventory and a fake `docker` that logs mutations. `test-ansible-aisix-mode.py` parses `ops.yml` as YAML.
- **CI**: no Ansible job.

## Goals / Non-Goals

**Goals:**
- Standard layout, per-operation playbooks, per-concern roles with argument specs, native check mode, and lint at the `production` profile with zero violations.
- The behavior and remote state are unchanged. A check run against today's production reports no change.

**Non-Goals:**
- No change to the drift-gate, baseline and receipt state machine, or to the component transaction's steps.
- No production apply. No host file moves. No package install on the host.
- No git history rewrite. The old production address stays in history (user decision): rewriting needs a force push to `main` and invalidates the commit hashes cited in records. Removing it from the tree stops further exposure.

## Decisions

### D1 Layout

```
ops/ansible/
├── ansible.cfg              inventory = private-config/inventories/production/hosts.yml
├── .ansible-lint            profile: production
├── requirements.txt         ansible-core, ansible-lint (pinned, pip)
├── requirements.yml         community.docker and its dependency (pinned, ansible-galaxy)
├── collections/             (gitignored) installed from requirements.yml
├── README.md
├── inventories/example/     hosts.yml, group_vars/gateway.yml (documented knobs), host_vars/example.yml
├── playbooks/               deploy.yml rollback.yml deploy-config.yml rollback-config.yml recovery.yml unlock.yml
├── roles/
│   ├── ai_ops/                  shared: preflight, capacity gate, Compose argv, script paths
│   ├── ai_ops_managed_file/     prepare / deploy / rollback one managed file (drift gate, predeploy, baseline)
│   ├── ai_ops_component/        component transaction: deploy / rollback (was front-*, component-*)
│   ├── ai_ops_lock/             stale lock recovery (was recover-stale-lock)
│   ├── ai_ops_recovery/         host recovery guard + handlers (was deploy-recovery)
│   ├── ai_ops_squid/            egress policy render, parse, tree deploy, source-only sync
│   ├── ai_ops_aisix/            resources normalize, validate, deploy
│   ├── ai_ops_model_sync/       policy preview, deploy, verify, rollback
│   └── ai_ops_model_catalog/    config validate, deploy, verify, rollback
└── private-config/          (gitignored) inventories/production/, .env, compose*.yaml, services/…, data/…
```

- Every role has `meta/main.yml` and `meta/argument_specs.yml`, with one entry point per `tasks_from` file that a playbook calls. Each role has `defaults/main.yml` only when it has tunables.
- `examples/vars.yml` is folded into `inventories/example/group_vars/gateway.yml`. The old `inventory/` is deleted.
- The shared role keeps the name `ai_ops`, and the concern roles are named after the existing variable prefixes (`ai_ops_model_sync_*`, `ai_ops_model_catalog_*`, `ai_ops_lock_*`, `ai_ops_squid_*`). Most operator variables therefore keep their names (D4).

### D2 Playbooks and component selection

| Old invocation | New playbook | `component` choices |
|---|---|---|
| `--tags deploy -e service=X` | `deploy.yml` | the 14 component profiles |
| `--tags rollback -e service=X` | `rollback.yml` | the 14 component profiles |
| `--tags deploy-aisix` / `deploy-squid` / `deploy-model-sync` / `deploy-model-catalog-config` | `deploy-config.yml` | `aisix`, `egress-proxy`, `cpa-model-sync`, `model-catalog` |
| `--tags sync-squid-source` | `deploy-config.yml -e component=egress-proxy -e ai_ops_squid_source_only=true` | — |
| `--tags rollback-model-sync` / `rollback-model-catalog-config` | `rollback-config.yml` | `cpa-model-sync`, `model-catalog` |
| `--tags deploy-recovery` | `recovery.yml` | — |
| `--tags recover-stale-lock` | `unlock.yml` | — |
| `deploy-ws-keepalive` | removed (user decision); use `deploy.yml -e component=ai-sse-keepalive-proxy`. Its optional OCI source-revision check is dropped. | — |
| `deploy-response-keeper`, `deploy-responses-path`, `restart-response-keeper` | removed (already retired) | — |

- Each playbook is one play on `hosts: gateway` with `gather_facts: false`.
  - `pre_tasks` validates the component: an `assert` against the role's choice list, with a message that lists them. This runs before any connection.
  - The play then runs the shared `ai_ops` preflight and calls the concern role with `include_role`/`tasks_from`. Argument specs validate there, for example `ai_ops_component_name`.
- `deploy-config.yml` maps each component to its role. The component is a name, not a role, so operators never pass role names.
- **Same `component` value, two playbooks**: `component=aisix` in `deploy.yml` deploys the AISIX container (image, Compose, `.env`). In `deploy-config.yml` it deploys `resources.yaml`. The two playbook names keep these apart.
- **Choice lists**: argument-spec `choices` are static lists. A test asserts that the `ai_ops_component` choices equal the keys of `ai_ops_component_profiles`, so the two cannot drift.
- **Tags**: tags remain only as filters (`preflight`, `validate`, `upload`, `activate`, `verify`). Gates carry `tags: [always]`, so `--tags` or `--skip-tags` cannot skip argument validation, preflight, drift or approval checks.

### D3 Roles from the current task files

| Current | New |
|---|---|
| `ops.yml` vars `_runtime`, `_compose`; `preflight.yml`; capacity gate in `pre_tasks` | `ai_ops` (`tasks/main.yml` = preflight; `tasks/capacity.yml`) |
| `prepare-file.yml`, `deploy-file.yml`, `rollback-file.yml` | `ai_ops_managed_file` entry points `prepare`, `deploy`, `rollback` |
| `front-component.yml`, `front-runtime.yml`, `front-state.yml`, `component-image.yml`, `component-progress.yml` | `ai_ops_component` entry points `deploy`, `rollback` |
| `recover-stale-lock.yml` | `ai_ops_lock` |
| `deploy-recovery.yml` | `ai_ops_recovery` |
| `deploy-squid-tree.yml`, the Squid `pre_tasks` | `ai_ops_squid` |
| the AISIX `pre_tasks` and the deploy section | `ai_ops_aisix` |
| the model-sync `pre_tasks`, the deploy and rollback sections, `verify-model-sync.yml` | `ai_ops_model_sync` entry points `deploy`, `rollback` |
| the model-catalog `pre_tasks`, the deploy and rollback sections, `verify-model-catalog-config.yml` | `ai_ops_model_catalog` entry points `deploy`, `rollback` |
| `verify-ws-keepalive.yml` and its section, `verify-response-keeper.yml`, `verify-responses-path.yml` and their sections | deleted |

The shared Compose arguments are defined once in `ai_ops`: `ai_ops_compose_argv` (the `env -i` argv list for the kept `command` calls) and the module arguments for `community.docker` (D12). All roles use them; the old `_compose` string goes.

### D4 Variable naming

- **Scheme**:
  - operator-facing variables use `<role>_<name>`;
  - internal registers and facts use `__<role>_<name>`, which ansible-lint's `var-naming[no-role-prefix]` accepts;
  - playbook-level extra vars are `component` and, for convenience, the operator variables themselves.
- **Unchanged**:
  - `ai_ops_config_dir`, `ai_ops_deploy_root`, `ai_ops_compose_project`, `ai_ops_compose_cmd`, `ai_ops_runtime`;
  - `ai_ops_min_mem_mb`, `ai_ops_min_disk_mb`;
  - `ai_ops_lock_*`, `ai_ops_squid_image`, `ai_ops_model_sync_*`, `ai_ops_model_catalog_*`.
- **Renamed or removed**:

  | Old | New |
  |---|---|
  | `service` (extra var) | `component` |
  | `ai_ops_mode` | removed: `--check` |
  | `ai_ops_profiles` | `ai_ops_component_profiles` |
  | `ai_ops_front_approved_digest` | `ai_ops_component_approved_digest` |
  | `ai_ops_front_verify_retries`, `ai_ops_front_verify_delay_seconds` | `ai_ops_component_verify_retries`, `ai_ops_component_verify_delay_seconds` |
  | `ai_ops_component_command_timeout` | unchanged (now in `ai_ops_component`) |
  | `ai_ops_adopted_direct_models`, `ai_ops_standalone_retained` | `ai_ops_aisix_adopted_direct_models`, `ai_ops_aisix_standalone_retained` |
  | `ai_ops_ws_keepalive_*` | removed |
  | (new) | `ai_ops_squid_source_only` (bool, default `false`) |
  | (new) | `ai_ops_scripts_dir` (default `<repo>/ops/scripts`), replaces `playbook_dir + '/../scripts'` |
  | `ai_ops_local_compose_cmd` | removed: `ai_ops_compose_renderer_version`, `ai_ops_compose_renderer_sha256`, `ai_ops_compose_renderer_dir` (D13) |
  | (new) | `ai_ops_compose_project_src`, `ai_ops_compose_files`, `ai_ops_compose_env_files`: the module form of the Compose prefix (D12) |
  | `ai_activate_cmd` (shell string) | `ai_ops_managed_file_activate`: `{service, state: present\|restarted, recreate}` run with `docker_compose_v2` (D12) |
  | `ai_file_*`, `ai_verify_*`, `ai_recovery_*`, `ai_failure_receipt_*` (task params) | `ai_ops_managed_file_*` |
  | `_capacity`, `_front_*`, `_me_*`, `_wsk_*`, `_ai_*`, `ai_model_sync_verified_*`, … | `__ai_ops_<role>_*` |

- The tracked production `host_vars` set only unchanged variables, so the private inventory copies them verbatim.
- The README documents the rename table.

### D5 Check-mode strategy

`ai_ops_mode == 'plan'` maps one-to-one onto `ansible_check_mode`.

| Task kind | Rule |
|---|---|
| Read-only remote or controller reads (`stat`, `slurp`, `docker inspect`, `compose config`, `validate-config`, the model-sync preview, `diff`, lock `info`) | `check_mode: false`, `changed_when: false` |
| Controller scratch (Squid render, CA, parse, tarball, temp dirs) | `check_mode: false`, `changed_when: false`; it is not managed state. This removes today's Squid `changed=2`. |
| Remote file writes (`copy`, `file`, units) | native check behavior: reports would-change and shows a diff |
| Secret-bearing writes and diffs (`.env`, compose files with secrets, CPA YAML, AISIX caller keys) | `diff: false` plus `no_log: true` where the current task already has it |
| Mutating commands and modules (`docker_image_pull`, `docker_compose_v2` up/restart, the kept `env -i` recreate, lock acquire/release, PATCH, `systemd_service`) | grouped into apply blocks with `when: not ansible_check_mode`, as the `ai_ops_mode != "plan"` blocks are today; check mode skips them |
| Approval gates | the digest is computed and printed in both modes; the `assert` runs only `when: not ansible_check_mode` |

- **Who must use `--check`**: operations that today assert an explicit `ai_ops_mode` are the component transaction, `recover-stale-lock` and `deploy-recovery`. Their safety still comes from what already binds them: the approval digest, the exact `ai_ops_lock_transaction`, and the unit diff.
- **What a plain run does**: like today's default `ai_ops_mode: apply`, running without `--check` applies.

### D6 Handlers

Activations stay inside their `block`/`rescue`. Handlers run at the end of the play, outside `rescue`, so moving activation into a handler would lose the restore-on-failed-verification guarantee. That alternative is rejected.

Handlers are used only where a restart is a pure event with no coupled verification. `ai_ops_recovery` gets `systemd daemon-reload` and a timer restart, notified by a unit file change, followed by `meta: flush_handlers` before verification.

### D7 Inventory and private configuration

- **Private inventory**: `private-config/inventories/production/` holds `hosts.yml` (group `gateway`, the host with `ansible_host` and `ansible_user`) and `host_vars/<host>.yml`, copied from the tracked files.
  - `ansible.cfg` sets `inventory` to it and `roles_path = roles`.
  - `.gitignore` already ignores `ops/ansible/private-config/`.
- **Without the private inventory**: Ansible warns, no host matches `gateway`, and nothing runs. Tests always pass `-i`.
- **Example inventory**: `inventories/example/` uses `gateway.example` / `192.0.2.10` (TEST-NET-1) and documents every operator variable.
- **Leak guard**: a `validate.sh` check fails if a tracked file under `ops/ansible/` contains a `100.64.0.0/10` address or an `ansible_host` outside `inventories/example/`.

### D8 Remote state compatibility

- Managed-file labels stay byte-identical: `compose.yaml`, `compose.override.yaml`, `.env`, `aisix/resources.yaml`, `cpa-model-sync/config.json`, `model-catalog/config.yaml`. Component profile names and the lock owner fields (`operation`, `service`) also stay. So existing baselines, snapshots and locks are read as they are.
- Retiring `deploy-ws-keepalive` and the response-keeper operations leaves their old `compose.yaml`, `.env` and `apisix-models__*` baseline files unused. They are not deleted; they are recovery material.
- A test asserts the label set and the `front-<component>` naming.

### D9 Lint and toolchain

- `.ansible-lint`: `profile: production`, `exclude_paths: [private-config/]`, no `skip_list`.
  - Exceptions are inline `# noqa: <rule>` with a reason, for example `command-instead-of-shell` where a pipeline is required.
  - Each shell pipeline gets `set -o pipefail` or becomes argv.
- `requirements.txt` pins `ansible-core` (2.21.x, the version used today) and `ansible-lint`.
- `requirements.yml` pins `community.docker` 5.4.0 (current release; needs ansible-core >= 2.17) and its dependency `community.library_inventory_filtering_v1` 1.1.5. They install with `ansible-galaxy collection install -r requirements.yml -p collections`. `ansible.cfg` sets `collections_path = ./collections`, so a different user-level copy (this workstation has 5.2.2 in `~/.ansible`) is never picked up. A test asserts the installed version equals the pin.
- **CI** (`validate.yml`) gets an `ansible` job: install the pinned requirements and collections, run `ansible-lint`, run `ansible-playbook --syntax-check` on each playbook against the example inventory, run the `test-ansible-*` / component tests, and run the Compose parity test (D11) on the runner's Docker Engine with the Compose plugin pinned to the renderer version.

### D10 Recorded deviations

These deviations from Ansible conventions are kept on purpose:
- **Some Compose and Docker calls stay `command`.** D12 lists each one with its reason: the `env -i` isolated calls of the component transaction, the offline renders, and the helper scripts.
- **The custom drift gate, baseline and receipt state machine stays.** Native `copy` diffs cannot detect remote edits made since the last deploy (the baseline), and they do not produce the recovery copies.
- **Approval digests are an addition to check mode.** Check mode alone cannot bind a later apply to the reviewed state.

### D11 Test migration

- **Invocations**: the harnesses (`test-component-apply.py`, `test-component-deploy.py`, `test-ansible-*.sh`) switch from `ops.yml --tags <op> -e service=X -e ai_ops_mode=…` to `playbooks/<op>.yml -e component=X [--check --diff]`. Their assertions stay, including the fake-docker mutation log that shows "no mutation in plan".
- **Module calls: module-level shims.** The fixture suites put a test-only `community.docker` collection first in `ANSIBLE_COLLECTIONS_PATH`. Its modules have the real names and argument specs, log every call (module, arguments, check mode) to the same mutation log as the fake `docker`, and return canned container, image and event results from the fixture state.
  - Why shims and not a real daemon: the API modules talk HTTP to a Docker socket, so the existing fake `docker` CLI cannot serve them. A real rootless daemon in every fixture would make the 15+ lifecycle cases slow and would need a working Docker Engine with the Compose plugin; the workstation has only Podman. The shims keep today's seam ("assert what would be called") and run anywhere.
  - A test asserts that each shim's argument spec equals the pinned module's spec, so a shim cannot accept arguments the real module rejects.
- **Compose parity test** (`test-compose-module-parity.sh`): proves the real modules against a real Docker Engine, which the shims cannot. It brings up a three-service project (target, dependency, neighbour) and checks:
  - `docker_compose_v2` with the D12 arguments recreates only the target (`--no-deps`), never pulls or builds, and leaves the neighbour's container ID and start time unchanged;
  - in check mode it changes nothing;
  - `project_name` is the configured one, not the directory basename;
  - the module's `up` argv equals the old `command` argv up to flag spelling (`--detach`, `--project-name`), plus the added `--no-color --quiet-pull --project-directory` and the `--` separator.

  It runs in the CI `ansible` job (real Docker, Compose plugin pinned to the renderer version) and locally wherever a Docker Engine socket exists; the Podman workstation has none, so locally it reports "no Docker Engine" and CI is the gate. It is not a fixture dependency.
- **YAML-parsing tests**: `test-ansible-aisix-mode.py` and the `ops.yml` text checks in `test-component-deploy.py` and `test-standard-cache-topology.py` read the role task files instead.
- **Removed tests**: `test-ansible-ws-keepalive.sh` goes. Its no-op assertion is covered by a component-apply case for `ai-sse-keepalive-proxy`.
- **New tests**:
  - argument validation rejects unknown or missing components before any connection;
  - check mode on an unchanged fixture reports `changed=0` for every playbook, including Squid;
  - a canary secret in a fixture `.env` never appears in `--check --diff` output;
  - the component choices equal the profile keys;
  - the state label set is unchanged;
  - the tracked-inventory leak guard works.

### D12 community.docker modules

Principle: use a module wherever it keeps every current guarantee; keep `command` where it cannot, with the reason. The pinned version is 5.4.0.

| Current call | New | Reason |
|---|---|---|
| `_compose ps -q <svc>` and `docker inspect --format .Image <id>` (config operations, verifiers) | `community.docker.docker_container_info` on `<project>-<svc>-1` (the naming the code already asserts), `no_log: true` because the result holds `Env` | read-only info module |
| `docker image inspect` for an exact pin (verifiers) | `community.docker.docker_image_info` | read-only info module |
| `docker pull <pinned ref>` in the component apply block | `community.docker.docker_image_pull` with `pull: always` (today's behavior) | it stays in the apply block |
| `_compose up -d --force-recreate --no-deps --no-build <svc>` (AISIX, model-catalog, model-sync activations and their rescue restores) | `community.docker.docker_compose_v2`: `services: [svc]`, `recreate: always`, `dependencies: false`, `build: never`, `pull: policy` (no `--pull` flag, as today), explicit `project_name`, `project_src`, `files`, `env_files` | identical argv plus `--no-color --quiet-pull`; these calls run without `env -i` today, so parity holds |
| `_compose restart egress-proxy` | `docker_compose_v2` `state: restarted`, `services: [egress-proxy]` | same Compose command |
| `_compose exec -T <svc> …` read-only (`validate-config` on stdin, `healthcheck`, env reads, AISIX admin read) | `community.docker.docker_compose_v2_exec` with `argv`, `stdin`, `check_mode: false`, `changed_when: false`, `no_log` as today | module has no check-mode support, so read-only execs opt out explicitly |
| `_compose logs … \| filter` (model-sync verify) | **kept** `command` argv, filtering moved to Jinja | no logs module; removes the shell pipe, so no `noqa` |
| component transaction: `env -i PATH=… docker compose … config --quiet`, `… up -d --force-recreate --no-deps --no-build --pull never <svc>` | **kept** `command` argv (`ai_ops_compose_argv`) | the module runs Compose with the remote module environment, and Compose lets process variables override `.env` during interpolation; `environment:` can add variables but cannot clear them, so `env -i` isolation would be lost |
| live render `env -i … config --no-path-resolution --format json` (`front-state`) | **kept** `command` argv | no module renders an effective configuration; `env -i` as above |
| candidate render on the controller | **kept** `command` with the pinned renderer (D13) | offline render, no daemon, no module |
| `component-lock.py`, `component-image.py` (`docker manifest inspect` for registry pullability), the runtime identity probe (netns inodes, recovery probes) | **kept** `command` argv running the scripts | no module reads a registry manifest without pulling, or namespace inodes; the scripts are tested on their own |
| `systemctl daemon-reload`, timer restart | `ansible.builtin.systemd_service` handlers (D6) | builtin module |

- No current call inspects networks or volumes, so `docker_network_info` and `docker_volume_info` are not used. `docker_compose_v2_pull` is not used either: the component path pulls one pinned reference with `docker_image_pull`.
- None of the kept `command` calls is flagged by a `production`-profile rule: `docker`, `python3` and `env` are not in `command-instead-of-module`, and each sets `changed_when`. If implementation still meets a rule, the line gets `# noqa: <rule> <reason>`; there is no `skip_list`.
- **Shared Compose arguments**: `ai_ops` defines `ai_ops_compose_argv` (for the kept `command` calls) and `ai_ops_compose_project_src`, `ai_ops_compose_files`, `ai_ops_compose_env_files`, `ai_ops_compose_project` (for the modules). Each playbook sets `module_defaults` for `docker_compose_v2` and `docker_compose_v2_exec` from these variables, and a test asserts that every playbook's block is identical.

**Check mode and diff**

| Module | Check mode | Diff | Use here |
|---|---|---|---|
| `docker_container_info`, `docker_image_info` | runs, never changes | n/a | runs in both modes |
| `docker_compose_v2` | full: runs `up/restart --dry-run` and reports would-change | none | stays in apply blocks (`when: not ansible_check_mode`), as today, so check runs never touch the production daemon, and a recreate is reported by the scope plan instead |
| `docker_compose_v2_exec` | none | none | read-only execs only, with `check_mode: false` |
| `docker_image_pull` | partial: `pull: always` always reports changed; diff shows image ID `unknown` | full | stays in apply blocks |

**Guarantees with modules**

- **Drift gate and approval digest**: unchanged. They use file stats, `slurp`, the two renders and the scope script, all kept as above.
- **Restore on failure**: a module that fails returns `failed`, so the surrounding `block`/`rescue` restores exactly as a failed `command` does today. The rescue restores call the same module with the same arguments.
- **Component locks**: unchanged (`component-lock.py`).
- **Protected neighbours unchanged**: the identity probe still reads every project container before and after activation. `dependencies: false` is required by the argument spec of `ai_ops_managed_file_activate`, and the parity test proves the neighbour is untouched.

**Remote prerequisites** (community.docker 5.4.0)

- `docker_compose_v2*`: Docker CLI with the Compose plugin 2.18.0 or later.
- API modules (`*_info`, `docker_image_pull`): Python `requests` for the remote interpreter, Docker API 1.25 or later. The collection vendors its Docker client, so the Docker SDK for Python is **not** needed; `paramiko` and `pyOpenSSL` are needed only for SSH or TLS daemon transports, which are not used (local Unix socket).
- Production, read-only check on 2026-10-27: Python 3.14.4 with `requests` 2.32.5, Docker 29.1.3 (API 1.52), Compose 2.40.3 (`2.40.3+ds1-0ubuntu1`), no Docker SDK. All present.
- `ai_ops` preflight asserts them read-only on every run (`docker compose version --short`, `python3 -c 'import requests'`, `docker_host_info` API version) and stops with the missing item. It installs nothing; a missing prerequisite is reported to the user before any host package change.

### D13 Pinned controller Compose renderer

Decision: pin the controller renderer to the host's Compose version. Rejected: moving the candidate render to the host.

- **Why not render on the host**: the candidate files (including `.env`) would have to be written to the host before the drift gate approves them. In check mode that is a remote write, which the check-mode requirement forbids; it would also put an unapproved secret file on production.
- **Why the pin is enough**: the drift gate compares two renders, so they need the same serializer, not the same machine. Proof, read-only: the upstream `docker-compose-linux-x86_64` v2.40.3, run on the Podman workstation without a daemon, rendered the production files to the same bytes as the host's distro build `2.40.3+ds1` (SHA-256 prefix `da8cdc4df04bb615` on both, same three input hashes).
- **Mechanism**:
  - `roles/ai_ops/defaults/main.yml` records `ai_ops_compose_renderer_version: "2.40.3"` and `ai_ops_compose_renderer_sha256` per architecture (`x86_64` `dba9d98e1ba5bfe11d88c99b9bd32fc4a0624a30fafe68eea34d61a3e42fd372`, `aarch64` `d26373b19e89160546d15407516cc59f453030d9bc5b43ba7faf16f7b4980137`, from the v2.40.3 release `checksums.txt`). This is the single record; `requirements.txt` is pip-only.
  - Preflight on the controller (`delegate_to: localhost`, `run_once`, `check_mode: false`, `changed_when: false`, because it is controller scratch, not managed state) fetches the release asset into `ai_ops_compose_renderer_dir` (default `/var/tmp/ai-gateway-ops/compose-<version>`) with `get_url` and `checksum: sha256:<pin>`, unless a file with that checksum is already there. Nothing is installed system-wide.
  - Preflight then asserts that `<renderer> version --short` equals the pin, and that the host's `docker compose version --short` (the part before `+`) equals it too. A mismatch stops the run before any render: "host Compose X differs from renderer pin Y; update ai_ops_compose_renderer_version and its checksums".
  - The renderer needs no Docker daemon and no `docker` CLI, so it works with the Podman shim.
- A host Compose upgrade therefore fails closed until the pin is bumped, instead of producing a false scope violation.

## Risks / Trade-offs

- **[Check mode skips a command whose result a later task reads, so the run fails or diverges]** → Every read-only command gets an explicit `check_mode: false`. The fixture suites run every operation in both check and apply mode. Lint `no-changed-when` covers the rest.
- **[`--diff` prints secrets]** → `diff: false` on secret-bearing tasks, plus the canary-secret test.
- **[A large rewrite silently changes behavior]** → Assertions of the existing tests are kept unchanged. The state label test is added. The final step is a production `--check --diff` for every deploy playbook, run on an `openai/*` model, which must report no change.
- **[Operators or agents use the old commands]** → `ops.yml` is deleted, so an old command fails immediately instead of doing something else. The docs, READMEs and the spec migration table cover every old invocation.
- **[`production` lint rules force awkward rewrites of shell blocks]** → Prefer argv `command`. Use `pipefail`. A documented `noqa` is allowed only per line with a reason.
- **[The production host address stays in git history]** → Accepted (user decision): it is a tailnet address that is only reachable inside the tailnet.
- **[A module call differs from the old command]** → Known differences, each pinned by an argument or a test:
  - `dependencies` defaults to `true` and `pull` to `policy`: both are set explicitly, and the activate spec requires `dependencies: false`;
  - `project_name` defaults to the directory basename (`AI-gateway`, not `ai-gateway`): set explicitly from `ai_ops_compose_project`; the parity test checks it;
  - `env_files` defaults to `<project_src>/.env`: set explicitly;
  - `recreate: always` equals `--force-recreate`, `build: never` equals `--no-build`; `scale`, `profiles` and `remove_orphans` are left unset, as the old calls had none;
  - the module adds `--no-color --quiet-pull --project-directory <deploy root>`; the project directory equals today's implicit one (the first `-f` file's directory).
- **[A Compose plugin release changes its output and breaks `docker_compose_v2` event parsing]** (the collection documents this) → The plugin version is pinned in effect by the D13 preflight, and the parity test runs against that version in CI.
- **[Two mechanisms for Compose]** → Accepted: the `env -i` calls cannot use the module (D12). The table names every kept call, and lint plus a grep test keep `docker compose` strings out of any other task.
- **[Shims drift from the real modules]** → The spec-equality test and the real-daemon parity test.

## Migration Plan

1. **Repository**: implement the change (layout, roles, playbooks, collections, renderer pin, tests, lint, docs). All tests, lint with zero violations, syntax-check and the parity test pass. CI is green.
   - The production prerequisites (D12) are re-checked read-only. If anything is missing, stop and ask the user before any host package change.
2. **Local private config**: move the production inventory (`hosts.ini` + `host_vars`) to `private-config/inventories/production/` as YAML. Delete the tracked files. Verify that `ansible-inventory --graph` shows the host.
3. **Production check, read-only**: on an `openai/*` model, from the Podman workstation, with no `docker-compose` on `PATH` and the renderer directory removed first (so preflight must fetch and verify the pinned renderer itself), run `--check --diff` for:
   - `deploy.yml` for every profile;
   - `deploy-config.yml` for its four components;
   - `recovery.yml`.

   Each must report `changed=0 failed=0`. Investigate any change before continuing.
4. **No apply is required.** Rollback is `git revert`. Remote state is untouched, because check mode writes nothing. The module-based activations are proven by the shims and the parity test; their first production use is the next real deploy (see Open Questions).

## Open Questions

- **Q4 Prove the module activation path in production now?** Check mode skips every `docker_compose_v2` call (D12), so step 3 cannot exercise it on the host. Options:
  - default: no production apply in this change; the first real deploy after it is the first production use, with the usual `block`/`rescue` restore;
  - alternative: one deliberate low-impact apply, for example re-activating the model-catalog configuration (recreates only `model-catalog`, a few seconds without the catalog), to prove `docker_compose_v2` and `docker_container_info` against the production daemon before it matters.
