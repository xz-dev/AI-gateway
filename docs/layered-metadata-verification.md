# Layered metadata: local verification

Scope: `simplify-layered-model-metadata`, including the explicitly authorized minimal front-intersection JSON fidelity correction. This report describes local implementation and fixtures, **not deployment, live inference, or an independent review**. The attempted subagent could not start; the checks below were run by the implementing agent.

## Behavior and evidence

- The shared structural merge recursively overlays objects, atomically replaces arrays, ignores null object properties, and preserves explicit false/zero/empty values. Public unknown fields remain ordinary metadata.
- Layer order is native → channel → reversed source chain → fixed-snapshot metadata reference → target overrides. Custom records remain hidden; static inheritance keeps earlier-entry priority and replaces a same-slug record without duplication.
- Adapters map declarations rather than invent capabilities. Native 272000 context and GPT-6 output limits survive without model-name exemptions. Request defaults do not become maxima; a reasoning boolean does not create effort choices.
- The front validates the decoded catalog and routing identities, but assembles authorized records from their original JSON text. Its scanner only finds spans in already-validated JSON; it is not a separate parser or capability interpreter. Duplicate collection/identity keys fail closed so different parsers cannot disagree about routing identity.
- The outbound pool keeps its existing configured concurrency until body close. Its original header-only lease release and silently truncating `LimitReader` behavior were reproduced by `TestHTTPPoolHoldsSlotUntilBodyClose` and `TestPublicMetadataResponseBounds`, then fixed without raising limits.

Observed results:

| Check | Evidence |
| --- | --- |
| Front contract | The same `model_list_intersection_test.lua` first failed with changed numeric values, then passed all seven checks, including identity ambiguity, auth-first ordering, ETag/304 and byte/concurrency limits. |
| Actual HTTP chain | `TestCatalogHTTPFixture`: real Go enricher → real APISIX proxy-cache **HIT** → nginx sidecar → front APISIX. 3,146,263 original bytes; 3,146,202 filtered bytes. Checks a >3 MiB field, exact numeric values **and JSON number types**, modalities, unauthorized cold requests, native failure, 16 MiB front limit, missing version, page and restricted sidecar paths. |
| Browser DOM | `testdata/models-table.json` and `testdata/models-table-check.js`: 11 sorted rows, nine columns, missing/null/empty/false/zero states, image-input versus image/audio/video-output, text-only hostile markup, 1px borders, HTTP/network errors, and one same-origin versioned catalog request. |
| Existing Pi behavior | `node --test tests/map-models.test.ts tests/provider-lifecycle.test.ts`: **27 passed**, zero failures. No plugin edits, online refresh or inference. |
| Go regression | `go test -race -count=1 ./...` and `go vet ./...`; the container-only HTTP test is skipped in ordinary runs and executed separately below. |

The HTTP fixture runs with `--network none --pull=never`. CPA, source catalogs, entitlement responses and DNS are local fixtures. Front and catalog routes are isolated on two ports of one APISIX instance; the sidecar uses the existing template with test-only loopback/file-path substitutions. This does not claim a production container/network-topology test. The browser uses intercepted fixture HTTP rather than the container network; its page bytes are the actual checked-in HTML.

## Reproduce

From the gateway repository root, using installed tools and the already-cached APISIX image:

```sh
repo="$PWD"
evidence="$(mktemp -d)"
(
  cd "$repo/models-enricher"
  GOTOOLCHAIN=local GOPROXY=off go test -race -count=1 ./...
  GOTOOLCHAIN=local GOPROXY=off go vet ./...
  CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off go test -c -o "$evidence/catalog-http-fixture"
)
podman run --rm --network none --pull=never \
  -v "$repo/apisix/lua:/fixtures:ro" \
  --entrypoint /usr/local/openresty/bin/resty \
  docker.io/apache/apisix:3.18.0-debian \
  /fixtures/model_list_intersection_test.lua
podman run --rm --network none --pull=never \
  -v "$repo:/work:ro" \
  -v "$evidence/catalog-http-fixture:/fixture:ro" \
  -e CATALOG_HTTP_FIXTURE_ROOT=/work \
  --entrypoint /fixture docker.io/apache/apisix:3.18.0-debian \
  -test.run '^TestCatalogHTTPFixture$' -test.v -test.timeout=60s
```

