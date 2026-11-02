# cpa-model-sync usage

Ownership boundary, commands, Compose wiring, exit codes and validation.

## Ownership boundary

- **Rust (this project)** owns the configured model membership of the five managed
  kinds. It writes only the `models` array of those kinds.
- **`model-catalog`** (TypeScript) only enriches metadata of members CPA already
  publishes. It never decides the member set, and its config has no channel
  `include`/`exclude`: `validate-config` rejects them as legacy fields.
- **Gemini and Interactions** (`gemini-api-key`, `interactions-api-key`) keep their
  existing CPA inventory; Rust never contacts them.
- **OAuth** (`auth-files`, `oauth-model-alias`) is not managed by Rust. This does not
  exclude OAuth models from the downstream catalog.
- `model-catalog`'s `metadata.include_models` (`!` entries hide matching public IDs)
  is unrelated: it hides public IDs from the catalog after admission and does not
  change synchronization.

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

- Mounts `${CPA_MODEL_SYNC_CONFIG:-./services/cpa/cpa-model-sync/config.example.json}` at
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
cd ../../routing/model-catalog && npm test
```

[COMPATIBILITY.md](records/COMPATIBILITY.md) holds the pinned, isolated real-CPA probe
(`tests/real_cpa.go`); `tests/resources/measure.py` reproduces the fixed
five-round resource measurement. Local checks are not production acceptance.
