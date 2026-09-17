## Context

See `proposal.md` for motivation. Production currently pins `ghcr.io/tridefender/zcode-proxy:4.6.3@sha256:db3a82544991bf65ec50cdd6f5fd029f3bb96135a81cc19605fa7a8ccfa87a2c`. ZCode shares the `provider-sidecar-tunnel` network namespace, publishes no host port, and reaches the internet only through a service-scoped Squid policy. Core inference works, but live probes showed Squid `403` responses for supported control-plane and MCP paths absent from the ZCode policy.

The 4.6.5 multi-platform image digest is `sha256:87f3f9d806e9c0fac15df0d7392bd3a860a6fa95db134ac9cc4695e6a284baf1`; its linux/amd64 manifest is `sha256:4ba45321f5ca84377c715b09201fa4ceaf9eb46ea85c0db4181fe9d06a5565ee`, with OCI version `4.6.5`, revision `b7f4429ca09d7e561812ead6aa0ba447817d7355`, and source `https://github.com/TriDefender/zcode-api`.

## Goals / Non-Goals

**Goals:**

- Preserve the current unpublished ZCode network boundary and fail-closed egress architecture.
- Pin the verified 4.6.5 image immutably.
- Permit the complete 4.6.5 feature set through exact service, domain, method, and path rules.
- Leave a runnable offline validation and a bounded live smoke check.

**Non-Goals:**

- No credential or API-key rotation.
- No change to `ZCODE_PROXY_CREDENTIAL_SECRET`, `data/zcode/credentials.json`, provider, plan, models, or CPA's ZCode API-key entry.
- No Cloudflare, CPA credential, database, Sub2API, AISIX, host firewall, or host-port change.
- No broad wildcard permission for future ZCode behavior; future paths require another reviewed rule.

## Decisions

### Pin the 4.6.5 multi-platform digest

Set both production ZCode image variables to:

```text
ghcr.io/tridefender/zcode-proxy:4.6.5@sha256:87f3f9d806e9c0fac15df0d7392bd3a860a6fa95db134ac9cc4695e6a284baf1
```

The index digest preserves architecture selection while remaining immutable. Pinning only the mutable tag was rejected; pinning the amd64 child manifest was unnecessary because the index itself is immutable and the deployment already uses the same tag-plus-index-digest form.

### Modify only the operator-owned ZCode policy

Keep the tracked fresh-install example unchanged because existing specs deliberately exclude provider and ZCode destinations. Edit only `services.zcode.destinations` in the production runtime policy, then regenerate artifacts with `scripts/render-egress-policy.py`.

Changing the tracked default or adding global domains was rejected because it would broaden fresh installs and unrelated services.

### Exact ZCode 4.6.5 egress contract

Preserve current entries and add only these missing rules:

| Domain | Method | Path expression | Function |
|---|---|---|---|
| `zcode.z.ai` | `POST` | `^/api/v1/oauth/cli/init($|[?])` | Z.AI login initialization |
| `zcode.z.ai` | `GET` | `^/api/v1/oauth/cli/poll/[^/?]+($|[?])` | Z.AI login polling |
| `zcode.z.ai` | `GET` | `^/api/v1/zcode-plan/billing/(balance|preview)($|[?])` | quota and claim preview |
| `zcode.z.ai` | `POST` | `^/api/v1/zcode-plan/billing/claim($|[?])` | claim execution |
| `zcode.z.ai` | `GET` | `^/api/v1/off-peak/ticket/availability($|[?])` | off-peak availability |
| `zcode.z.ai` | `POST` | `^/api/v1/off-peak/ticket($|[?])` | take ticket |
| `zcode.z.ai` | `POST` | `^/api/v1/off-peak/ticket/status($|[?])` | batch ticket status |
| `zcode.z.ai` | `POST` | `^/api/v1/off-peak/ticket/[^/?]+/settle($|[?])` | settle ticket |
| `zcode.z.ai` | `POST` | `^/api/v1/off-peak/anthropic/v1/messages($|[?])` | execute the ready off-peak model request |
| `open.bigmodel.cn` | `POST` | `^/api/mcp/(web_search_prime|web_reader|zread)/mcp($|[?])` | Bigmodel MCP servers |
| `api.z.ai` | `POST` | `^/api/mcp/(web_search_prime|web_reader|zread)/mcp($|[?])` | Z.AI MCP servers |
| `api.z.ai` | `POST` | `^/api/auth/z/login($|[?])` | exchange Z.AI OAuth access token for Biz token |
| `api.z.ai`, `bigmodel.cn` | `GET` | `^/api/biz/customer/getCustomerInfo($|[?])` | resolve account organization and project |
| `api.z.ai`, `bigmodel.cn` | `GET`, `POST` | `^/api/biz/v1/organization/[^/?]+/projects/[^/?]+/api_keys($|[?])` | list or create the ZCode coding-plan API key |
| `api.z.ai`, `bigmodel.cn` | `GET` | `^/api/biz/v1/organization/[^/?]+/projects/[^/?]+/api_keys/copy/[^/?]+($|[?])` | retrieve the coding-plan signing secret when available |

