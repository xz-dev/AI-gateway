## Why

On November 20, 2026, Codex overload made AISIX select `xl/gpt-6.2-sol`. CPA did not register that target and returned HTTP 400 for a valid public model. The XL target is not a usable fallback and must also be removed from the AISIX model inventory.

## What Changes

- Audit every AISIX fallback against CPA's native model inventory and corroborate missing entries against registration configuration where needed.
- Remove unsupported fallback references, including all references to `xl/gpt-6.2-sol`, and delete that direct declaration. Use an exact registered Congee target only if the same AISIX configuration already uses Congee as a sibling-model fallback. Otherwise preserve remaining targets and normalize their fallback budget.
- Extend the AISIX deployment validator to check every CPA-backed fallback against a fresh native capability snapshot before deployment. Test primary overload and cooling with the old invalid fallback present only in the mock upstream.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `aisix-route-lifecycle`: Require native CPA capability checks for explicit fallback targets.

## Impact

Production activation changes only the private AISIX resources file. No provider or alias is added, and CPA, Sub2API, images, credentials and error classification are unchanged. A route with no verified fallback loses only its unusable alternate. A separate proposal may address status mapping when an otherwise valid gateway-selected fallback returns `model_not_found` at runtime.
