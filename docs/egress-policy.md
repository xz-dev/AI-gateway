# Egress policy

Squid allowlist policy: the tracked control-plane baseline, provider rules and optional components.

Fresh installations start with a control-plane-only policy. Every entry below is enabled for the named service, uses `GET` with `tls: "bump"`, and matches only the listed domain and JSON path expression:

| Service | Component | Exact domain | Anchored path |
|---|---|---|---|
| CPA | Core version check | `api.github.com` | `^/repos/router-for-me/CLIProxyAPI/releases/latest$` |
| CPA | Management Center release metadata | `api.github.com` | `^/repos/router-for-me/Cli-Proxy-API-Management-Center/releases/latest$` |
| CPA | Management Center browser download | `github.com` | `^/router-for-me/Cli-Proxy-API-Management-Center/releases/download/[^/]+/management\\.html$` |
| CPA | Management Center release asset | `release-assets.githubusercontent.com` | `^/github-production-release-asset/1051566067/` |
| CPA | Model catalog | `raw.githubusercontent.com` | `^/router-for-me/models/refs/heads/main/models\\.json$` |
| CPA | Codex client model catalog | `raw.githubusercontent.com` | `^/router-for-me/models/refs/heads/main/codex_client_models\\.json$` |
| Sub2API | Core version check | `api.github.com` | `^/repos/Wei-Shaw/sub2api/releases/latest$` |
| Sub2API | Codex latest release | `api.github.com` | `^/repos/openai/codex/releases/latest$` |
| Sub2API | Codex stable-release fallback | `api.github.com` | `^/repos/openai/codex/releases\\?per_page=30$` |
| Sub2API | Claude Code latest release | `api.github.com` | `^/repos/anthropics/claude-code/releases/latest$` |
| Sub2API | Claude Code stable-release fallback | `api.github.com` | `^/repos/anthropics/claude-code/releases\\?per_page=30$` |
| Sub2API | Pricing data | `raw.githubusercontent.com` | `^/Wei-Shaw/model-price-repo/main/model_prices_and_context_window\\.json$` |
| Sub2API | Pricing hash | `raw.githubusercontent.com` | `^/Wei-Shaw/model-price-repo/main/model_prices_and_context_window\\.sha256$` |

The Management Center flow needs all three download hosts. Its final GitHub URL contains a release-specific asset identifier and signed query, so the policy pins only the start-anchored repository-ID prefix; never copy signed redirect queries into policy, documentation, or logs.

Provider destinations remain deployment-specific. Example bumped provider rule:

```json
{
  "domain": "api.openai.com",
  "tls": "bump",
  "methods": ["POST"],
  "paths": ["^/v1/(responses|chat/completions|embeddings)($|[?])"]
}
```

Example TLS-preserving destination:

```json
{
  "domain": "subscription.example.com",
  "tls": "splice"
}
```

The tracked default does not authorize provider inference. `models-enricher` has no internet egress: models.dev and modelparams.dev data come from exactly pinned npm snapshots (`@opencode-ai/models`, `modelparams`) converted at image build time and bumped by an hourly Dependabot entry, and channel upstream data comes through CPA `api-call`. Optional components need separate exact rules:

| Component | Exact domain | Anchored path |
|---|---|---|
| Sub2API rollback-version listing | `api.github.com` | `^/repos/Wei-Shaw/sub2api/releases\\?per_page=15$` |
| CPA plugin registry, listing only | `raw.githubusercontent.com` | `^/router-for-me/CLIProxyAPI-Plugins-Store/main/registry\\.json$` |
| CPA model-catalog fallback | `models.router-for.me` | `^/models\\.json$` |
| CPA Codex-catalog fallback | `models.router-for.me` | `^/codex_client_models\\.json$` |

Plugin registry access does not authorize plugin installation. Add each deliberately installed plugin's exact GitHub release API path, repository-specific browser download path, and numeric `release-assets.githubusercontent.com` repository-ID prefix. Never use an owner, repository, release-path, or repository-ID wildcard. Do not enable `cpamc.router-for.me` by default: CPA identifies that fallback Management Center page as lacking release digest verification.

Provider inference and OAuth, GitHub user-profile APIs, backup, payment, webhook, private override, and local ZCode destinations remain operator-owned. Sub2API's default latest-release rule provides version awareness only; upgrade the pinned `SUB2API_IMAGE` and recreate the coupled stack instead of allowing in-container binary, checksum, or rollback asset downloads.

Repository upgrades do not modify an existing `data/egress-proxy/policy.json`. Existing installations must compare it with `egress-proxy/policy.example.json`, manually merge only desired exact rules, run `./scripts/init-egress-proxy.sh`, and validate before recreating Squid with its dependent clients.

Domain entries may be exact names or start with `.` to include subdomains. A domain may have multiple `bump` entries when different paths require different methods, but it cannot mix `bump` and `splice`. IP literals, plaintext HTTP, missing SNI, CONNECT ports other than 443, unlisted redirects, unknown methods/paths, private/link-local/loopback/CGNAT/documentation/multicast/reserved destinations, and malformed upstream certificates fail closed. Never allow all of GitHub: add only the exact repository API, raw-content, or release paths a configured component actually reads. Re-run `./scripts/init-egress-proxy.sh` after every policy edit, then validate and recreate Squid with its dependent clients together.
