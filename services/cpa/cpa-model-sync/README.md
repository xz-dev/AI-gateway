# cpa-model-sync

**Where:** `services/cpa/cpa-model-sync/`, Compose service `cpa-model-sync` (Rust). It sits on the private two-member network `model-sync-cpa` next to CPA. It has no listener, no published port and no database.

**What:** It owns the configured `models` membership of five CPA channel kinds: `openai-compatibility`, `claude-api-key`, `codex-api-key`, `xai-api-key` and `vertex-api-key`. Each round it discovers those channels through CPA's management API and reads each channel's upstream inventory through CPA `api-call`. It filters the inventory with the policy's per-channel regexes, and PATCHes `models` only when the result differs. CPA makes every upstream request with its own credentials.

**How to use:**

```sh
cargo run --locked -- --once config.example.json      # one round
cargo run --locked -- config.example.json             # periodic mode
cat config.example.json | CPA_MANAGEMENT_KEY=... cpa-model-sync --preview -   # read-only preview
```

In Compose, mount a private policy with `CPA_MODEL_SYNC_CONFIG`. Production changes go through `ansible-playbook playbooks/deploy-config.yml -e component=cpa-model-sync` (from `ops/ansible/`, preview with `--check --diff` first).

Details: [ownership boundary, commands, Compose wiring, exit codes and validation](docs/usage.md), [policy schema](docs/configuration.md), [round flow and write protocol](docs/architecture.md).

Historical records (point-in-time, not current production state): [`docs/records/`](docs/records/), starting with [PRODUCTION.md](docs/records/PRODUCTION.md).
