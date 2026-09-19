## Context

`ansible/` (from `automate-production-ops`) is rehearsal-proven but non-standard: shared logic as `include_tasks` snippets, ini `[group:vars]` config, scattered variable defaults, four parallel playbooks with triplicated compose boilerplate, and the private config dir in the operator's `$HOME`. See proposal.md — Why. The constraint that shapes everything: **behavioral parity is mandatory** — drift gate, rollback, receipts, and remote state layout must not change, because the existing fixture proofs and the rainyun-la rehearsal evidence must remain valid.

## Goals / Non-Goals

**Goals:**
- Conventional Ansible layout: roles with `defaults/main.yml`, YAML inventory (`group_vars`/`host_vars`), single entry playbook + tags.
- Private config moves into the project (`ansible/private-config/`, gitignored), overridable via inventory.
- Every operator-tunable variable declared once in role defaults with a comment; `ai_ops_` prefix everywhere.
- Record the deliberate deviations (plan/apply vs `--check`, command/shell compose, custom drift gate) as accepted decisions.

**Non-Goals:**
- No change to deployment behavior, remote paths, state layout, receipt format, or activation commands.
- No adoption of `community.docker` modules or native `--check` (recorded deviations).
- No new managed components, no changes to `scripts/ops/` helpers (they stay as-is; playbooks keep calling them).
- No re-verification of deployment *behavior* beyond a smoke check — the fixtures already proved it.

## Decisions

### D1: Roles for shared logic, not tasks/ snippets

Create `ansible/roles/ai_deploy_file/`, `ansible/roles/ai_rollback_file/`, `ansible/roles/ai_preflight/` (or a single `ai_ops` role with task files if the three are tightly coupled — decide at implementation; the spec only requires *roles*). Each role gets `defaults/main.yml` declaring its tunables. The playbook invokes roles via `include_role`/`roles:` instead of `include_tasks: ../tasks/x.yml`.

*Rationale*: roles are the community unit of reuse and give `defaults/main.yml` — the single declaration point for variables. Task snippets have no defaults mechanism.

*Alternative considered*: keep `tasks/` snippets, add a `vars/` file — rejected: still no per-variable documentation/override convention, still non-standard.

### D2: Configuration in inventory YAML, template in examples/

`ansible/inventory/group_vars/gateway.yml` holds group-level defaults; `ansible/inventory/host_vars/<host>.yml` holds per-host secrets/overrides. `ansible/examples/vars.yml` is the commented template. `inventory.local.ini` is retired; `ansible.cfg` points at `inventory/` (gitignored except `examples/`).

*Rationale*: matches mdad; YAML supports structured values (component profiles become a real dict, not an ini string); `host_vars` is the standard secret boundary.

*Alternative considered*: keep ini — rejected: cannot express lists/dicts, no template convention.

### D3: Private config at `ansible/private-config/`, gitignored, overridable

Default `ai_ops_config_dir` becomes `{{ playbook_dir }}/../private-config` (resolves to `ansible/private-config/`). `.gitignore` gains `ansible/private-config/` (total coverage — nothing under it can be staged). Operators re-adopt production files into the new path on first run (the scp-from-production flow is unchanged, only the destination moves). Inventory override preserves the old behavior for anyone who needs an external path.

*Rationale*: the repo is the single source location; home-directory state is invisible to the project and not covered by its ignore rules.

*Alternative considered*: keep `$HOME` location — rejected by user: "配置文件不可以放在用户家目录下的呀而是项目里然后 gitignore".

### D4: Single entry playbook + tags

One `playbooks/ops.yml` (name flexible at implementation) with tasks grouped under `tags: [deploy]`, `[deploy-squid]`, `[deploy-aisix]`, `[rollback]` — plus `never`/`always` as appropriate so preflight always runs. Compose command prefix becomes one role-default variable or one `group_vars` entry, referenced everywhere. Old playbook files are deleted after parity is demonstrated.

*Rationale*: mdad's `setup.yml --tags` pattern; kills the triplicated boilerplate.

*Alternative considered*: keep four playbooks, extract a shared `vars_files` — rejected: still four entry points to document and keep in sync.

