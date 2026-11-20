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

The operator explicitly requested removing the `xl/gpt-6.2-sol` direct declaration as well. The wildcard remains unchanged; losing an exact declaration does not guarantee that all callers get 404. No sibling used Congee as a fallback, so no Congee entry was added. Two missing primary targets (`zcode/glm-5.2` and `xl/claude-fable-5-1`) are findings only. No other declaration was altered.

## Local acceptance

- The revised validator rejects the old configuration's eight fallback references, accepts the candidate with fresh native CPA inventory, and rejects absent or malformed inventory. The gate runs during preview and apply, before upload.
- Mock Responses on the deployed AISIX revision `67343036`: old resources reached the mock XL target and returned 400. The candidate returned 502 for initial overload, and 502 again after the admin status endpoint confirmed the only candidate was cooling. AISIX can re-attempt its only candidate while it is cooling. Neither candidate request called XL.
- On the deployed Sub2API revision `ede9e5ff`, the policy classifier ignores 5xx and the Responses forwarding path treats these upstream errors as eligible for failover before response commitment. The currently loaded error rule for 502/503 passes through the status; the default exhaustion mapper returns 502 for both if no rule applies. This is source and read-back evidence, not a production inference test.

## Deferred proposal: classify failures of gateway-selected targets

Consider a separate change at the AISIX aggregate boundary: when a valid public model resolves to an internal target that now returns a structured `model_not_found`, report the gateway-owned routing failure as 502, or report aggregate temporary unavailability as 503. Preserve real client unknown-model and policy errors. Use local mocks for both affected failover paths and genuine client 4xx. This is a proposal for owner decision, not part of this rollout.

## Deployment

Pending independent review and guarded activation. No production generation probe or new credential is in scope.
