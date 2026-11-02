## Why

`gpt-6.1-terra` is registered upstream but lacks the operator's bare aggregate and its 500k catalog policy. Add the new model without altering older Terra routes.

## What Changes

- Add exact direct targets `codex/gpt-6.1-terra` and `xl/gpt-6.1-terra` to private AISIX resources, using existing Terra cooldown policy.
- Add bare `gpt-6.1-terra` with ordered Codex → XL failover, zero retries and one fallback.
- Add its static catalog entry to the tracked example and private profile, with `context_window` and `max_context_window` both 500000. Existing bundled metadata supplies model information.
- Preview, deploy only AISIX and model-catalog, and verify effective routes and catalog metadata. The owner approved both target order and short interruption on 2026-11-01.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This is an operational configuration instance of existing `aisix-model-routing`, `aisix-route-lifecycle`, `aisix-catalog-supplement` and `codex-models-enricher` requirements; no service contract or algorithm changes. Specs are explicitly skipped.

## Impact

Private AISIX resources and private model-catalog profile; tracked catalog example and operation record. Only AISIX and model-catalog are recreated. No provider/account/entitlement/credential, Compose, image or dependency changes. Congee remains directly available through existing CPA routing, outside this aggregate.
