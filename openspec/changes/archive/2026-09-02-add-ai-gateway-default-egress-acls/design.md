## Context

See `proposal.md` for motivation and `specs/default-egress-policy/spec.md` for the behavior contract.

The tracked `egress-proxy/policy.example.json` currently contains empty destination lists. `scripts/init-egress-proxy.sh` copies it to `data/egress-proxy/policy.json` only when that runtime file does not exist. The generated CPA configuration enables its control panel, and the pinned CPA and Sub2API implementations start several control-plane refreshers by default.

The design is constrained by the existing egress architecture:

- Squid policies are service-specific and default-deny.
- HTTP method and path filtering requires TLS bump.
- Paths are POSIX regular expressions evaluated after interception.
- Runtime policy under `data/` is operator-owned and intentionally not updated by repository upgrades.
- Provider, account, plugin, and private-sidecar destinations vary by deployment and can carry credentials.

The source baseline used for endpoint classification is the digest-pinned production-compatible code inspected during exploration:

- CPA source revision `0f10a3635c39984d9eae139a4d7b3759f51ec6d0`.
- Sub2API source revision `2ac784c51a5d0925b324efef2ba6b3446c364781`.
- CPA Management Center repository ID `1051566067`, matching the GitHub repository API and the redirect prefix observed for releases v1.22.8 through v1.22.12.

## Goals / Non-Goals

**Goals:**

- Make a fresh installation's fixed built-in control-plane behavior work without broadening provider access.
- Keep every default allowance attributable to a fixed source code path and a stable, exact upstream destination.
- Represent multi-hop downloads as a complete set of least-privilege rules rather than fixing one redirect reactively.
- Make the tracked default policy an offline-validated security contract.
- Explain which denied requests are intentional and how operators extend the policy safely.

**Non-Goals:**

- Automatically merge new rules into an existing runtime policy.
- Change or deploy the current production policy.
- Make CPA's dynamic plugin store work under a repository wildcard.
- Enable provider inference, OAuth, account quota, backup, payment, webhook, or private-sidecar traffic.
- Enable Sub2API in-container binary update or rollback downloads.
- Enable unverified CPA fallback pages or speculative alternate GitHub CDN domains.
- Add network-dependent CI tests.

## Decisions

### 1. Define the baseline by fixed behavior, not by observed traffic alone

A destination enters the tracked default only when all of these conditions hold:

1. The generated application configuration starts the request automatically, or an enabled built-in management surface uses it for read-only release awareness.
2. The pinned source fixes the domain, repository, method, and path rather than accepting operator input or remote registry output.
3. The request uses `GET` and does not require provider or user credentials.
4. The response is control-plane metadata or a fixed Management Center asset, not a dynamically selected executable integration.
5. A precise policy can express the request without an owner, repository, path, or repository-ID wildcard.

Production requests confirm real use and reveal redirect hops, but production traffic does not itself authorize a default. This prevents optional provider and plugin behavior from entering the baseline merely because one deployment used it.

**Alternative considered:** Keep a completely empty default and document a template. Rejected because it knowingly ships application defaults that cannot perform their own maintenance and recreates the Management Center failure on every new installation.

### 2. Store one exact destination entry per service and domain

The implementation will group multiple paths for the same service and domain in one destination object. Each object uses:

- `tls: "bump"`
- `methods: ["GET"]`
- the exact domain
- only the paths assigned below

The CPA baseline is:

| Domain | Paths |
|---|---|
| `api.github.com` | `^/repos/router-for-me/CLIProxyAPI/releases/latest$`; `^/repos/router-for-me/Cli-Proxy-API-Management-Center/releases/latest$` |
| `github.com` | `^/router-for-me/Cli-Proxy-API-Management-Center/releases/download/[^/]+/management\\.html$` |
| `release-assets.githubusercontent.com` | `^/github-production-release-asset/1051566067/` |
| `raw.githubusercontent.com` | `^/router-for-me/models/refs/heads/main/models\\.json$`; `^/router-for-me/models/refs/heads/main/codex_client_models\\.json$` |

The Sub2API baseline is:

| Domain | Paths |
|---|---|
| `api.github.com` | `^/repos/Wei-Shaw/sub2api/releases/latest$`; `^/repos/openai/codex/releases/latest$`; `^/repos/openai/codex/releases\\?per_page=30$` |
| `raw.githubusercontent.com` | `^/Wei-Shaw/model-price-repo/main/model_prices_and_context_window\\.json$`; `^/Wei-Shaw/model-price-repo/main/model_prices_and_context_window\\.sha256$` |

Exact end anchors are used wherever the source constructs a fixed URL. The release-assets path is intentionally a start-anchored repository prefix because GitHub appends a release-specific asset identifier and signed query parameters.

**Alternative considered:** Reuse the existing production-style generic GitHub release expression. Rejected because it permits arbitrary owners and repositories and violates the project's fail-closed boundary.

### 3. Treat the Management Center redirect chain as one security unit

The default must include all three hosts used by the verified updater:

```text
api.github.com
        |
        v
github.com/.../management.html
        |
        v
release-assets.githubusercontent.com/github-production-release-asset/1051566067/...
```

The first request supplies release metadata and digest information. The second is restricted to the fixed repository and `management.html` filename. The final rule is restricted to that repository's numeric GitHub ID. Signed query parameters and asset identifiers are never copied into documentation or tests.

