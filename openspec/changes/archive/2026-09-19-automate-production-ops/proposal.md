## Why

Routine production changes repeatedly require rediscovering image selectors, private overrides, Squid ACLs, AISIX model references, restart dependencies, and recovery steps. A private desired configuration plus reusable operations can make those changes repeatable without overwriting application-managed state or depending on an AI session to reconstruct the procedure each time.

## What Changes

- Add Ansible operations for read-only drift planning, explicitly approved application, verification, and scoped rollback. Deployment is push-based: configuration is edited **locally** (by a human or an agent/script), reviewed locally, then one Ansible deploy command uploads the local files to the remote deployment root and restarts or triggers config reload. Ansible does not construct configuration on the remote side.
- Establish an operator-owned local private configuration as the source of truth for the adopted scope: image selectors, Compose configuration, Squid policy, AISIX resources. Production customizations are adopted into the local files once; afterwards the local files carry them and the deploy command pushes the complete reviewed files.
- Before upload, verify the remote still matches the last-deployed baseline (drift check). Remote-side drift in the deployed files stops the deploy rather than being silently overwritten; secrets, OAuth credentials, databases, dynamically synchronized CPA inventories, and panel/API-managed settings stay outside deployment files.
- Keep secrets, OAuth credentials, databases, dynamically synchronized CPA inventories, and panel/API-managed settings outside routine reconciliation. Preserve unowned state and reject unexplained drift in selected fields or deployment preconditions.
- Update explicit component versions, prepare exact artifacts and rollback references, and recreate only the reviewed application or coupled service set. Require fresh capacity, backup, and interruption checks.
- Automate the project's firewall: **Squid outbound policy only**. Render and validate selected ACL entry/field changes while preserving other rules and custom settings even within the same service. Regenerate only explicitly disposable derived output; preserve operator-maintained Squid configuration or stop if the activation path cannot accommodate it. Verify allowed and denied boundaries without rotating the existing CA.
- Plan AISIX target/order/strategy/retry changes together with reference-aware cleanup. Report orphaned managed direct models, preserve shared or intentionally standalone targets, and delete only the explicitly approved set. Do not cascade deletions into CPA or Sub2API.
- Interpret retry and fallback fields against the selected service version. A literal `retries: -1` and `max_fallbacks = target_count - 1` are different requests; neither is inferred from ambiguous wording.
- Add repository `skills/` guidance, starting with Sub2API's supported management API, for AI-assisted read, approved minimal-field update, and read-back verification. Ansible does not duplicate panel business operations.
- Produce a sanitized deployment receipt and protected recovery material. A second run with the same effective configuration makes no unnecessary changes.

## Capabilities

### New Capabilities

- `production-operations-control`: Private desired-state adoption, drift planning, guarded Compose deployment, verification, and scoped recovery.
- `production-firewall-policy`: Service-scoped Squid ACL deployment and recovery; no host or cloud firewall management.
- `aisix-route-lifecycle`: Exact model identities, routing-policy updates, shared-target preservation, and approved orphan cleanup.
- `operator-api-skills`: Version-aware, secret-safe management API skills, beginning with Sub2API.

### Modified Capabilities

- `default-egress-policy`: Permit explicit operator-reviewed deployment from adopted private desired policy while preserving fresh-install defaults and prohibiting automatic merges on repository updates.
- `safe-aisix-cutover`: Add the adopted operations workflow for routine AISIX changes without reopening retired routers, changing account placement, or weakening management isolation.

## Impact

Later implementation would add `ansible/` and `skills/`, private-environment examples, focused checks, and operator documentation. Full existing validation belongs in an isolated build/test environment; production planning must not invoke build/test-container steps from `scripts/validate.sh`. Squid rendering remains owned by `scripts/render-egress-policy.py`.

The inspected target is `rainyun-la:/root/AI-gateway`, Compose project `ai-gateway`; these are private inventory inputs, not reusable defaults. Host nftables/iptables/UFW, Docker-generated firewall rules, Tailscale policy, cloud security groups, Matrix operations, automatic latest-version upgrades, and Headroom implementation are out of scope. This proposal authorizes no implementation, private-state adoption, secret migration, production mutation, inference spending, commit, or removal of recovery evidence.
