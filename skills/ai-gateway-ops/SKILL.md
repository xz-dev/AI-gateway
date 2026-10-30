---
name: ai-gateway-ops
description: Deploy AI-gateway from scratch, update components or configuration, and perform day-2 operations or incident handling on this stack.
---

# AI-gateway operations

Work from the repository root unless a linked procedure says otherwise.
Select one flow below; read its linked procedure before running commands.
Keep deployment-specific configuration private. Tracked defaults are a template,
not desired state for an existing host.

## From-scratch deployment

1. Follow [Installation — Quick start](../../docs/installation.md#quick-start).
   Confirm runtime, Compose, TUN and submodule prerequisites; run
   `./ops/scripts/init.sh` without replacing existing files. Done: private files
   exist with protected permissions and approved credentials/routes.
2. Configure the [Cloudflare Tunnel](../../docs/installation.md#cloudflare-tunnel)
   and [upstreams](../../docs/upstream-configuration.md). Configure Sub2API accounts
   through [sub2api-ops](../sub2api-ops/SKILL.md), including the bridge rules below.
   Done: ingress targets APISIX, not a backend, and account settings read back.
3. Render the approved egress policy and run the installation validation/start
   commands. Done: readiness probes and an authorized inference/terminal-stream
   smoke pass; private administration ports remain unexposed publicly.
4. For subsequent remote management, prepare the controller's ignored private
   inventory/config directory using [Ansible layout](../../docs/ansible-operations.md#layout).
   Adopt the reviewed live configuration, not public examples. Done: a read-only
   component preview reports no unexplained drift.

## Update or change

1. Read the active [OpenSpec requirements](../../openspec/specs/) and any change
   under `openspec/changes/`. Define owned files/services, acceptance and rollback.
   Done: scope and interruption are approved; previous deployment state is known.
2. Classify the change and follow exactly one deployment path:
   - Image or approved environment fields: [guarded component rollout](../../docs/ansible-operations.md#guarded-component-rollout),
     `playbooks/deploy.yml -e component=<name>` from `ops/ansible/`.
     Preview with `--check --diff`; apply only with its reviewed approval digest.
   - Managed configuration: [deploy-config usage](../../docs/ansible-operations.md#usage).
     AISIX manages `resources.yaml`, not `config.yaml`; Squid renders/parses policy;
     [model-sync](../../docs/ansible-operations.md#model-sync-policy-safety) requires
     its preview digest; [model-catalog](../../docs/ansible-operations.md#model-catalog-config-safety)
     validates config and any private membership script before activation.
   - Port, network, service, healthcheck or other Compose structure: use a reviewed
     manual window. The image/environment component gate refuses this by design.
     Follow the [scoped binding window](../../docs/records/tailnet-front-door-binding-2026-10-30.md#preview-and-narrow-diff-evidence)
     and the manual-window rules below; preserve the refusal as evidence.
   - User-specific bindings/services: edit private `compose.override.yaml` using
     [the private-config layout](../../docs/ansible-operations.md#layout), not a
     hand-started container. Classify its effective change using the paths above.
   Done: the preview/render shows only the approved effective change.
3. Validate locally, then activate only the selected services. Follow
   [coupled namespace operations](../../docs/operations.md#operations) when an
   owner changes. Done: image/config/namespace readback and unrelated-container
   identity checks pass; final previews have no unexplained drift.
4. Record UTC timing, observed interruption, checks, deviations, rollback and
   key cleanup under [docs/records/](../../docs/records/); mark unmeasured timing
   explicitly. Tick only fully verified tasks. Run `openspec validate --all --strict`
   and archive an OpenSpec change only after deployment, review and green CI.

## Day-2 operations and incidents

1. Distinguish [liveness, readiness and request success](../../docs/gateway-health-and-recovery.md).
   Front APISIX local `/healthz` expects 404; inner apisix-models expects 200.
   Done: identify the failing hop using bounded logs/probes, not just timer success.
2. Use [bounded recovery](../../docs/gateway-health-and-recovery.md#application-recovery)
   or [controller/lock recovery](../../docs/ansible-operations.md#controller-deadline-logs-and-stale-lock-recovery).
   Inspect receipts and live identities after an interrupted apply before retrying.
   Done: recovery is verified or the unresolved state is reported without guessing.
3. Follow [logs and persistent state](../../docs/operations.md#operations).
   AISIX [config drift reporting](../../docs/ansible-operations.md#usage) is read-only:
   `config_diff` does not authorize reconciling unmanaged `config.yaml`.
   For disk pressure, [inventory first](../../docs/records/prod-disk-cleanup-2026-10-29.md#safety-and-verification);
   preserve databases, named volumes and rollback images/backups. Done: findings
   and any separately approved cleanup have before/after evidence.
4. Route Sub2API business changes to [sub2api-ops](../sub2api-ops/SKILL.md).
   Follow [worker-owned CPA configuration](../../docs/ansible-operations.md#worker-owned-cpa-configuration)
   and [CPA maintenance safety](../../docs/records/cpa-plugins-2026-10-27.md#backups-and-safe-window).
   Done: owned fields read back; credentials and unrelated business state remain intact.

## Hard rules

- Preview first with `--check --diff`; serialize production windows with one writer.
  Use existing approvals only within their scope; stop on authentication failure.
- Bound SSH/remote commands with `timeout`; use `/var/tmp` for private scratch.
  Allow at least 30 minutes for a component controller, per its documented bounds.
- Before a manual window, back up to mode-0600 files inside a mode-0700 directory
  under `.ai-ops-state/`. Keep a protected controller copy too.
- Render old/new Compose independently with the deployment's `env -i` argv.
  Require only the approved delta; preserve image IDs and unrelated private settings.
- Rewrite live bind-mounted files in place (`cat >`, not `sed -i` or `mv`).
  Check SHA-256 and reconcile controller/live copies before closing the window.
- Sample established connections three times, ten seconds apart; choose low traffic.
  Recreate only the selected application, or a changed owner and all its sharers,
  with `--force-recreate --no-deps --no-build --pull never`.
- Judge behavior against approved acceptance, not arbitrary byte identity. Investigate
  inventory/metadata churn; record any explicit gate reinterpretation. Roll back for
  unhealthy services, change-caused inference failure, isolation breach, OOM,
  data/credential loss or unexplained membership loss. Do not waive broken contracts.
- Use realistic bounded production probes, never destructive synthetic large bodies.
- Prove temporary-key cleanup: DELETE 200, GET 404, then local secret files absent.
- Before CPA config/plugin work, take an online SQLite backup and verify integrity.
  Keep plugin version keys untouched; use a stopped-CPA window, never hot reload.
  Rewrite CPA config in place; preserve its worker-owned fields.
- Keep every OpenAI API-key account's
  `extra.openai_apikey_responses_websockets_v2_mode` at `off` while the front
  keeper/WS→HTTP bridge is live; neither Sub2API `http_bridge` nor
  `passthrough`/`ctx_pool` replaces that bridge.
  Disable the separate `extra.openai_passthrough` on keeper-path accounts so native
  error codes survive. Read back both settings using [sub2api-ops](../sub2api-ops/SKILL.md#front-bridge-account-invariants).
- For tracked-template validation, temporarily move root `compose.override.yaml`
  to protected `/var/tmp` scratch; run `./ops/scripts/validate.sh .env.example` in
  the foreground in one tool call. Always restore the override, including on failure.
- Keep private addresses, hostnames, keys and config out of tracked/upstream artifacts.
  Commit unsigned with explicit pathspecs, not `git add -A`; pull with `--rebase`
  before pushing. Keep secrets outside Git, including during conflict resolution.
