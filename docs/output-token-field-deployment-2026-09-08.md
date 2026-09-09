# Output-token fields: production deployment, 2026-09-08

## Deployed scope

Host: `<tailscale-ip>`, directory `/root/AI-gateway`.
Page: <http://<tailscale-ip>:9083/models-table> (Tailscale access).

The user explicitly confirmed manual deployment of only the Go image and table
HTML. The subsequently rejected explanatory paragraph was removed from both
local and production HTML. The three independent headers remain:
`max_tokens(abandon)`, `max_completion_tokens`, `max_output_tokens`.

- `.env`: only `MODELS_ENRICHER_IMAGE` changed.
- `model-catalog-sidecar/html/models-table.html`: three columns, no added notice.
- Only `models-enricher` was recreated, using `--no-deps --no-build --pull never`.
- The sidecar's directory bind made the atomic HTML replacement visible without
  recreating that container.
- All 47 other container identities and restart counts remained unchanged.
- `compose.yaml`, its override and Go configuration retained their baseline
  hashes. The repaired `pull_policy: never` remains in place.
- No Pi, Core, Rust, routing, authorization, egress, resource-limit or database
  changes were performed. No inference or load tests were run.

## Artifacts

The candidate was built from the prior deployed source plus only the five
reviewed Go files: `adapters.go`, `config.go`, `merge.go`, `ollama.go`, `sources.go`.
Those files' pre-change copies matched the deployed source; no other production
Go source or dependency changed. Pinned build/runtime images were reused with
`--network=none --pull=never`. Complete candidate effective Compose differed
only at `services.models-enricher.image`.

| Artifact | Identity |
|---|---|
| Image | `localhost/models-enricher:output-fields-20260908T045911Z` |
| Loaded Docker image ID | `sha256:902d7b8a05be0e171c7f76ca904275ced97bf0e7f6c936d939fd416723d35d2a` |
| Running executable SHA256 | `b30255f33fc96b98741cb8b6f9b0081050d0f7e0dfe9718426f6c52d23885427` |
| Archive SHA256 | `d5efcaaab7b567dd1ac8328956c4e5e637e3ea7b4c10d56ae2cd1d1f2a79a229` |
| Final HTML SHA256 | `9d50c92924c71ce6bb003c6eaa16b516da4cd6445a766a887af728d3fa158ade` |
| New Go container | `e5277c5806edec1eac82de1c22c7bbb1b2343e1f20f05b9a42649f1020781650` |

Podman and Docker image IDs differ. Loaded filesystem layers and runtime image
configuration matched the archive; the executable copied from the running
container matched the recorded hash.

## Production observation

A bounded read-only pass through production port 9083 returned HTTP 200 for the
page and `/v1/models?client_version=v0.65.0`. The served HTML matched the final
file hash and contained all three headers without the rejected paragraph.

The directory response contained **191 models**, 8,716,426 bytes:

| Field | Present | Missing |
|---|---:|---:|
| `max_tokens` | 46 | 145 |
| `max_completion_tokens` | 0 | 191 |
| `max_output_tokens` | 17 | 174 |

Actual response examples (`未知` below means absent property):

| Model | max_tokens | max_completion_tokens | max_output_tokens |
|---|---:|---:|---:|
| `codex/gpt-5.6-sol` | 128000 | 未知 | 未知 |
| `gpt-5.6-sol` | 128000 | 未知 | 未知 |
| `shuaiapi/claude-opus-4-8` | 未知 | 未知 | 128000 |
| `zcode/glm-5` | 131072 | 未知 | 未知 |

All four available Codex static children matched their parent's three fields,
including property absence. Missing values were not filled for display. These
counts describe this response only, not a controlled comparison with historical
188-model captures or proof of exhaustive upstream capability coverage.

Response SHA256:
`070d3f206f51db4de76975fdceec1a37df59764fb6f2f7b8271a57f9c7ea7ae4`.

Final inspection: 48 running containers; all nine declared health checks
healthy; new Go restart count zero; no inspected container OOM flag. Go runtime
environment and HostConfig matched the baseline. Container flags are not a
host-wide historical OOM audit.

The Playwright direct navigation to the Tailscale URL failed with
`net::ERR_ABORTED` before obtaining the catalog. Production HTML/JSON were
therefore verified through the same server's localhost port 9083. This receipt
does **not** claim successful live-browser DOM validation. Actual-page DOM and
handler fidelity had already passed locally; no replacement browser harness
or repeated production probe was added. Front entitlement, inference and
all-client compatibility were not revalidated by this bounded deployment pass.

## Evidence and recovery

Local evidence: `/root/.cache/output-token-deploy-20260908/`.
Private production backup/evidence:
`/root/AI-gateway/.output-token-deploy-20260908/`.

Retained rollback image:
`localhost/models-enricher:identity-20260908T004700Z`.
Backups: `env.before` and `models-table.html.before`; previous Compose and
override copies remain for evidence, not wholesale restore.

If rollback is explicitly needed, manually restore only those two target
files with their original ownership/modes, then recreate only
`models-enricher` with no dependencies, builds or pulls. Keep the production
Compose startup repair intact. Do not restore databases or restart the stack.

No commit or archive was performed. Verification stopped after the scoped
production checks; user acceptance of the displayed results remains separate.
