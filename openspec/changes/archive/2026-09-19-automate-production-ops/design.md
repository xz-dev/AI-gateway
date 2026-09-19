## Context

See `proposal.md` for motivation and scope. This is a set of repeatable Ansible operations plus a Sub2API API skill, not a new deployment controller.

The read-only production inspection located the deployment at `rainyun-la:/root/AI-gateway`, Compose project `ai-gateway`, using base and private override files. It found private/local differences, 48 running project containers, and limited free memory with swap in use. These are inspection-time observations, not defaults or capacity guarantees. The existing checkout cannot be replaced wholesale with the local dirty tree.

Relevant existing contracts and tools:

- `README.md` Operations distinguishes proven application-only replacement from coupled namespace/relay recreation and prohibits restarting a shared namespace owner alone.
- `scripts/render-egress-policy.py` consumes the native policy JSON and an output directory. Reuse it; do not create a second ACL language.
- `scripts/validate.sh` builds images and starts test containers as well as checking configuration. It belongs in isolated validation, not a production read-only plan.
- `aisix/config.example.yaml` selects a native resources file, and Compose bind-mounts that file. `scripts/check-aisix-resource-policy.py` validates the fallback-status contract, not complete resource references or deletion behavior.
- `aisix-model-routing` already defines exact identities, strategy, session behavior, bounded fallback, and no replay after output. This change does not replace those semantics.
- `safe-aisix-cutover` preserves internal-only administration and the completed router cutover. A routine route edit is not an account migration.

Only change artifacts are being captured now. Ansible, skills, tests, private adoption, and production rollout require later authorization.

## Goals / Non-Goals

**Goals:**

- Make the next CPA upgrade, one-service Squid edit, or AISIX target removal follow an already documented operation with small explicit inputs.
- Review production adoption once; thereafter the local private files are the deployable source and a checksum drift check guards the remote against concurrent edits.
- Express common approval, drift, backup, verification, and recovery rules once in `production-operations-control`; keep operation-specific checks small.

**Non-Goals:**

- A daemon, workflow service, universal resource graph, new configuration DSL, or cross-application transaction engine.
- Continuous reconciliation of Sub2API business settings, CPA synchronized inventory, databases, or OAuth state.
- Automatic latest-version selection, provider discovery/onboarding, host firewall ownership, secret rotation, or inference probes without approval.
- Guaranteed zero downtime for every upgrade, automatic schema downgrade, or automatic cross-system model deletion.

## Decisions

### 1. Local edited files are the deployable source; Ansible pushes them

Deployment is push-based:

1. Configuration lives in an operator-controlled **local** private directory (outside the public checkout): environment/image selections and Compose overrides, Squid policy JSON, AISIX resources YAML. Production customizations are adopted into these files once; afterwards the local copies carry them.
2. A human or agent edits the local files (editor or small local scripts such as `scripts/ops/config-edit.py`). The diff is reviewed locally before anything is deployed.
3. One Ansible deploy command uploads the reviewed local files to the remote deployment root, then restarts the affected services or triggers the supported reload.

Ansible never constructs configuration on the remote side. Before upload it verifies the remote files still match the last-deployed baseline (checksum drift check); a mismatch stops the deploy so a concurrent remote edit is never silently overwritten. Within the adopted scope the pushed files are owned whole-file; secrets stay in separate untracked files referenced by the config, never in the pushed files. The ownership record names the local files, remote target paths, deployment root/project, and per-component activation profile; no new application schema.

**Alternative rejected:** constructing candidates on the remote from fresh observed state adds a second editing engine and risks stale merges; local-edit-plus-drift-check gives the same protection with the file diff as the review surface.

**Alternative rejected:** using live export as the desired source on every run would silently adopt accidents. Tracking private configuration in the public tree or deploying the dirty checkout would risk secret disclosure and unrelated changes.

### 2. Three bounded operations share one lifecycle

Implement three ordinary entry points under `ansible/`: component deployment, Squid policy deployment, and AISIX route deployment. Use Ansible tasks, assertions, handlers, protected copies, and Compose service selection; share only repeated preflight/receipt handling. A small fixed profile per supported component records its image selector, affected files, proven service set, readiness checks, backup needs, and recovery steps. Unknown topology/version/profile is a stopped plan, not a request for the AI to invent a deployment recipe.

```text
local private config --edit/review--> local diff
          |
          v
   ansible deploy command
          |
          v
  drift check (remote == last-deployed?)
          |
          v
  upload files -> activate (restart/reload) -> verify
                                            /      \
                                        success    failure
                                           |          |
                                   record baseline  upload prior files + reactivate
                                           |          |
                                           +---- sanitized receipt
```

