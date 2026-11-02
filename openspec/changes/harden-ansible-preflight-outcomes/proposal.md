## Why

The shared preflight can confuse the standalone `podman-compose` provider with its container engine, and its existing fixture forces explicit Docker settings instead of exercising detection. The optional AISIX Admin probe also prints a skipped/failed message after a successful command.

## What Changes

- Exercise automatic provider selection, explicit overrides and check-mode boundaries through the existing local Ansible fixtures.
- Keep provider argv and canonical engine distinct, retaining Docker preference and existing unsupported-transaction refusals; reject an empty effective provider with a named prerequisite error.
- Report optional Admin command outcomes accurately without changing its nonfatal contract, credentials, execution target or receipt meaning.
- Keep command-probe success separate from effective route/model verification. No production rollout or claim to reproduce the historical private-context empty-sequence failure is included.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `production-ops-tooling`: specify canonical runtime selection and truthful, sanitized optional Admin probe outcomes.

## Impact

The shared `ai_ops` resolution and its existing component rollback-renderer caller, AISIX probe diagnostics, and the two existing fixture scripts change. The controller toolchain and collection pins remain unchanged. No production configuration, routing, management port, account, image or target dependency changes are needed.