For the existing Playwright tool, intercept only `http://catalog-fixture.test/**`: fulfill `/models-table` with the absolute path to `model-catalog-sidecar/html/models-table.html`, and `/v1/models?client_version=v0.65.0` with `models-enricher/testdata/models-table.json` as `application/json`. Navigate to the page, then run `models-enricher/testdata/models-table-check.js` with the tool's `filename` argument. The checker returns the actual cells and request list, then exercises HTTP and transport errors. Use `route.fulfill({path: ...})`; the tool sandbox does not expose Node imports.

Apply-baseline scope audit: 11 existing files changed and eight files added, all within the authorized code/page/test/documentation paths. No baseline file is missing; protected WS and closeout files and deployment configurations retain their baseline hashes. The existing middleware gitlink is unchanged (it is not a newly added file). HEAD remains `b59a7598fcca864d62fcabe46492bd587c15b6f2`; nothing is staged.

Apply-session baseline and detailed logs: `/tmp/cpa-layered-apply.m0RRXw/` (`baseline.json`, `baseline.diff`, `http-fixture.log`, `pi-offline.log`, final check/scope reports). These temporary files supplement, rather than replace, the reusable tests.

## Delta scenario → evidence

Test names below refer to `models-enricher/*_test.go`, unless marked **DOM**, **Lua**, or **Pi**. DOM and Lua refer to the fixtures above. Each of the 45 delta scenarios has a separate row.

### enricher-source-priority — 30 scenarios

| Scenario | Local evidence |
| --- | --- |
| exact channel configuration required | `TestValidateRuntimeMissingChannelConfig`, `TestValidateRuntimeDuplicatePrefix`, `TestValidateRuntimeEmptyPrefix`, `TestHandlerConflictOnEmptyChannelPrefix` |
| provider-qualified exact lookup | `TestLookupOneExactOnly`, `TestIndexModelsDevNamespaced`, `TestIndexModelparamsAuthTypeSplitAndProviderRequired` |
| model-level whole-list replacement | `TestSourceChainTwoLevel`, `TestMergeModelLevelChainReplacesChannel` |
| source-specific lookup ID | `TestMergeLookupIDsPerSource` |
| tag remains literal without lookup override | `TestLookupOneExactOnly`, `TestMergeLookupIDsPerSource`, `TestStripExactPrefixOnly` |
| overrides always win | `TestMergeProviderChainPrecedence`, `TestOverrideFormsShareLayerSemantics` |
| source chain outranks channel capability templates | `TestNativeMetadataPreservedWithExplicitSourceOverride`, `TestMergeSourceAuthoritativeAndMetadataReference` |
| explicit input limit only | `TestAdapterRecordsReachManifest`, `TestSourceRecordsReachManifest` |
| source miss preserves channel metadata | `TestSourceNullKeepsNativeAndChannelMetadata`, `TestMergeMissingMetadataReferencePreservesCapabilities` |
| source order is independent of fetch order | `TestSourceFetchOrderDoesNotChangeMetadata` (both completion orders), `TestMergeSourceMapsPreservesFirstSourceFields` |
| custom model is hidden by default | `TestMergeCustomPoolHiddenAndStaticInherit`, `TestStaticGenericInheritance` |
| static model materializes custom data | `TestStaticGenericInheritance` |
| id@ is rejected | `TestLoadConfigRejectsIDAt` |
| missing inheritance does not erase a lower layer | `TestMergeStaticInheritPublicPoolAndMiss`, `TestStaticGenericInheritance` |
| metadata reference is one explicit layer | `TestReferencesReadFixedSnapshot`, `TestMergeMissingMetadataReferencePreservesCapabilities` |
| metadata references do not become recursive discovery | `TestReferencesReadFixedSnapshot` (A→B→C with reversed model order) |
| legacy flat overrides keep working | `TestModelOverridesLegacyMerge`, `TestOverrideFormsShareLayerSemantics` |
| null override is not a delete operation | `TestOverrideFormsShareLayerSemantics` (both forms and combined forms) |
| nested metadata is preserved | `TestLayeredMetadataFixtures` / `nested fields and modalities` |
| explicit empty values override | `TestLayeredMetadataFixtures` / `explicit empty values`; `TestYAMLMetadataLayers` |
| null never fills or clears a property | `TestLayeredMetadataFixtures` / `null does not overwrite or create` |
| arrays are atomic | `TestLayeredMetadataFixtures` / `arrays are atomic including null elements` |
| object and scalar conflicts are deterministic | `TestLayeredMetadataFixtures` / both object/type-conflict fixtures |
| lookup identity does not replace routing identity | `TestSourceRecordsReachManifest`, `TestAdapterRecordsReachManifest`, `TestStaticGenericInheritance` |
| shared source metadata remains independent | `TestSharedSourceMetadataIsIsolated`, `TestLayeredMetadataFixtures` (mutation isolation), race suite |
| modalities survive source adaptation | `TestAdapterRecordsReachManifest`, `TestSourceRecordsReachManifest`, `TestOllamaPublicRecordAndSharedLookup` |
| missing information remains missing | `TestLayeredMetadataFixtures` (exact output equality), `TestNativeNumbersAndDeclaredLimitsSurvive`, `TestSourceRecordsReachManifest` |
| a request default is not a model limit | `TestSourceRecordsReachManifest` / modelparams default-only |
| declared output aliases agree | `TestAdapterRecordsReachManifest` (canonical zero versus conflicting alias), `TestSourceFetchOrderDoesNotChangeMetadata`, `TestReferencesReadFixedSnapshot` |
| reasoning support does not invent selectable levels | `TestSourceRecordsReachManifest` (reasoning boolean, exact output equality) |

