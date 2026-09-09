# CPA model synchronization

> **Production acceptance remains blocked; both attempts were rolled back.**
> The first omitted channel filters and exceeded 16 MiB. Filtering is now corrected
> and the 32 MiB candidate withdrawn, but the second attempt encountered Sub2API
> cgroup OOM kills before candidate public-load acceptance was completed.
> See [PRODUCTION.md](PRODUCTION.md) and [CATALOG-LIMIT.md](CATALOG-LIMIT.md).
> Production uses the original deployment. Local test passes in
> [VALIDATION.md](VALIDATION.md) do not constitute production acceptance.

Rust owns configured model inventories for **openai-compatibility, claude-api-key,
codex-api-key, xai-api-key and vertex-api-key**. Go remains the metadata enricher.
Gemini and Interactions retain their existing Go inventory and filters. OAuth is
not managed by Rust; this does not exclude OAuth models from the downstream catalog.

## Run

```sh
cargo run --locked -- --once config.example.json
# Periodic mode, immediate first round:
cargo run --locked -- config.example.json
```

Inject `CPA_MANAGEMENT_KEY` privately through the environment. Do not put upstream
keys in the policy file. `client_version` is explicit: the example `0.101.0` is a
sample, not a claim about the latest client. Set a version accepted by your upstream.

Compose adds only a dedicated internal two-member network to CPA's namespace.
The sidecar resolves `cli-proxy-api` to **172.30.44.2:8317**, directly to CPA, not
APISIX or a relay. It has no listener, public port, external network, database,
helper process, or dependency on a healthcheck timer. No production start or
migration is implied by adding the service.

Override `CPA_MODEL_SYNC_CONFIG` with a private policy file and optionally
`CPA_MODEL_SYNC_IMAGE` with the built versioned image. The default mounts the
example policy. Mounting a policy must not copy the provider inventory: CPA remains
the source of providers and credentials.

```json
{
  "cpa_url": "http://cli-proxy-api:8317",
  "client_version": "0.101.0",
  "interval_seconds": 600,
  "requests": { "retries": 3, "retry_delay_ms": 1000, "timeout_ms": 30000 },
  "channels": {
    "exact-cpa-prefix": { "path": "/v1/models", "include": ["^A$"], "exclude": [] }
  }
}
```

Unknown fields and invalid regexes are rejected. Retries must be 0–30, delays must
fit the finite exponential budget, and request timeout/interval must be positive.
Omitting a channel policy means no filtering. The checked-in example deliberately
keeps NIM's three selected IDs and ShuaiAPI's Claude-only policy; do not drop these
when preparing this deployment. `exclude` wins over `include`.
Filters match exact upstream ID strings; no model family/chat classification,
static aliases or `excluded-models` wildcard conversion is added.

## Semantics

- Fetch the complete inventory; never write a partial, malformed, oversized or
  originally empty inventory. Claude pagination is capped at 128 pages; bounded
  response reads use 8 MiB for management and 32 MiB for inventory envelopes.
- Explicit filtering may produce `models=[]`. This means zero **configured**
  models, not necessarily zero runtime models: CPA's native fallback remains intact.
- Write only `models`, as a full replacement. Manual extra models/aliases/attributes
  will be overwritten next round. Credentials, disabled, prefix, URL, proxy and
  `excluded-models` are not managed by this process.
- Disabled channels still refresh, without enabling them or publishing inventory
  around CPA. If no live auth index exists, CPA-returned credentials may be used
  only transiently in its own `api-call` headers, never for direct upstream access.
- Compare against fresh CPA state, not a private history cache. Outer model and
  object-key order do not matter; other fields, types, duplicates and nested array
  order do. Same-name alias defaults are normalized. A matching second round writes
  nothing, including after process/CPA restart.
- Each operation gets one initial attempt plus at most three retries by default,
  delayed 1/2/4 seconds. A failed pagination attempt starts over, without nested
  page retries. A lost write response uses remaining attempts to re-read; an already
  applied value is not written again. Exhausted ambiguity is `unconfirmed`, not
  success, and never triggers a whole-config restore.
- At most two channel workers fetch together. A single process-local mutex spans
  the fresh CPA read, comparison, PATCH and uncertain-result confirmation, avoiding
  CPA's known concurrent-write bug. Run **one active synchronizer per CPA**; this
  mutex does not coordinate other processes or external management writers. No
  fixed waiting period or cross-channel transaction is added. Rounds do not overlap.
  After a slow
  round the next strictly future monotonic tick is used; missed ticks are discarded.
  Ten minutes is **not** an end-to-end maximum staleness guarantee: operation time,
  failures, CPA propagation and downstream caches also contribute.
- stdout contains one updated/unchanged/failed/unconfirmed summary per round;
  stderr contains logical channel identity, static reason and configured
  `max_attempts` (not an observed attempt count). `--once` exits 1 for failed or
  unconfirmed results, 2 for startup/configuration errors. Periodic mode retries
  failed work in subsequent rounds. Full inventories are local to each round.

## Go policy migration

For each current CPA prefix, inspect its actual kind; never infer kind from a
name. For the five managed kinds, move only include/exclude to this JSON policy.
Copy an explicit path if inventory discovery needs it; Go may still need that
path for metadata lookup. Keep `source_priority`, `lookup_ids`, metadata references,
overrides and Ollama settings in Go. Its runtime validator explicitly rejects
remaining include/exclude on discovered managed channels.

Gemini/Interactions keep their Go include/exclude and upstream-derived membership.
Custom pools and static alias configuration also stay in the existing Go/downstream
layer. Do not copy them into this synchronizer.

Checked-in migration inventory (`models-enricher/config.yaml`):

| Prefix | Existing include/exclude/path | Change |
| --- | --- | --- |
| axis | absent | No policy to move; metadata unchanged |
| xl | absent | No policy to move; lookup/reference unchanged |
| zcode | absent | No policy to move; overrides unchanged |
| ollama-cloud | absent | No policy to move; Ollama/lookup/overrides unchanged |
| nim | absent | No policy to move; source priority unchanged |
| shuaiapi | absent | No policy to move; source priority unchanged |
| commandcode | absent | No policy to move; existing egress limitation unchanged |
| gmicloud | absent | No policy to move; native metadata unchanged |

Thus the checked-in Rust policy has `channels: {}`; no provider list or metadata
configuration was duplicated. This table is **not** a production provider-kind
inventory. Private runtime policies still require the same type-aware migration.

Production policy is carried by the private deployed policy file, not this
repository. The approved runtime filters (NIM exact three-ID include, ShuaiAPI
`^claude-`, `skip_channels: [gmicloud]`) live in that private policy and in the
parallel Go `skip_channels` flag; examples here document the mechanism only.
Provider-qualified chains and exact `lookup_ids` (94 verified mappings at last
review) are configured in `models-enricher/config.yaml`; see
[SOURCE-BINDING-REVIEW.md](SOURCE-BINDING-REVIEW.md) for the binding-evidence
boundary — lookup-target existence is execution evidence, not reseller-identity
proof, and unverified variants remain explicitly unset.

## Validation

```sh
cargo fmt --check
cargo test --locked
cargo clippy --locked --all-targets -- -D warnings
cd ../models-enricher
go test -race ./...
go vet ./...
```

See [COMPATIBILITY.md](COMPATIBILITY.md) for the pinned, isolated real-CPA probe.
The existing layered HTTP fixture and front Lua contract remain separate downstream
gates. The pre-existing exact egress-policy baseline failure is not repaired here.
Independent model-based review is waived for this change at the user's request;
local automated checks must not be described as independent review.
