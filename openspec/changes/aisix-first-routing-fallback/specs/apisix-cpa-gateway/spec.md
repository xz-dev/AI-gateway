> Planning status: blocked on design decisions A–D. Task 0 must replace conditional alternatives with the approved contract before apply or archive.

## ADDED Requirements

### Requirement: Priority passthrough to AISIX or CPA

The internal instance SHALL apply the approved priority policy to classified model-bearing JSON HTTP inference: `POST /v1/responses`, `POST /v1/chat/completions` and `POST /v1/images/generations`, including front-bridge WS turns forwarded through Sub2API as HTTP. Decision B SHALL resolve AISIX-defined slash IDs and decision C SHALL resolve CPA-only bare IDs before activation.

AISIX SHALL be preferred within the approved priority scope. Only established AISIX model absence SHALL allow advancement to terminal CPA. AISIX SHALL receive the caller credential unchanged; CPA SHALL receive the CPA service credential. Model IDs and validated request bodies SHALL be preserved apart from the existing `prompt_cache_retention` removal. Existing size limits and file-backed request handling SHALL remain effective.

If decision A selects live-list or short-TTL selection, inference SHALL remain APISIX-native proxying. Membership SHALL be direct-backend evidence, never recursive entrance catalog lookup. If A selects error fallback, a bounded pre-commit relay SHALL inspect only the eligible local absence envelope and preserve incremental SSE, framing, safe headers, cancellation, existing timeout behavior and exact request replay. No selected mechanism SHALL buffer a whole successful stream or retry after response commitment.

Malformed classified payloads SHALL fail explicitly before lookup or forwarding. Requests outside classification SHALL retain current behavior: multipart `POST /v1/images/edits` to AISIX, direct WebSocket upgrades and other catch-all endpoints (including `/v1/messages`) to CPA. Catalog routes SHALL remain unchanged. Raw source-discovery traffic SHALL not traverse this entrance.

#### Scenario: AISIX-defined ID in approved scope reaches AISIX
- **WHEN** an authorized classified request names an ID in the approved priority scope that AISIX establishes it defines
- **THEN** AISIX's body and status reach the existing downstream handling path with no separate CPA inference request

#### Scenario: Absent ID reaches CPA when permitted
- **WHEN** the selected mechanism establishes AISIX absence and decisions B/C permit terminal fallback
- **THEN** CPA receives the unchanged validated body with the CPA service credential
- **AND** CPA's final response is not mistaken for another AISIX absence signal

#### Scenario: SSE streaming passes through
- **WHEN** Sub2API sends a streaming `POST /v1/responses` request
- **THEN** SSE bytes flow from the selected backend incrementally without full-response buffering, event rewriting or cross-backend replay after commitment

#### Scenario: Direct WebSocket passes through to CPA
- **WHEN** a client opens a direct WebSocket upgrade on `/v1/responses`
- **THEN** the upgrade and frames pass through to CPA without classification
- **AND** public bare-ID WS support SHALL instead use keeper → stateless responses-ws-sse-bridge → HTTP-only Sub2API with API-key account WS mode `off`
- **AND** keeper SHALL retain `IGNORE_CLIENT_STORE_FALSE=true`; Sub2API SHALL NOT move in front of keeper/bridge or use `passthrough`/`ctx_pool` for this chain, as explained in `docs/websocket-termination.md`

#### Scenario: Chosen backend failure does not re-route
- **WHEN** an AISIX-defined target or selected CPA backend returns 429/5xx, a provider error, timeout or mid-stream error
- **THEN** the failure surfaces without a separate attempt on the other backend

#### Scenario: Malformed classified payload does not fall through
- **WHEN** a classified path receives malformed JSON or lacks an extractable model ID
- **THEN** the entrance returns an explicit protocol-shaped error before lookup or forwarding

#### Scenario: Restricted caller cannot use discovery omission to bypass denial
- **WHEN** AISIX membership is incomplete for the caller because of ACL filtering or wildcard omission
- **THEN** omission does not authorize a CPA attempt
- **AND** the unsafe list-based rollout is blocked or the separately approved safe membership policy applies

## REMOVED Requirements

### Requirement: Slash-rule passthrough to CPA or AISIX

**Reason**: The requested priority policy replaces the slash selector. The mechanism and compatibility effects are still operator decisions, not approved by this planning draft.

**Migration**: Resolve A–D before apply. Replace with the approved priority-passthrough requirement at the same classified seam. Preserve body validation, credential boundaries, streaming, direct WS and all non-classified endpoints. No generic provider-error retry is introduced.