Planning renders the diff (local desired vs remote current) and checks preconditions, but performs no production writes, image pulls, builds, test-container launches, or API mutations. Unavailable validation or runtime observation is recorded as incomplete.

Apply binds approval to the target, the reviewed local file set, the expected remote baseline, the affected service set, interruption, verification traffic, and recovery policy. The remote baseline is rechecked immediately before upload. Recovery uploads the protected pre-deploy copy of the same files and reactivates; a remote change made after this operation started would already have stopped the deploy at the drift gate.

Store a sanitized receipt and protected pre-change material for each operation. The receipt records configuration/artifact identities, the exact changed fields/entries, actual affected services, check results and skips, and recovery outcome. Configuration recovery reverses only those recorded changes against fresh state, with conflict checks, not by copying a stale whole-file backup over later custom edits. A complete backup supplies evidence and prior values, not broader write authority. Secret-bearing tasks use `no_log` and disabled diff; a separately constructed safe summary provides useful review. Secret-bearing fingerprints and snapshots stay private. A small receipt is sufficient; no new event store or approval service is needed.

**Alternative rejected:** a generic multi-system reconciler adds ownership and rollback logic for applications that already have management APIs. Blanket check mode and raw diff output are not a substitute for a deliberately read-only, secret-safe plan.

### 3. Component deployment changes effective versions, not the whole checkout

The operator changes an explicit component image/version selector at its approved source. The plan resolves the effective Compose configuration using the existing environment and all overrides, reports old and new artifact identities, and selects the existing component profile. An image-only update preserves that service's mounts, environment, command, networks, extension fields, and all other custom values. Do not serialize normalized `compose config` output back over source files or replace a service block from a template; normalized output is for effective-diff validation.

For a proven application-only change, prepare the selected image, preserve its prior identity and required backups, and use targeted Compose recreation without dependency recreation or builds. Use the equivalent of `up -d --no-deps --no-build <service>` only where the reviewed profile proves it safe. Verify the actual image and configuration plus explicit readiness and unrelated-container identity invariants. Do not rely only on engine healthchecks.

A namespace-owner, network, or relay change follows the repository's coupled-operation rule with its full reviewed dependent set and order, including private provider-sidecar components where applicable. Do not attempt to derive a universally minimal restart graph. If that set is larger than the approval, stop for a revised plan.

Build locally maintained images and run `scripts/validate.sh` in an isolated environment before deployment. Production apply prepares fixed image artifacts without building; it checks fresh memory/disk/swap and the operation's approved interruption conditions. A routine in-place replacement does not require a duplicate full stack. When candidate coexistence is actually needed, its additional memory is part of the gate. Do not tune the host to force a fit.

Database-affecting upgrades need the component's consistent backup and compatibility procedure. An image downgrade is offered only if compatible with current data; otherwise stop with explicit recovery instructions. This is a profile prerequisite, not a new universal database backup engine.

**Alternative rejected:** unconditional forced full-stack recreation breaks idempotence and increases disruption. Copying the entire repository conflates image deployment with unrelated configuration changes.

### 4. Squid edits local policy JSON, deploys the rendered config, keeps the CA

