# cpa-model-sync

Rust sidecar that owns the **configured `models` membership** of five CPA channel
kinds: `openai-compatibility`, `claude-api-key`, `codex-api-key`, `xai-api-key` and
`vertex-api-key`. Each round it discovers those channels through CPA's management
API, fetches each channel's upstream inventory through CPA's management `api-call`,
filters it with the policy's per-channel regexes, and PATCHes the channel's `models`
array only when the canonical result differs from what CPA currently holds. It never
contacts upstream providers itself - CPA performs those requests with the credentials
it already holds. The process has no listener and no database.

Policy schema: [docs/configuration.md](docs/configuration.md).
Round flow, write protocol and error reasons: [docs/architecture.md](docs/architecture.md).

## Ownership boundary

- **Rust (this project)** owns the configured model membership of the five managed
  kinds. It writes only the `models` array of those kinds.
- **Go (`models-enricher`)** only enriches metadata of members CPA already publishes
  for those kinds. It never decides the member set. Its runtime validator rejects a
  configured `include`/`exclude` on a discovered managed channel with
  `move include/exclude to cpa-model-sync`, returning HTTP 500
  `catalog_configuration_conflict` instead of a catalog.
- **Gemini and Interactions** (`gemini-api-key`, `interactions-api-key`) keep their
  existing Go inventory and Go `include`/`exclude`; Rust never contacts them.
- **OAuth** (`auth-files`, `oauth-model-alias`) is not managed by Rust. This does not
  exclude OAuth models from the downstream catalog.
- `models-enricher`'s `exclude_models` is unrelated: it hides exact public IDs from
  the public catalog after enrichment and does not change synchronization.

## Run

```sh
# One round, then exit:
cargo run --locked -- --once config.example.json

# Periodic mode; first round starts immediately:
cargo run --locked -- config.example.json

# Read-only preview; policy is read from stdin:
cat config.example.json | CPA_MANAGEMENT_KEY=... \
  CPA_MODEL_SYNC_IMAGE_IDENTITY=<immutable-image-id> \
  cpa-model-sync --preview -
```

- `CPA_MANAGEMENT_KEY` is required and must be non-empty in every mode. Inject it
  through the environment; never put upstream keys in the policy file.
- `CPA_MODEL_SYNC_IMAGE_IDENTITY` is used by `--preview` only and defaults to the
  empty string.
- `client_version` is explicit in the policy. The example value `0.101.0` is a
  sample, not a claim about the latest upstream client. Set a version your upstream
  accepts.
- Preview performs discovery and inventory reads but never PATCHes CPA.

## Compose notes

`compose.yaml` service `cpa-model-sync` (around line 791):

- Mounts `${CPA_MODEL_SYNC_CONFIG:-./cpa-model-sync/config.example.json}` at
  `/app/config.json` (read-only) and runs the default daemon command with that path.
  Override `CPA_MODEL_SYNC_CONFIG` with a private policy file; override
  `CPA_MODEL_SYNC_IMAGE` to pin a built versioned image (compose default tag:
  `cpa-model-sync:v0.1.3`).
- Joins only the dedicated internal two-member network `model-sync-cpa`
  (172.30.44.0/29), with `extra_hosts: cli-proxy-api:172.30.44.2`. It therefore
  reaches CPA directly at 172.30.44.2:8317, not APISIX or a relay.
- No published port, no listener, no external network, no database, no helper
  process and no healthcheck.
- Runs read-only as `65534:65534` with `cap_drop: ALL`, `no-new-privileges`,
  `pids_limit: 16`, `mem_limit: 256m`, `cpus: 0.5`, and waits for
  `cli-proxy-api` to be healthy.
- Mounting a policy must not copy the provider inventory: CPA remains the source of
  providers and credentials. Adding this service does not by itself start or migrate
  anything.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | `--once` with zero failed and zero unconfirmed channels, or `--preview` succeeded |
| 1 | `--once` with at least one failed or unconfirmed channel |
| 2 | Startup or configuration error (usage, unreadable or invalid policy, invalid retry budget or interval, invalid regex in `--preview`, missing or empty `CPA_MANAGEMENT_KEY`), or a read failure during `--preview` |

Periodic mode retries failed channels in later rounds and runs until it is stopped;
it never exits for a channel failure.

## Validation

```sh
cargo fmt --check
cargo test --locked
cargo clippy --locked --all-targets -- -D warnings

# Downstream, unchanged by this project:
cd ../models-enricher && go test -race ./... && go vet ./...
```

[COMPATIBILITY.md](COMPATIBILITY.md) holds the pinned, isolated real-CPA probe
(`tests/real_cpa.go`); `tests/resources/measure.py` reproduces the fixed
five-round resource measurement. Local checks are not production acceptance.

## Documentation

Current reference:

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
