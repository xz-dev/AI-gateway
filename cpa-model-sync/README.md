# cpa-model-sync

**Where:** Compose service `cpa-model-sync` (Rust). It sits on the private two-member network `model-sync-cpa` next to CPA. It has no listener, no published port and no database.

**What:** It owns the configured `models` membership of five CPA channel kinds: `openai-compatibility`, `claude-api-key`, `codex-api-key`, `xai-api-key` and `vertex-api-key`. Each round it discovers those channels through CPA's management API and reads each channel's upstream inventory through CPA `api-call`. It filters the inventory with the policy's per-channel regexes, and PATCHes `models` only when the result differs. CPA makes every upstream request with its own credentials.

**How to use:**

```sh
cargo run --locked -- --once config.example.json      # one round
cargo run --locked -- config.example.json             # periodic mode
cat config.example.json | CPA_MANAGEMENT_KEY=... cpa-model-sync --preview -   # read-only preview
```

In Compose, mount a private policy with `CPA_MODEL_SYNC_CONFIG`. Production changes go through `ansible-playbook ops.yml --tags deploy-model-sync`.

Details: [ownership boundary, commands, Compose wiring, exit codes and validation](docs/usage.md), [policy schema](docs/configuration.md), [round flow and write protocol](docs/architecture.md).

## Documentation

Current reference:

- [docs/usage.md](docs/usage.md) - ownership boundary, commands, Compose wiring,
  exit codes, validation.
- [docs/configuration.md](docs/configuration.md) - policy schema, defaults,
  validation, filter semantics, worked examples, common mistakes.
- [docs/architecture.md](docs/architecture.md) - round flow, inventory fetch,
  canonical comparison, serialized write protocol, concurrency, preview output,
  logging, static error reasons.

Historical records (kept as written; they describe earlier attempts, not the current
state):

| Record | Content |
| --- | --- |
| [PRODUCTION.md](PRODUCTION.md) | Two production attempts and their rollback |
| [CATALOG-LIMIT.md](CATALOG-LIMIT.md) | Catalog size, channel policy and resource gates |
| [VALIDATION.md](VALIDATION.md) | Local validation results and known gaps |
| [COMPATIBILITY.md](COMPATIBILITY.md) | Real-CPA compatibility probe and reproduction |
| [EVIDENCE.md](EVIDENCE.md) | Delta scenario evidence mapping |
| [SOURCE-BINDING-REVIEW.md](SOURCE-BINDING-REVIEW.md) | Source-binding/deployment boundary (Chinese) |
| [SUB2API-OOM.md](SUB2API-OOM.md) | Read-only Sub2API OOM findings |
| [UPSTREAM-MIGRATION.md](UPSTREAM-MIGRATION.md) | Upstream image migration checks |
| [DEPLOYED-2026-09-07.md](DEPLOYED-2026-09-07.md) | Manual production deployment |
| [RECOVERY-2026-09-07.md](RECOVERY-2026-09-07.md) | Rollout recovery and evidence boundary |

These are point-in-time records, not a statement of current production state. For
production status, see [PRODUCTION.md](PRODUCTION.md) and the later records above;
local test passes are not production acceptance.