### codex-models-catalog — 15 scenarios

| Scenario | Local evidence |
| --- | --- |
| 非清单路径被拒 | `TestCatalogHTTPFixture`: management, inference and arbitrary sidecar paths close the connection |
| models-table 显示空值 | DOM: explicit null row |
| models-table 显示缺失能力 | DOM: missing row; no name/text/reasoning/limit synthesis |
| 表格渲染空值 | DOM: JSON text for arrays/objects; null literal |
| 绑定范围 | `TestCatalogHTTPFixture`: sidecar page and restricted paths; baseline scope comparison confirms deployment/bind configuration unchanged |
| 视觉对话与图片输出可区分 | DOM: vision/image/audio/video rows and nine-column assertions |
| 保留明确空值与否定值 | DOM: `未知`, `null`, `[]`, `false`, `0`, `""` are distinct |
| 模型文本不可执行 | DOM: exact hostile text, zero injected img/script/svg elements, no executed marker |
| 网关完整而 Pi 仅列对话模型 | `TestCatalogHTTPFixture`, DOM mixed inventory; Pi `mapCatalog filters non-chat models` and image-input/text-output checks |
| 合法对话模型缺 limits | Pi missing-limits mapper/lifecycle cases; no gateway defaults |
| 仅缺输出上限沿用客户端既有行为 | Pi existing output-limit fallback cases |
| 未知模型元数据穿过授权交集 | `TestCatalogHTTPFixture` plus Lua exact-body contract; numeric types checked, unauthorized record absent |
| 合并不生成跟踪包装 | Exact manifest assertions in `TestLayeredMetadataFixtures`, `TestAdapterRecordsReachManifest`, `TestSourceRecordsReachManifest`; final source/diff inspection |
| 上游自带来源描述不被删除 | `TestSourceRecordsReachManifest` retains `vendor_source` unchanged |
| 更大元数据不绕过原有边界 | `TestPublicMetadataResponseBounds`, `TestHTTPPoolHoldsSlotUntilBodyClose`, existing handler failure/coalescing/version tests, Lua limits, `TestCatalogHTTPFixture` |

## Limits and remaining work outside this change

- YAML verification covers the approved JSON-compatible nested maps/lists, null/empty values and integer `9007199254740993`. Arbitrary non-JSON YAML keys/tags and numeric values beyond the YAML decoder's native range have not been independently characterized; no broader YAML acceptance or precision guarantee is claimed here. They are follow-up characterization items, not asserted defects or a reason to add a conversion framework.
- No independent subagent review completed. No authentication workaround or model-environment repair was attempted.
- No production access, inference, new runtime dependency/service, provenance/debug endpoint, staging, commit, push, main-spec synchronization or archival was performed. Existing GPT-6/WS and closeout work is retained separately from this apply baseline.