The GitHub release-asset URL shape is not a documented stable API. The least-privilege response is still to pin the observed repository prefix: a future GitHub format change fails closed and requires an explicit reviewed policy update.

**Alternative considered:** Allow all paths on `release-assets.githubusercontent.com`. Rejected because it would turn any GitHub release asset known to the service into an allowed download.

### 4. Distinguish the two Sub2API release-list endpoints by source semantics

`/repos/openai/codex/releases?per_page=30` is part of the default because the automatically enabled Codex synchronizer uses it when `releases/latest` fails or does not identify a usable stable client release.

`/repos/Wei-Shaw/sub2api/releases?per_page=15` remains optional because the pinned source uses it only to list rollback candidates. It is not a fallback for the Sub2API latest-version check. Binary update and rollback are also inconsistent with the repository's image-pinned deployment model.

**Alternative considered:** Allow both list endpoints because both are read-only GitHub API calls. Rejected because that classifies by method instead of by enabled fresh-install behavior.

### 5. Exclude remote destination delegation from the baseline

The CPA plugin registry is not included. Its contents name dynamic repositories, and opening the store fans out to each repository's release API. A wildcard that makes this work would effectively delegate future egress authorization to the mutable remote registry. Operators must instead allow each deliberately installed plugin's API path, browser download path, and numeric release-asset repository ID.

The same boundary excludes provider endpoints, OAuth user endpoints, metadata plugins, user-configured URLs, backups, payment integrations, private overrides, and local ZCode services.

The CPA mirrors `models.router-for.me` and `cpamc.router-for.me` are not defaults. The primary GitHub model source is sufficient, and the CPA source explicitly warns that the fallback Management Center page is downloaded without digest verification.

**Alternative considered:** Include every fixed URL literal found in the pinned source. Rejected because many literals are provider-specific, feature-gated, user-authenticated, or weaker-integrity fallbacks.

### 6. Preserve copy-on-create runtime policy semantics

No migration logic is added to `scripts/init-egress-proxy.sh`. Changing the tracked example affects only a new installation or an operator who intentionally recreates the runtime policy. Existing installations receive a documented table and manually merge only the rules appropriate for their runtime.

This avoids surprising outbound access after a repository pull and preserves local private rules that cannot be reconstructed from tracked state.

**Alternative considered:** Merge missing defaults automatically during initialization or upgrades. Rejected because semantic JSON merging cannot safely distinguish operator removal from an outdated baseline and could silently broaden production egress.

### 7. Validate the tracked policy structurally and offline

`scripts/validate.sh` will render `egress-proxy/policy.example.json` into its temporary validation directory in addition to the existing synthetic test policy. A small inline validation will compare the default service/domain/method/TLS/path map with the expected baseline and fail on missing, extra, or broadened entries.

The validation remains offline. It does not call GitHub, depend on current release tags, or assert a release-specific asset identifier. Existing proxy integration tests continue to test generic method, path, host, DNS, and fail-closed behavior.

**Alternative considered:** Add live probes for all upstream endpoints. Rejected because network availability, GitHub rate limits, and mutable latest releases would make repository validation flaky and would not prove that the local ACL stayed least-privilege.

### 8. Change documentation vocabulary from deny-all to control-plane-only

README installation text will state that the generated policy permits only the listed built-in control-plane reads and denies provider and user-defined egress. The egress section will present three groups:

1. enabled default control-plane rules;
2. optional management rules, including Sub2API rollback listing and per-plugin examples;
3. provider- or deployment-specific rules.

It will also state that existing runtime policy is not updated automatically and that Sub2API upgrades continue through pinned container images rather than in-container asset downloads.

## Risks / Trade-offs

- **GitHub changes its release-asset redirect host or path format** → The download fails closed. Revalidate the repository ID and add a reviewed exact rule only after observing the new chain; do not pre-authorize speculative CDN hosts.
- **Mutable `main` branch metadata changes upstream** → The policy permits only fixed non-executable model and pricing data paths already consumed by the pinned applications. Keep plugin registries and executable assets outside the baseline.
- **GitHub or Raw GitHub is unavailable or rate-limited** → Applications continue using their cached or bundled data. Do not weaken integrity by enabling the unverified Management Center fallback by default.
- **Default outbound requests expose installation IP and timing to GitHub** → Document the exact automatic requests. No default rule requires a user or provider token.
- **TLS bump becomes incompatible with a future client implementation** → Keep the rule fail-closed and require a separate reviewed design before using splice, because splice cannot enforce method or path.
- **Version awareness is allowed while Sub2API self-update assets remain blocked** → Document that release information is informational and deployment upgrades use pinned images.
- **Current production contains a broader GitHub browser-download rule** → Do not copy it into tracked defaults. Narrowing the existing private runtime rule is a separate operator action outside this change.

## Migration Plan

1. Update the tracked example, documentation, and offline validation together.
2. Run the policy renderer, repository validation, and existing egress proxy tests.
3. For a new installation, run the existing initialization command; it copies the new baseline because no runtime policy exists.
4. For an existing installation, review the documented diff, manually merge desired exact rules into `data/egress-proxy/policy.json`, render the policy, and reconfigure the proxy using the existing operational procedure.
5. Do not deploy or modify production as part of applying this repository change.

Rollback is a normal repository revert of the three implementation files. Existing runtime policies are unaffected by either the forward change or its rollback. A fresh installation made after rollback returns to the prior empty default and loses the built-in control-plane allowances, but provider egress remains denied in both states.
