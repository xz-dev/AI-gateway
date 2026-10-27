## Context

See proposal.md for the motivation. Current state:
- **Entry point**: `ops/ansible/ops.yml` is one play with ~100 tasks. A task runs only when `--tags` names its operation.
  - Preflight blocks guard tag combinations, for example "one deploy or rollback operation" and a blocklist of retired tags.
  - Operations are reached through `include_role: ai_ops, tasks_from: …, apply: {tags: …}`.
- **Role**: one role, `ai_ops`, with 18 task files and one `defaults/main.yml`.
- **Modules**: only `ansible.builtin` modules are used. Compose runs through `command`/`shell`.
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
- No Docker Compose module (`community.docker`): Compose stays a `command` call (D10).
- No change to the drift-gate, baseline and receipt state machine, or to the component transaction's steps.
- No production apply. No host file moves.
- No git history rewrite (Open Questions).

## Decisions

### D1 Layout

```
ops/ansible/
├── ansible.cfg              inventory = private-config/inventories/production/hosts.yml
├── .ansible-lint            profile: production
├── requirements.txt         ansible-core, ansible-lint (pinned)
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
| `deploy-ws-keepalive` | removed; use `deploy.yml -e component=ai-sse-keepalive-proxy` | — |
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

The shared Compose argv is built once in `ai_ops`: `ai_ops_compose_argv` (list) and `ai_ops_compose` (string, for the existing `command` strings). All roles use it.

### D4 Variable naming

- **Scheme**:
  - operator-facing variables use `<role>_<name>`;
  - internal registers and facts use `__<role>_<name>`, which ansible-lint's `var-naming[no-role-prefix]` accepts;
  - playbook-level extra vars are `component` and, for convenience, the operator variables themselves.
- **Unchanged**:
  - `ai_ops_config_dir`, `ai_ops_deploy_root`, `ai_ops_compose_project`, `ai_ops_compose_cmd`, `ai_ops_runtime`;
  - `ai_ops_min_mem_mb`, `ai_ops_min_disk_mb`, `ai_ops_local_compose_cmd`;
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
  | `ai_file_*`, `ai_activate_cmd`, `ai_verify_*`, `ai_recovery_*`, `ai_failure_receipt_*` (task params) | `ai_ops_managed_file_*` |
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
| Mutating commands (pull, `up`, `restart`, lock acquire/release, PATCH, `systemctl`) | grouped into apply blocks with `when: not ansible_check_mode`, as the `ai_ops_mode != "plan"` blocks are today; check mode skips them |
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
- There is no `requirements.yml`: only `ansible.builtin` is used, so it would pin nothing.
- **CI** (`validate.yml`) gets an `ansible` job: install the pinned requirements, run `ansible-lint`, run `ansible-playbook --syntax-check` on each playbook against the example inventory, and run the `test-ansible-*` / component tests.

### D10 Recorded deviations

These deviations from Ansible conventions are kept on purpose:
- **Compose stays a `command` call.** `community.docker.docker_compose_v2` cannot express the secret-free `env -i` argv, the exact `--no-deps --no-build --pull never` recreate set, or the offline candidate render. Adding it would also add a collection dependency.
- **The custom drift gate, baseline and receipt state machine stays.** Native `copy` diffs cannot detect remote edits made since the last deploy (the baseline), and they do not produce the recovery copies.
- **Approval digests are an addition to check mode.** Check mode alone cannot bind a later apply to the reviewed state.

### D11 Test migration

- **Invocations**: the harnesses (`test-component-apply.py`, `test-component-deploy.py`, `test-ansible-*.sh`) switch from `ops.yml --tags <op> -e service=X -e ai_ops_mode=…` to `playbooks/<op>.yml -e component=X [--check --diff]`. Their assertions stay, including the fake-docker mutation log that shows "no mutation in plan".
- **YAML-parsing tests**: `test-ansible-aisix-mode.py` and the `ops.yml` text checks in `test-component-deploy.py` and `test-standard-cache-topology.py` read the role task files instead.
- **Removed tests**: `test-ansible-ws-keepalive.sh` goes. Its no-op assertion is covered by a component-apply case for `ai-sse-keepalive-proxy`.
- **New tests**:
  - argument validation rejects unknown or missing components before any connection;
  - check mode on an unchanged fixture reports `changed=0` for every playbook, including Squid;
  - a canary secret in a fixture `.env` never appears in `--check --diff` output;
  - the component choices equal the profile keys;
  - the state label set is unchanged;
  - the tracked-inventory leak guard works.

## Risks / Trade-offs

- **[Check mode skips a command whose result a later task reads, so the run fails or diverges]** → Every read-only command gets an explicit `check_mode: false`. The fixture suites run every operation in both check and apply mode. Lint `no-changed-when` covers the rest.
- **[`--diff` prints secrets]** → `diff: false` on secret-bearing tasks, plus the canary-secret test.
- **[A large rewrite silently changes behavior]** → Assertions of the existing tests are kept unchanged. The state label test is added. The final step is a production `--check --diff` for every deploy playbook, run on an `openai/*` model, which must report no change.
- **[Operators or agents use the old commands]** → `ops.yml` is deleted, so an old command fails immediately instead of doing something else. The docs, READMEs and the spec migration table cover every old invocation.
- **[`production` lint rules force awkward rewrites of shell blocks]** → Prefer argv `command`. Use `pipefail`. A documented `noqa` is allowed only per line with a reason.
- **[The production host address stays in git history]** → It is a tailnet address that is only reachable inside the tailnet (see Open Questions).

## Migration Plan

1. **Repository**: implement the change (layout, roles, playbooks, tests, lint, docs). All tests, lint with zero violations and syntax-check pass. CI is green.
2. **Local private config**: move the production inventory (`hosts.ini` + `host_vars`) to `private-config/inventories/production/` as YAML. Delete the tracked files. Verify that `ansible-inventory --graph` shows the host.
3. **Production check, read-only**: on an `openai/*` model, run `--check --diff` for:
   - `deploy.yml` for every profile;
   - `deploy-config.yml` for its four components;
   - `recovery.yml`.

   Each must report `changed=0 failed=0`. Investigate any change before continuing.
4. **No apply is required.** Rollback is `git revert`. Remote state is untouched, because check mode writes nothing.

## Open Questions

Resolve these before `/opsx-apply`. Each has a default that the specs and tasks already assume.

- **Q1 Retire `deploy-ws-keepalive`?** Default: retire it.
  - It is undocumented. It deploys `compose.yaml` + `.env` and recreates `ai-sse-keepalive-proxy` with a digest gate and an optional OCI revision check.
  - The generic profile `ai-sse-keepalive-proxy` already covers digest-approved deploy and rollback, but it has no OCI-revision check.
  - Alternative: keep it as a `deploy-config.yml` component, which adds one more component and its vars.
- **Q2 Rewrite git history** to drop the production address from `inventory/hosts.ini` (present since 5b61fc7)? Default: no. It needs a force push to `main`, and it invalidates existing clones and the commit hashes cited in records. Removing the address from the tree stops further exposure.
- **Q3 No `requirements.yml`**: only `ansible.builtin` is used. Default: no file, and the toolchain is pinned in `requirements.txt`. If the user wants the file anyway, it would contain only `collections: []`.
