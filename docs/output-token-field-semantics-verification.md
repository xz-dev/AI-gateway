# Independent output-token fields: local verification

## Scope and compatibility

The local Go implementation preserves `max_tokens`, `max_completion_tokens` and
`max_output_tokens` independently. It no longer synthesizes sibling aliases or
uses one key to overwrite another. Existing same-key source priority, recursive
objects, atomic arrays and null-as-no-update layer behavior are unchanged.
OAuth/failure-preserved CPA records retain their separate exact-value contract.

Explicit models.dev `limit.output`, Gemini `outputTokenLimit` and supported
`top_provider.max_completion_tokens` structures still map to `max_output_tokens`.
A non-null destination, including zero, wins within its source record.
modelparams maps each explicit `range.max` to that parameter's own name; defaults
and minima do not become maxima. None of these mappings proves a universal model
capacity or a unified OpenAI Models schema.

The actual diagnostic page has three adjacent output columns:
`max_tokens(abandon)`, `max_completion_tokens`, `max_output_tokens`. The first
label refers only to the deprecated Chat Completions request parameter. Values
are displayed as supplied by the catalog, without cross-field fallback.

The local validation below preceded [production deployment on 2026-09-08](output-token-field-deployment-2026-09-08.md). At the user's request, the explanatory paragraph was removed from the page; semantics remain documented here.

No Pi/client, source configuration,
identity/admission, authorization, routing, cache, resource or database change
is part of this implementation. The separate production Compose startup repair
is not evidence that these Go/UI changes are running in production.

### Known consumer impact

The configured Pi extension source at
`/root/.pi/agent/git/github.com/xz-dev/openai-api-pi-extension/index.ts:158–186`
selects finite positive `max_output_tokens`, then `max_completion_tokens`, then
`max_tokens`, otherwise its resolved context limit. The inspected checkout was
`2b8edc426a6a6e272999fbfa4f560e7f94440972`. No client code was changed.

A controlled input with `max_tokens=4096`, `max_completion_tokens=16384` and no
`max_output_tokens` illustrates a compatibility change: old Go synthesized
`max_output_tokens=4096`, so Pi selected 4096; without that synthesis, unchanged
Pi selects 16384. This is a source-derived example, not a production failure,
physical-capacity assertion or executed inference test. The user requested no
speculative merging or compatibility layer and continued with Pi unchanged.

Other clients and already-loaded Pi processes remain unverified. Do not restore
Go alias synthesis solely to preserve a hypothetical client's selection.

## Acceptance example index

All named Go tests are in `models-enricher/` and use local fixtures only.

| Example | Observable check | Result |
|---|---|---|
| A1: unequal values | `TestHandlerOutputTokenFields/distinct`; actual browser columns | 4096 / 16384 / 8192 retained |
| A2: legacy only | `TestHandlerOutputTokenFields/legacy-only`, `TestGeminiLegacyOutputField`; browser | 128000 / unknown / unknown |
| A3: exact passthrough | `TestHandlerOutputTokenPassthroughAndInheritance`, `TestSharedSourceFailureRollsBackOnlyDependentChannels`; independent JSON parsing | null, zero, missing key and integer 9007199254740993 preserved |
| A4: same-key null | `TestHandlerOutputTokenFields/same-key-null`, `TestSourceNullKeepsNativeAndChannelMetadata` | null does not erase a lower value or unify siblings |
| A5: explicit structure | `TestStructuredOutputFields` (three sources × missing/null/zero destination) | only the destination is generated; existing zero wins |
| A6: parameter identity | `TestModelparamsOutputFields/distinct` and `/reversed` | separate maxima independent of parameter order |
| A7: no guessed maximum | `TestModelparamsOutputFields/no-maximum`, `/zero`, `/declared-destination` | defaults/minima/null do not produce maxima; explicit zero retained |
| A8: inheritance | `TestHandlerOutputTokenPassthroughAndInheritance`, `TestStaticGenericInheritance` | OAuth parent unchanged; static child does not invent aliases |
| A9: layered priority | `TestReferencesReadFixedSnapshot`, `TestOverrideFormsShareLayerSemantics`, `TestSourceFetchOrderDoesNotChangeMetadata` | same-key precedence and snapshot semantics retained |

