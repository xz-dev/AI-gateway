## Why

Production runs ZCode Proxy 4.6.3 while 4.6.5 is the latest stable release. Its fail-closed egress policy permits core inference but blocks supported ZCode functions including OAuth login, quota and claim operations, off-peak tickets, MCP Web Reader, and MCP ZRead.

## What Changes

- Upgrade only the production ZCode Proxy image from 4.6.3 to an immutable 4.6.5 image digest.
- Extend only the ZCode service's egress policy with exact domains, HTTP methods, and paths required by ZCode 4.6.5 supported functions.
- Regenerate Squid policy artifacts and verify both allowed ZCode flows and denied undeclared requests.
- Preserve existing ZCode credentials, API keys, credential-encryption secret, provider configuration, inbound exposure, and every non-ZCode service and policy.
- Retain a rollback point for the prior 4.6.3 image and ZCode egress policy.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `default-egress-policy`: Require the operator-owned production ZCode policy to cover the exact ZCode 4.6.5 control-plane, off-peak, MCP, inference, signing, routing, and captcha destinations without broad domain or path grants.

## Impact

- Production-private ZCode image pin and runtime overlay.
- Production-private `data/egress-proxy/policy.json` ZCode service entry and generated Squid artifacts.
- Existing egress-policy validation and ZCode reachability checks.
- ZCode-related containers during a separately approved production rollout.
- No credential rotation, Cloudflare change, CPA credential change, database change, Sub2API change, AISIX change, or host firewall change.