### D5: Variable namespace and defaults audit

Sweep all playbooks/tasks: rename stray variables to `ai_ops_` prefix, move every operator-tunable `| default(...)` and `set_fact` fallback into role defaults. Internal transient facts (registered results, `_ai_*` loop temporaries) stay underscore-prefixed and are *not* declared in defaults — they are not operator-facing.

*Rationale*: one naming rule, one declaration point.

### D6: Record deliberate deviations (no code change)

Add to this design (done, below in "Accepted deviations") and to `ansible/README.md`: why plan/apply exists instead of `--check` (check mode support across command/shell/copy is partial and we need real remote diffs in plan), why compose runs via `command` (remote root shell + drift gate already does change detection; module adds a dependency without changing guarantees), why the drift-gate state machine exists (remote hand-edits are a real incident source; Ansible has no native equivalent).

### D7: Squid policy deploy uses tarball + manifest, not per-file drift checks

Added during implementation after the restructured `deploy-squid` plan proved unusable on the real host: 177 generated policy files × ~5 SSH round trips each ≈ 900 connections (~1 hour), which coincided with sshd refusing connections until the run was killed. The old playbook had the same loop but was never run against production, so the cost was invisible. New mechanism: render locally → deterministic tarball + `sha256sum` manifest over all generated files → one upload → remote extract → drift gate verifies the remote tree against the last deployed manifest (`sha256sum -c`). Same guarantee (any remote hand-edit fails the manifest check), ~2 SSH round trips. Per-file activation-failure recovery is replaced by tree-level semantics (manifest + tree are consistent post-extract; recovery = redeploy previous local policy). Approved by user decision 2026-09-18.

### D8: Capacity gate default lowered to 200MB

`ai_ops_min_mem_mb` default changed 256 → 200 (per user decision 2026-09-18): rainyun-la has 2GB total with 250-330MB available steady-state, so 256MB would permanently block deploys. Per-host override retained for larger machines.

## Risks / Trade-offs

- [Restructure breaks a working, rehearsal-proven tool] → Behavioral parity is a spec requirement; smoke-check each operation (plan + apply no-op) against rainyun-la after restructure, before deleting old playbooks. Remote state layout is untouched so existing baselines/receipts stay valid.
- [Single playbook + tags makes accidental wrong-tag runs possible] → Every operation already requires explicit `-e service=` or equivalent; preflight always runs; `--list-tags` documents the surface. Rollback keeps its own tag so it cannot trigger on a plain deploy run.
- [Private config in-repo raises accidental-commit risk vs home dir] → `.gitignore` covers the entire directory; spec requires total coverage verified by a broad-stage test.
- [Migration friction for the operator's existing `~/.ai-gateway-ops/config/`] → One-time `mv`/re-adopt; inventory override allows keeping the old path if needed.
- [Roles add files/indirection for a small tool] → Accepted: the gain is the defaults mechanism and conventions, not size reduction.

## Migration Plan

1. Create roles + inventory YAML + examples template (new files, old ones still active).
2. Port shared task logic into roles verbatim (no behavior edits), wire single entry playbook with tags.
3. Update `.gitignore` (`ansible/private-config/`, `ansible/inventory/` except examples).
4. Move operator config: `~/.ai-gateway-ops/config/` → `ansible/private-config/`; update inventory to point at it.
5. Smoke check on rainyun-la: plan-mode run of each tag (zero remote writes expected), then one apply no-op deploy (drift gate must report "no effective change" for all files, container IDs unchanged).
6. Delete old playbooks/tasks/inventory.local.ini; rewrite `ansible/README.md`.
7. Rollback strategy: the restructure is pure git history — revert the commit; remote state was never touched by the restructure itself.

## Open Questions

- Single `ai_ops` role with internal task files vs three separate roles (`ai_preflight`/`ai_deploy_file`/`ai_rollback_file`) — decide at implementation by cohesion; either satisfies the spec.
- Whether the entry playbook lives at `ansible/playbooks/ops.yml` or `ansible/ops.yml` — cosmetic, decide at implementation.