The browser used the actual `models-table.html` served by a task-scoped Python
HTTP server bound to loopback, first with four synthetic rows and then with five
rows emitted by the local Go handler. It checked actual DOM columns and cells,
including the static child, against the same fetched JSON. It also checked one
same-origin versioned request, sorted membership, count, null/zero/false/empty
values, separate modalities and HTML-text safety. An injected HTTP 503 retained
the existing error display. No production directory was requested.

The existing repository fixtures `models-enricher/testdata/models-table.json`
and `models-table-check.js` were also updated and run: eleven rows, eleven
columns, independent output values, six explicit/missing states, directional
modalities, hostile text, 1px borders, and both HTTP and network errors passed.
For repeatable checks, intercept `http://catalog-fixture.test/**`, fulfill the
page with `route.fulfill({path: <absolute HTML path>})` and the versioned models
request with the JSON fixture path, then run the checker using Playwright's
`filename` argument. This reuses the existing checker without Node imports or
a new test framework.

Browser `Number` is **not** the precision oracle for large integers. The HTTP
test uses the Go number-preserving decoder, and an independent Python JSON check
verified 9007199254740993 in the saved handler response.

## Commands and evidence

Run from `models-enricher/`:

```sh
go test -run '^TestHandlerOutputTokenFields$' -v -count=1 .
go test -run '^TestModelparamsOutputFields$' -v -count=1 .
go test -run '^Test(StructuredOutputFields|GeminiLegacyOutputField)$' -v -count=1 .
go test -race -json -count=1 ./...
go vet ./...
gofmt -l ./*.go
```

Recorded result: **147 tests/subtests passed**, one opt-in container test skipped;
no race findings. Vet, format and scoped diff checks passed.
`TestCatalogHTTPFixture` was skipped because `CATALOG_HTTP_FIXTURE_ROOT` was not set;
the extra-container APISIX/sidecar chain was not exercised in this run. CI was
not run. These local results do not establish production inference, sustained
load or all-client compatibility.

Local evidence directory: `/root/.cache/output-token-implementation/`.

- `go-fields-red.log` / `go-fields-green.log`: actual HTTP behavioral red → green.
- `modelparams-red.log` / `modelparams-green.log`: independent parameters and zero
  failed under the old fold; no-maximum already passed and is not called new red.
- `structured-red.log` / `structured-green.log`: explicit mappings passed;
  the remaining Gemini legacy fallback failed and was removed.
- `inheritance-mutant.log`: restoring the old merge implementation in an isolated
  copy makes the new HTTP static-child assertion fail for an invented alias.
  This is mutation evidence, not a retrospective claim that A8 was test-first.
- `regression-before-expectations.jsonl`: failures in old alias-equality assertions;
  only expectations directly contradicted by the new contract were changed.
- `race.log`, `vet.log`, `handler-catalog.json`, `baseline.json`.

Browser helper and captured DOM:
`/root/cpa-plugin-mono/.playwright-mcp/output-tokens/check-table.js` and
`handler-page.md`. The helper's loopback port is task-specific; start
`table-server.py` from the evidence directory and update that port to repeat the
check through Playwright. The server reads the actual repository HTML and the
saved handler JSON. Earlier tool sandbox/path setup failures were not behavioral
red evidence.

## Remaining boundaries

Missing source data, source failures and absent static parents remain missing;
no extra sources, guessed bindings or defaults were introduced. The browser
checks are local acceptance evidence, not a new installed testing framework.
The dirty checkout's unrelated changes were preserved. No commit or archive was performed. Production deployment has its separate
receipt linked above; final product acceptance remains with the user.
