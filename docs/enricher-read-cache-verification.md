# Enricher read cache and channel fallback: local verification

Local verification record from 2026-09-07, before deployment. The configuration/activation follow-up below is separate from the earlier HTTP/resource fixture. The subsequent manual production deployment and actual checks are recorded in [the deployment report](enricher-read-cache-deployment-2026-09-07.md); Rust was not changed.

## Behavior checked

- `apisix-models`: nonempty `client_version` on `GET /v1/models` selects Go; missing/empty values pass to CPA. Front-door authorization/intersection behavior is unchanged.
- APISIX does not retain final enriched catalogs. Sequential requests with the same version both reach Go; only overlapping same-version builds share a result.
- Go successful input reads are fresh for five minutes. Eligible transient failures can reuse retained successful input bytes without a maximum stale age; failure does not renew freshness. Cache capacity/eviction and process lifetime still bound retention.
- An unconfigured channel returns its CPA baseline without Go enrichment. An explicit `channels.<prefix>: {}` defaults to fetching that channel's own inventory. No explicit source chain means no external metadata requests. Go no longer has a `skip_channels` setting; Rust synchronization policy is independent.
- `fetch_models: false` disables channel inventory only. Explicit sources use CPA membership; no effective source chain returns the exact CPA baseline. A model-level `source_priority: []` disables the inherited chain.
- A required inventory/source/Ollama failure without a usable cached read returns the affected channel's complete CPA records, including nulls, unknown fields and precise numbers. Successful caches and unrelated channel enrichment survive. Overrides/static replacements cannot re-enrich a failed prefix.
- Shared sources affect only channels whose actual members depend on them. A metadata lookup miss after a successful source read is not a transport failure.
- No usable CPA native baseline still fails the whole request. Configuration conflicts remain separate from upstream outages.

## Tests

From `models-enricher/`:

```sh
go test -race ./...
go vet ./...
```

Both passed after the changes. HTTP integration is opt-in and was run separately using `TestCatalogHTTPFixture` in isolated, network-disabled Podman fixtures.

Focused regressions include:

- `TestEnabledEnrichmentSteps`: default/unconfigured channels, disabled fetch, explicit sources only, unused model chains, explicit empty model chain, enabled-step failures, and request-count assertions.
- `TestFetchModelsConfigDefaultAndFalse`: YAML missing/true/false semantics.
- `TestManagedFetchFailureReturnsPureCPAMember`: no continued source/override layering after inventory failure.
- `TestSharedSourceFailureRollsBackOnlyDependentChannels`: two dependent channels return exact baseline; unrelated channel enriches; successful source is fetched only once across repeated failures.
- `TestOneOllamaModelFailureRollsBackWholeChannelAndRetainsSuccess`: one missing model degrades its whole channel, not another channel; successful model responses remain cached.
- `TestUsableStaleSourceStillEnrichesChannel`: a year-old retained response after transient failure does not incorrectly trigger baseline-only fallback.
- `read_cache*_test.go`: freshness, failure classes, private request identity/files, cancellation/singleflight, entry/byte limits, eviction, restart non-reuse and inner API-call statuses/read-only methods.

The new shared-source regression was first observed failing under the old skip-and-continue behavior. Old assertions requiring enrichment after inventory failure or requiring every channel to configure a source chain were updated because the user changed those contracts. An attempted blanket rule treating every undiscovered native prefix as failed broke existing static metadata fixtures; it was removed rather than changing those fixtures.

`bash -n scripts/start-apisix-models.sh` and scoped `git diff --check` passed. ShellCheck was not available; it was not installed. The HTTP fixture runs APISIX's real runtime but does not prove production template rendering, live credential forwarding, inference or WebSocket behavior.

## Configuration and activation follow-up

The current `models-enricher/config.yaml` has no `custom_channels`, no per-model overrides, and no Go skip list:

| Channel | Own inventory | External sources |
| --- | --- | --- |
| Axis | Off | OpenAI subscription only |
| XL | On | OpenAI, xAI, Z-ai, Moonshot subscription only |
| Zcode | On | `models.dev/zai-coding-plan`, then `modelparams.dev/z-ai/subscription` |
| Ollama-cloud | On | `ollama_cloud` only |
| ShuaiAPI | On | `models.dev/anthropic`, then Anthropic subscription |
| NIM, Commandcode, GMI | On | None; each is explicitly configured as `{}` |

