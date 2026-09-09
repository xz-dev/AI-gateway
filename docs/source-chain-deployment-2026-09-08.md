# Source-chain configuration deployment, 2026-09-08

## Scope

The user approved the five-channel configuration candidate and requested manual
production deployment. Only `models-enricher/config.yaml` changed in production.
The existing Go image was reused; only its container was recreated to load the
startup-read configuration.

Appended sources (earlier existing sources retain higher priority):

| Channel | Appended source_priority entries |
|---|---|
| axis | models.dev/openai |
| xl | models.dev/openai, models.dev/xai, models.dev/zai-coding-plan, models.dev/moonshotai, models.dev/deepseek |
| commandcode | models.dev/openai, models.dev/anthropic |
| nim | models.dev/nvidia |
| ollama-cloud | models.dev/ollama-cloud, after existing ollama_cloud |

Inventory activation, OAuth handling, other channels, the five static mappings,
lookup IDs and overrides were unchanged. No new aliases or field-copying logic
were introduced. The candidate was parsed and compared against the production
backup: these five source-priority appends were its only semantic differences.

## Local validation

The repository's two configuration snapshot tests still required the old empty
chains and subscription-only Axis chain. Their expectations were updated for the
approved configuration. No runtime Go source changed.

- Initial old-config assertion failed as expected.
- First complete run exposed the second obsolete subscription-only assertion;
  its failure log was retained, then its expectation was updated.
- Final `go test -json -count=1 ./...`: 147 passing tests/subtests, one opt-in
  `TestCatalogHTTPFixture` skip, zero failures.
- Formatting and `git diff --check` passed.
- No new race, container-chain, inference or load run is claimed for this
  configuration-only deployment.

## Production result

Host: `<tailscale-ip>`; project directory `/root/AI-gateway`.

A single post-switch directory request to
`http://127.0.0.1:9083/v1/models?client_version=v0.65.0` returned HTTP 200 and
**191 models**, 8,811,782 bytes. Its membership was identical to the preceding
saved 191-model snapshot. Both snapshots are observations, not atomic upstream
state captures.

| Field presence | Previous snapshot | After deployment |
|---|---:|---:|
| output_modalities | 17 | 78 |
| max_input_tokens | 0 | 22 |
| max_completion_tokens | 0 | 0 |
| max_output_tokens | 17 | 78 |

| Updated channel | Models | Output modalities / output limit | Input limit |
|---|---:|---:|---:|
| Axis | 11 | 9 | 9 |
| XL | 22 | 16 | 6 |
| Commandcode | 67 | 15 | 7 |
| NIM | 3 | 3 | 0 |
| Ollama | 19 | 18 | 0 |

ShuaiAPI's 13 and Zcode's four output-field records remain present. GMI produced
its known channel-fetch failure and CPA fallback; its configuration was not
changed or represented as repaired.

Examples from the response:

- `axis/gpt-5.6-sol`: context 1050000, input 922000, output 128000, output
  modalities `["text"]`.
- `xl/gpt-5.6-sol`: the same added declarations; its existing `max_tokens=128000`
  also remains. Equal numbers are independent declarations, not new aliasing.
- `commandcode/gpt-5.5`: context 1050000, input 922000, output 128000.
- `nim/minimaxai/minimax-m3`: context 1000000, output 16384.
- `ollama-cloud/glm-5.2`: native context 1048576 remains higher priority;
  models.dev supplies output 131072 and output modality `["text"]`.

These are source-catalog declarations, not measured inference capacities.
Same-key source priority can change existing context or other metadata; it is
not a four-column-only fill. Missing maxima remain absent, and no input bound
was calculated by subtracting output from context.

## Runtime and artifacts

- Config SHA256: `2282345fdd4d705e248641483b338a871aa2dd4f30d74e98d9237477cc0c3aee`.
- Mounted config matched this hash.
- Image unchanged: `localhost/models-enricher:output-fields-20260908T045911Z`.
- Docker image ID unchanged:
  `sha256:902d7b8a05be0e171c7f76ca904275ced97bf0e7f6c936d939fd416723d35d2a`.
- New Go container:
  `b14f31b0481881d480963f104efab8f709ecc14bf0040cc66640c90346066f52`.
- Response SHA256:
  `43689ad8e72c5db630d714c2b0aa0e900f78f6dcfedc37386fc106a296c331d0`.

All 48 containers were running and all nine declared health checks healthy.
The other 47 IDs and restart counts were unchanged; Go restarted zero times
following recreation. Go HostConfig and runtime environment matched baseline.
No inspected container had an OOM flag; this is not a historical host OOM audit.

Effective Compose was identical before/after. `.env`, Compose and its override,
Go image, resource settings and the notice-free table HTML were unchanged.
Production `pull_policy: never` remains intact. No front authorization,
production-browser, inference or sustained-load test was added.

## Evidence and rollback

Local: `/root/.cache/catalog-source-chain-deploy-20260908/`.
Production: `/root/AI-gateway/.source-chain-deploy-20260908/`.

The production `config.before.yaml` is the rollback file, with SHA256
`e0bfd4d5dc14a7e8d67ec0a8735e9358ab4d5fc7718bfc257572885113ce45c4`.
If rollback is required, manually restore only that configuration with its
original mode/ownership and recreate only `models-enricher` using
`--no-deps --no-build --pull never --force-recreate`. Do not replace Compose,
change images or restore databases.

No commit or archive was performed. Verification stopped after the bounded
production pass; backups and failure evidence were retained.
