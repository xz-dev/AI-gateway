## 1. Prepare ZCode 4.6.5 Inputs

- [x] 1.1 Resolve and record the immutable `ghcr.io/tridefender/zcode-proxy:4.6.5` multi-platform digest and verify OCI version `4.6.5`, source `TriDefender/zcode-api`, and expected revision from GHCR.
- [x] 1.2 Add offline ZCode policy fixtures/assertions for every new OAuth, billing, claim, off-peak, and MCP method/path plus representative undeclared paths, and verify the checks fail against the current production-shaped policy.
- [x] 1.3 Render the current Compose and egress configuration and capture a redacted rollback receipt containing the 4.6.3 image pin, ZCode policy hashes, generated artifact hashes, container health, and absence of ZCode host-published ports.

## 2. Implement the ZCode-Only Change

- [x] 2.1 Update only the production ZCode image variables to `ghcr.io/tridefender/zcode-proxy:4.6.5@sha256:87f3f9d806e9c0fac15df0d7392bd3a860a6fa95db134ac9cc4695e6a284baf1` and verify rendered Compose changes no other image, service, port, environment value, or credential.
- [x] 2.2 Extend only `services.zcode.destinations` with the exact OAuth, quota, claim, off-peak, and provider-specific MCP rules defined in `design.md`, then verify all existing ZCode inference, routing, signing, and captcha rules remain present.
- [x] 2.3 Regenerate Squid artifacts with `scripts/render-egress-policy.py` and verify generated configuration passes `squid -k parse`/repository egress validation without manual generated-file edits.
- [x] 2.4 Run the focused offline policy checks and verify declared ZCode requests are allowed while domain-wide, `/api/v1/*`, `/api/mcp/*`, malformed identifier, wrong-method, and unrelated-service requests remain denied.
- [x] 2.5 Compare credential and non-ZCode policy hashes before and after preparation and verify all API keys, `ZCODE_PROXY_CREDENTIAL_SECRET`, `credentials.json`, non-ZCode destinations, Cloudflare, CPA, database, Sub2API, AISIX, and host firewall configuration are unchanged.

## 3. Production Rollout — Separate Explicit Approval Required

- [x] 3.1 Present the exact prepared diff, affected container set, maintenance impact, backup receipt, validation evidence, and rollback command sequence; obtain separate explicit production execution approval before any write, pull, policy replacement, or container recreation.
- [x] 3.2 After approval, pull the pinned image and verify its resolved digest and OCI metadata match task 1.1 before changing the running deployment.
- [x] 3.3 Apply the prepared ZCode image and policy files atomically, recreate only the bounded ZCode/egress containers required by the changed image and generated Squid policy, and verify each affected health check becomes healthy.
- [x] 3.4 Verify CPA reaches the unchanged ZCode provider with the existing key through `/v1/models`, OpenAI chat, Anthropic messages, and Responses, and confirm ZCode still has no host-published port.
- [x] 3.5 Probe every newly allowed OAuth, quota, claim, off-peak, and MCP route from the ZCode network namespace and verify each reaches an upstream application response rather than a Squid ACL `403`.
- [x] 3.6 Probe representative undeclared ZCode paths and wrong methods and verify Squid still returns policy `403`, then confirm unrelated service health and non-ZCode egress behavior are unchanged.
- [x] 3.7 Record final image identity, health, positive/negative ACL evidence, unchanged-credential hashes, and rollback readiness in a redacted production receipt.

## 4. Rollback Gate

- [x] 4.1 Keep the captured 4.6.3 image pin and prior ZCode policy/generated artifacts ready for restoration; no rollback trigger occurred because image identity, health, reachability, denial, and unchanged-scope gates passed.