The Zcode namespaces follow the [models.dev coding-plan provider](https://models.dev/providers/zai-coding-plan/) and [modelparams Z-ai provider](https://modelparams.dev/providers/z-ai). Lookup remains exact; no reseller alias bindings are added.

`TestConfiguredSourcesAndFiveStaticModels` checks this source policy, absence of hidden pools/model overrides, and exactly five same-name static mappings. Its synthetic merge also proves CPA bare records do not supply stray fields to those aliases. It does **not** establish that all real parents exist.

A separate authenticated, read-only CPA native snapshot (`client_version=v0.65.0`, HTTP 200, 313 records) found:

| Static slug | Explicit parent | Present in this snapshot |
| --- | --- | --- |
| `gpt-5.6-sol` | `codex/gpt-5.6-sol` | Yes |
| `gpt-5.6-terra` | `codex/gpt-5.6-terra` | No |
| `gpt-5.6-luna` | `codex/gpt-5.6-luna` | No |
| `gpt-6-astra` | `codex/gpt-6-astra` | No |
| `grok-4.7` | `supergrok/grok-4.7` | No |

All five configured IDs remain unchanged. Four aliases therefore lack their designated parent metadata in this snapshot; no alternative channel, ID variant or invented capability is substituted. A present parent does not establish complete metadata or successful inference. These checks do not change inference routes or user entitlements.

The old Commandcode `/v1/models` egress-403 comment is not current evidence. A bounded recent Squid log sample showed nine successful directory requests. A separate one-shot request reproduced Go's URL/auth-index/header rules through the existing CPA relay and `api-call`: the configured base path yields **`/provider/v1/models`**, which returned upstream **200 and 67 models**. No current failure was reproduced, so no egress permission or request implementation was changed. This is a read-path check, not a deployed-candidate catalog test.

Final `go test -race ./...` and `go vet ./...` passed. `TestEnabledEnrichmentSteps` now distinguishes an absent channel entry from an explicit `{}` entry, and checks request counts and exact CPA fallback. The former requirement for at least 90 mappings was removed from deployment-config tests: independent, bounded provider fixtures retain exact-lookup, inheritance and zero/false/empty-value coverage without restoring the deleted mappings to the real configuration.

## Original-budget fixture (before the follow-up)

The following fixture and binaries predate the configuration/activation follow-up. It was not rerun for this cleanup and does not certify the new complete configuration under production load. The production limits were not increased. The same HTTP fixture was run with separate component cgroups, the newly built Go executable mounted read-only, and the fixture's external-APISIX mode:

| Component | Limit | Kernel memory peak |
| --- | ---: | ---: |
| Go enricher, including 64MiB `/tmp` tmpfs | 256MiB | 110.21MiB |
| Sidecar nginx | 32MiB | 21.82MiB |
| Catalog APISIX | 128MiB | 68.89MiB |
| Front APISIX | 128MiB | 122.77MiB |

All four cgroups reported `oom 0`, `oom_kill 0`, and `max 0`. The fixture completed successfully with 251 exact records / 11,076,924 bytes and two concurrent admitted requests. Kernel peak/cumulative event counters were captured in three samples while component containers remained running. The test-data helper had a separate 1GiB budget; it is not an application component. A separate combined 1GiB functional run checked the local enricher request counter; that combined run is **not** the per-component resource gate.

This is one bounded fixture pass, not a sustained-load guarantee. Front APISIX had only about 5.2MiB headroom. Earlier 128MiB OOM evidence remains valid for those earlier runs; this pass does not erase it. Different client versions can still build concurrently; the read cache is not a global catalog-build admission limit. Maximum-size simultaneous upstream responses and full cache churn were not load-tested here.

## Evidence

Local private evidence root: `/root/.cache/enricher-read-cache-20260907/`.

- `channel-steps-race-v2.log`, `channel-steps-vet.log`
- `http-functional-steps.log`
- `http-budget-128.py`, `http-budget-128.log`, `channel-budget-128/result.json` and per-component logs
- Earlier failing test/HTTP logs are retained separately, not overwritten with passing output.
- Follow-up: `simplified-config-focused.log` (including obsolete mapping assertions), `simplified-config-race-first.log`, `simplified-config-race-final.log`, `simplified-config-vet.log`
- `simplified-config-live-readback.json`: sanitized Commandcode status/count and exact static-parent presence; no credentials or response bodies.

Tested fixture binaries:

- Go: SHA-256 `618c8d3edd4e6c1e3b868f02d724231977162470c807163f688b1e4f803ae364`
- HTTP test executable: SHA-256 `15c37edcca4baf36f7c18f9c53c0032918abe80cf5ae4491e689f87d25799e36`

These are local fixture artifacts, not a published or deployed release candidate. Any later production rollout needs a fresh exact artifact/config review and individual manual deployment commands. The follow-up includes the explicit Axis/source configuration above, but no alias-route or WebSocket migration. It was subsequently deployed as recorded in the linked deployment report. The four missing parents describe that earlier snapshot; the later [CPA-only upgrade](cpa-v7.2.153-deployment-2026-09-07.md) restored the Codex parent IDs to the page-data catalog.