Existing exact entries remain authoritative for direct inference, start-plan and ultra inference, `/api/v1/oauth/token`, endpoint routing, captcha configuration, request-signing handshake, and Aliyun captcha resources.

The policy intentionally uses bounded identifiers (`[^/?]+`) only where the upstream API defines an opaque OAuth flow or ticket ID. A generic `/api/v1/*` or `/api/mcp/*` grant was rejected.

### No credential changes

The existing ZCode API key, provider-sidecar API key, credential encryption seed, and encrypted OAuth credentials remain byte-for-byte unchanged. This avoids coupling a version/ACL maintenance action to client reconfiguration or re-login.

### Validate positive and negative cases

Offline checks render Squid configuration and assert exact domain/method/path entries plus representative denials. The required endpoint inventory is independently derived from every non-test ZCode 4.6.5 outbound fetch construction site, rather than copied only from this table. Live checks issue unauthenticated or deliberately invalid requests from the ZCode container: an upstream response such as `200`, `400`, `401`, or application `403` proves routing, while `server: squid/6.13` with policy `403` indicates an ACL failure. Representative undeclared paths must continue returning Squid `403`.

## Risks / Trade-offs

- **Upstream adds another path after 4.6.5** → Keep fail-closed behavior; add it only after source or live-flow verification.
- **Regex accidentally overlaps another endpoint** → Use anchored expressions and add negative-path checks beside every variable-ID rule.
- **Pull succeeds but image metadata is unexpected** → Inspect pulled OCI version, revision, and source before replacing the running container.
- **Squid reload interrupts unrelated egress briefly** → Render and run offline checks first, then recreate only the bounded egress/ZCode chain during the approved window.
- **A feature cannot be fully exercised without user interaction or valid upstream state** → Accept an upstream application response as reachability evidence and record any human OAuth/claim step as pending rather than broadening ACLs.

## Migration Plan

1. Freeze a maintenance snapshot containing the old ZCode image reference, runtime ZCode policy, generated Squid artifacts, rendered Compose ZCode service, and current health evidence. Do not print credentials.
2. Pull the pinned 4.6.5 image and verify digest plus OCI version/revision/source before touching the running service.
3. Prepare the ZCode-only policy edit in a private temporary file, render generated artifacts, and run offline policy and Compose validation.
4. Verify the diff changes only both ZCode image variables, `services.zcode.destinations`, and generated ZCode policy artifacts; confirm credential values and non-ZCode service policy hashes are unchanged.
5. Apply the prepared files and recreate only containers required for the ZCode image and regenerated Squid policy. Keep ZCode without host-published ports.
6. Wait for egress proxy, tunnel, and provider-sidecar health checks, then verify core inference and each newly allowed feature path. Run representative undeclared-path denial checks.
7. Record final image identity, container health, positive/negative ACL results, and unchanged-scope evidence.

Rollback: restore the captured 4.6.3 image variables, prior runtime policy, and prior generated artifacts; recreate the same bounded container set; verify provider-sidecar health, core inference, and undeclared-path denial. Credentials require no rollback because they are never changed.
