# AISIX fallback capability repair — 2026-11-20

## Audit and planned scope

A Codex `503 / server_is_overloaded` exposed a pre-existing invalid AISIX fallback: CPA returned `400 / model_not_found` for `xl/gpt-6.2-sol`. The Sub2API policy change did not cause or reclassify that failure. No Sub2API rollback is needed.

Compared 26 AISIX aggregates (correcting the preliminary count of 25) with 573 IDs from the native CPA inventory. The candidate removes these eight secondary targets and adjusts only their fallback budgets:

| Aggregate | Removed secondary target |
|---|---|
| `grok-4.7` | `xl/grok-4.7` |
| `gpt-6.1-terra` | `xl/gpt-6.1-terra` |
| `gpt-6.2-sol` | `xl/gpt-6.2-sol` |
| `gpt-6.2-astra` | `xl/gpt-6.2-astra` |
| `gpt-6-luna` | `xl/gpt-6-luna` |
| `glm-5.2` | `sensenova/glm-5.2` |
| `gpt-image-2.5-flare` | `xl/gpt-image-2.5-flare` |
| `gpt-image-2.5-sunburst` | `xl/gpt-image-2.5-sunburst` |

The owner's November 20 decision removes eight non-Claude XL direct declarations: `xl/gpt-6.2-sol`, `xl/gpt-6-luna`, `xl/gpt-6.1-sol`, `xl/gpt-6.1-terra`, `xl/gpt-6.2-astra`, `xl/gpt-image-2.5-flare`, `xl/gpt-image-2.5-sunburst`, and `xl/grok-4.7`. The bare Luna aggregate keeps its Codex primary. No Congee entry was added because no sibling uses Congee as a fallback. The missing primary `zcode/glm-5.2` remains a finding only.

The same decision migrates `xl/claude-fable-5-1` and its sole caller, the bare `claude-fable-5-1` aggregate, to 5.2. Native CPA listed `xl/claude-fable-5-2` but not XL Fable 5.1. The seven-day Sub2API usage audit found zero counts for both old IDs, as did AISIX's retained logs since November 18 at 12:54 UTC. This does not cover older direct access outside Sub2API. Requests pinned to the old name may fail or take a different route; the unchanged CPA native catalog still includes 5.1 from other channels.

## Local acceptance

- The revised validator rejects the old configuration's eight fallback references, accepts the candidate with fresh native CPA inventory, and rejects absent or malformed inventory. The gate runs during preview and apply, before upload.
- Mock Responses on the deployed AISIX revision `67343036`: old resources reached the mock XL target and returned 400. The candidate returned 502 for initial overload, and 502 again after the admin status endpoint confirmed the only candidate was cooling. AISIX can re-attempt its only candidate while it is cooling. Neither candidate request called XL.
- On the deployed Sub2API revision `ede9e5ff`, the policy classifier ignores 5xx and the Responses forwarding path treats these upstream errors as eligible for failover before response commitment. The currently loaded error rule for 502/503 passes through the status; the default exhaustion mapper returns 502 for both if no rule applies. This is source and read-back evidence, not a production inference test.

## Deferred proposal: classify failures of gateway-selected targets

Consider a separate change at the AISIX aggregate boundary: when a valid public model resolves to an internal target that now returns a structured `model_not_found`, report the gateway-owned routing failure as 502, or report aggregate temporary unavailability as 503. Preserve real client unknown-model and policy errors. Use local mocks for both affected failover paths and genuine client 4xx. This is a proposal for owner decision, not part of this rollout.

## Deployment

The independent review covered revision `a9abbd8` and candidate SHA-256 `671fc1e692350addbb1d865a32e5145f8a364be66f9825531c2708f98466947d`. Validate run `44672177308` passed all four jobs for that revision. The fresh managed-file preview reported 54 OK, 1 change, no failures and `config_diff=false`.

The AISIX-only apply ran 04:31:19–04:31:53 UTC and finished with 66 OK, 5 changes and no failures. AISIX started at 04:31:48 UTC on the same image, running with zero restarts. The 47 other containers, Sub2API account 10 and its error rules, and Compose, environment and unmanaged AISIX config hashes were unchanged. Pre-apply connection samples were 3, 2 and 2; user-visible interruption was not measured.

The live resources and managed baseline matched the reviewed candidate hash. The protected pre-deploy copy matched `acff6bf63c98310ceb2a2d3e8a13cb9b1f0dd10d853427043c66a72bd38a0975`. The 04:44:23 UTC follow-up preview against the reviewed resources reported 51 OK, zero changes and no failures with `config_diff=false`. No rollback was needed; the production write window closed. The approved emergency path, pinned to `d74700a`, was limited to recovery from a failed AISIX behavior check during that window, not a stale public catalog.

## Zero-token readback

- **Effective AISIX:** 45 direct declarations and 26 aggregates, with model names and ordered targets matching the candidate. Only XL Fable 5.2 and Opus 5.5 remained. Every declared secondary target passed a fresh CPA native capability check.
- **Internal catalog:** At 04:42:44 UTC, generation 307 returned 354 models with no retired XL IDs. Bare and XL Fable 5.2 were present. Bare, Devin and ShuaiAPI Fable 5.1 remained as CPA-native entries.
- **Public catalog:** At 04:38:59 UTC the standard list still showed retired XL entries. At 06:27:05 UTC an ordinary authenticated `GET /v1/models` returned HTTP 200 and 354 IDs, with no `xl/gpt-*`, `xl/grok-*` or `xl/claude-fable-5-1`. Both `claude-fable-5-2` and `xl/claude-fable-5-2` were present. The bare `claude-fable-5-1`, `devin/claude-fable-5-1` and `shuaiapi/claude-fable-5-1` remained: they are CPA-native entries outside the AISIX removal scope. The list converged without a forced refresh; the earlier stale response does not identify a particular cache. The earlier public health check returned an empty 404.
- **Effective AISIX recheck:** At 06:11:10 UTC the status GET again returned 45 direct declarations and 26 aggregates. Its only explicit XL models were `xl/claude-fable-5-2` and `xl/claude-opus-5-5`. The bare Fable 5.2 route had only the XL 5.2 target; no explicit Fable 5.1 declaration or target remained. `gpt-6.2-sol` still targeted only `codex/gpt-6.2-sol`.

No production inference request, forced refresh or new credential was used.

## Observation: Codex model-list transfer interrupted

Between 06:27 and 06:31 UTC, the first `GET /v1/models?client_version=1` following the successful standard GET did not finish before the probe's 48-second process deadline. A separate Codex GET returned HTTP 200, but its chunked body ended after 406,351 bytes (`IncompleteRead`). Neither attempt provided a complete JSON document. Codex membership is unknown; this is not evidence of stale data.

The supervisor accepted the effective AISIX routes and the complete standard public list for task 3.2. No direct link to this change has been established, and the cause is undetermined. The interruption may affect Codex clients fetching their model list. For a separate owner-approved read-only investigation, compare the complete inner model-catalog Codex response size with the response received through Sub2API and front APISIX to locate any truncation. No further probe or production change was made for this observation.