Edit the local private policy JSON (one service's entries/fields). The deploy renders locally with `scripts/render-egress-policy.py` into a staging directory, validates with the pinned Squid artifact in an isolated test, then uploads the rendered generated directory and activates via the tested reload/restart path of the deployed Squid version. The remote Squid generated-config directory is disposable derived output owned by this operation; any operator-maintained custom directives/includes live in local files and are pushed as part of the same reviewed file set. First-run adoption fails loudly if the remote layout carries customizations not represented locally, instead of overwriting them.

The supported profile records the mount layout and a tested activation procedure: install the validated generated file set without mixing generations, then use supported Squid reconfiguration where verified. If reload cannot activate the changed mounted files safely, present the required bounded recreation and interruption instead. A changed file on disk is not proof the process loaded it; bind-mount replacement behavior must be exercised in the activation test.

After activation, verify the changed allow case and representative deny cases from the relevant service boundary using the traffic already approved in the plan. Use controlled fixtures for comprehensive domain/SNI/Host/method/path coverage; live production checks are narrowly scoped and must not make unapproved provider calls. Preserve the CA and the route restrictions throughout. On failure, upload the protected pre-deploy rendered directory and reactivate, then verify recovery; an unchanged effective policy does nothing.

**Alternative rejected:** merging the tracked default, broadly allowing a domain to make a probe pass, or editing host firewalls does not implement a service-specific Squid change.

### 5. AISIX uses a bounded local reference check, not cross-system garbage collection

Desired routes and direct declarations live in the local AISIX resources YAML, edited locally by the operator or a small local helper script (`scripts/ops/config-edit.py`, explicit scalar/list edits with identity-key matching, refusing phantom removals). Plain editing plus local diff review is the default path; whole-file ownership applies within the adopted scope, and production custom routes/fields are adopted into the local file at baseline adoption. Review all retained AISIX route target references, including unowned routes, before cleanup. For the supported direct-target shape, the candidate set is a simple set difference:

```text
cleanup candidates = adopted direct models
                   - targets referenced by any retained AISIX route
                   - explicitly retained standalone models
```

Complete inventory is required. Duplicate names, unresolved references, or route shapes the checker cannot safely interpret stop cleanup. There is no need to inspect a cross-system graph to propose this AISIX-only deletion set. Usage cannot be inferred from zero route references: the plan explicitly warns about loss of direct-call/catalog exposure and requires approval for each proposed deletion. Unapproved candidates remain declared in both desired and effective configuration.

The local resources file already carries adopted custom routes and fields, so uploading it preserves provider keys, caller keys, unrelated routes, and unselected fields by construction. Reuse the existing fallback-status validator and add only focused identity/reference/deletion checks (`scripts/ops/check-aisix-orphans.py`). A membership edit does not reset cooldown, strategy, weights, or retry fields. A target-count-minus-one fallback rule is applied only when explicitly selected and verified; it is not a global default and never means literal `retries: -1`.

The deploy uploads the edited resources file, then activates via the tested AISIX application-only recreation while its namespace remains intact, with interruption included in approval. An isolated test with the pinned artifact must prove that startup loads the pushed file and that omitted approved declarations disappear from the effective inventory. An internal Admin API read verifies exact effective routes and catalog membership afterward.

Read-only runtime observation uses an existing authorized internal management peer where supported. Planning must not create a helper container or publish a port to get an inventory; if the established path is unavailable, report the missing observation and require separately approved temporary maintenance access. Never reuse the status-page binding as an Admin API proxy. On approved recovery, the deploy uploads the protected pre-deploy copy of the same files and reactivates; because the remote is drift-gated, the pre-deploy copy is authoritative for exactly the files this operation replaced. Sub2API accounts are never migrated and retired routers never revived.

Removing an AISIX declaration does not imply removal from CPA's catalog or the front-door entitlement intersection. Report known external effects and unverified client usage as review notes; do not change CPA models, Sub2API mappings, provider credentials, or Squid rules.

**Alternative rejected:** deleting every unreferenced model breaks legitimate direct use. API-only route edits create drift from the resources file; cross-application cleanup silently expands authority.

### 6. Sub2API skill preserves the panel's ownership

Add a repository skill for occasional account/routing/mapping and pool/retry work through supported management APIs. It records version-matched endpoints, request/response envelopes, exact identity selection, replacement semantics, safe read-back checks, and field-scoped recovery. It is guidance for the existing API, not an API wrapper framework or scheduled reconciler. Ansible can deploy a Sub2API image through its component profile, but does not own its business settings.

For a write: inspect, propose the exact owned fields, obtain approval, read the latest complete resource, verify the baseline, update only those fields, then read back. If a nested credentials object is replaced by PUT, construct it from the latest complete object inside a protected execution context; do not emit it into the conversation. Use API concurrency controls when available; otherwise require a non-overlapping edit window and disclose the residual race. On a lost response, read before retrying. Stop on unknown schema, masked required fields, or failed authentication.

Local operation notes are evidence leads, not authoritative API schemas. The existing Sub2API configuration guide includes historical examples and a direct-SQL emergency option; the new skill must verify the deployed API contract and must not inherit SQL access as an authentication fallback.

**Alternative rejected:** duplicating account CRUD in Ansible creates two owners for panel state; generic partial PUT assumptions can erase credentials or peer settings.

## Acceptance examples

These are future implementation checks, not executed production tests:

| Routine request | Fixed checks and expected outcome |
|---|---|
| Upgrade CPA to an explicit version | Effective selector is the only configuration difference; that service's custom mounts/environment/command/networks and override precedence remain intact; artifact prepared before interruption; approved application-only recreation; actual image/readiness verified; unrelated containers preserved; identical second run causes no recreation |
| Add one service's Squid endpoint | Native renderer and Squid syntax pass; custom ACLs/settings in that same service survive; unsafe custom-directive loss blocks activation; representative allow/deny checks pass; CA unchanged; recovery reverts only this operation's edits; identical second run does not reload |
| Remove one target from an AISIX logical route | Remaining exact names/order/policy and custom fields inside that route and its targets survive; omitted input is not deletion; shared and standalone targets retained; orphan candidates displayed; only approved declarations removed; effective AISIX inventory checked; CPA and Sub2API unchanged |
| Adjust a Sub2API account field through the skill | Supported version contract; exact resource/field approval; latest complete nested object preserved; read-back proves the field change; no deployment or SQL side effect |

Test these with disposable Compose fixtures and API doubles before a separately approved deployment. Include private sibling fields absent from example templates, ambiguous list identity, shadowing overrides, drift-before-apply, stale mounted config, incomplete inventory, failed recovery, and rerun/no-change checks. For each operation, assert that configuration outside the approved field/entry diff is unchanged; repeat the assertion for recovery with a later independent custom edit. No new BDD framework is required.

## Risks / Trade-offs

- **Existing private drift is legitimate in places** -> First adoption requires review; ambiguous resources remain unowned. Routine runs never infer ownership anew.
- **Local overrides can shadow image/config choices** -> Compare effective Compose inputs and runtime identity; stop on conflicts without changing precedence or rewriting a service.
- **Generic serialization or rendering can erase custom configuration** -> Change only selected fields/entries, compare the complete effective diff, and stop on unsafe preservation. Do not normalize production into template shape.
- **Small host headroom limits canaries** -> Use fresh measurements and a reviewed operation profile; do not promise candidate coexistence or use host tuning as a workaround.
- **Mounted files and loaders can retain stale state** -> Test the activation path with the exact artifact and mount shape, then verify effective runtime state; stop on unsupported behavior.
- **No general transaction spans files, containers, and APIs** -> Operate one bounded change at a time, retain protected pre-change material, and distinguish restored, incomplete, and recovery-required outcomes.
- **Direct AISIX callers may not appear in configuration references** -> Require the explicit retention/deletion decision and report the exposure impact; do not call orphan candidates unused.
- **External manual writes can race with automation** -> Single cooperating apply plus a no-overlap edit window, fresh drift checks, and read-back; no claim of universal locking.
- **Historical retry/API notes can be wrong for a new version** -> A version change invalidates unsupported operation assumptions, not the chosen ownership model.

## Migration Plan

This is a future rollout sequence, not authorization to execute it.

1. Implement the three operations, shared checks, component profiles, and Sub2API skill against disposable fixtures. Validate the tracked release and private candidate inputs separately; publish no private test material.
2. Perform a read-only adoption comparison for the actual production root and Compose inputs. Review private overrides, selected Squid ACL entries/fields, selected AISIX route fields/target identities, and standalone retention. Record custom directives/includes and distinguish operator-maintained files from disposable derived output. Save only explicitly approved field/entry ownership and a protected baseline; do not change serving state.
3. Run the routine plans against that adopted baseline. A no-change plan must be empty. Prove prepare/activation/recovery for the selected artifact versions before enabling those profiles for production.
4. With separate production approval, execute one bounded real operation at a time and preserve its receipt and recovery data. Repeat the operation to prove no unnecessary changes; expand supported profiles only through reviewed fixture evidence.
5. On failure, use only the selected operation's approved recovery. On conflicting drift or incompatible data, stop for operator action. Do not reset the repository, prune old images/backups, rotate the CA, or restore retired routers.

## Deferred evidence checks

These checks fill in version-specific profile data during implementation or before an affected operation; they do not justify changing the scope or leaving the ownership architecture undecided:

- The remembered literal `-1` setting still lacks an identified field/version and verified semantics. Do not enable that value until proved compatible with bounded retries; existing verified values remain usable.
- Pin and exercise the selected Ansible/Compose versions, Squid activation procedure, AISIX file-load/removal behavior, and Sub2API API contracts. A failed check disables that operation profile rather than silently selecting a broader method.
- Re-measure capacity and confirm each profile's backup/readiness/traffic conditions for the selected release. Historical inspection values are not deployment thresholds.

## References

- Common behavior: `specs/production-operations-control/spec.md`.
- Operation boundaries: the Squid, AISIX lifecycle, and operator API skill specs in this change; deltas to `default-egress-policy` and `safe-aisix-cutover` preserve existing constraints.
- Ansible check/diff limitations: <https://docs.ansible.com/ansible/latest/playbook_guide/playbooks_checkmode.html>.
- Compose module service selection and idempotence caveats: <https://docs.ansible.com/ansible/latest/collections/community/docker/docker_compose_v2_module.html>.
