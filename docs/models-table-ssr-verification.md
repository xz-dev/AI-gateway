# Pure server-rendered models-table

Local implementation and validation are complete. The subsequently approved
manual production cutover is recorded in [the deployment handoff](models-table-ssr-deployment-2026-09-08.md).

## Behavior and boundary

`GET /models-table` now uses the existing Go catalog handler and its in-flight
result, then renders the eleven-column document through Go `html/template`.
The embedded template is `models-enricher/models-table.html`. There is no page
JavaScript, hydration, browser JSON fetch, new service, or new dependency.
The source chain, input cache and JSON representation are unchanged. A later
change fixes the CPA native catalog request and the table caption to
`client_version=1`, independent of the caller's version. The table's non-CPA
inventory requests still use `v0.65.0`. This avoids CPA's legacy reasoning-level
filter; it does not bypass authentication or membership admission. No final HTML
cache was introduced. The override was deployed separately in a Go-only cutover;
see [the version change record](../PLAN/cpa-native-catalog-version.md).

The sidecar proxies the page to `apisix-models`. Its new HTML route matches only
`realip_remote_addr=172.30.42.3`, the sidecar's existing connection address;
forwarded headers cannot select that route. Other callers retain the previous
catch-all behavior. Networks, ports, credentials and resource budgets are not
changed. The unused static-HTML mount is removed from the local Compose service.

Missing, null, false, zero, empty string and empty array remain distinct.
Output token columns remain independent. Slugs are sorted, rows counted, and
model data is rendered as text; no explanatory page notice was added. The
existing number-preserving decoder also keeps `9007199254740993` exact in HTML.
Catalog HTTP/transport failures return error HTML with HTTP 502, not an empty
successful table.

## Checks

- `TestModelsTableSSR`: before implementation, HTTP 400 JSON rather than rendered
  HTML (behavioral red). After implementation, complete HTTP HTML, JSON parity,
  and catalog HTTP/transport error HTML pass.
- Initial fixture integration used bare source IDs and hit the existing positive
  admission filter. The fixture was corrected to registered `oauth/` IDs;
  admission policy was not weakened to satisfy the page test.
- `go test -race -json -count=1 ./...`: 148 passing tests/subtests, one opt-in
  container test skipped, zero failures. `go vet ./...` and compilation pass.
- The skipped `TestCatalogHTTPFixture` was then run explicitly in a task-scoped,
  network-none cached APISIX 3.18 container: PASS. It exercises the actual route
  and sidecar configuration, 251 server-rendered rows from an 11,076,924-byte
  catalog, non-sidecar/forged-header rejection, existing JSON fidelity,
  entitlement-first authorization, response limits and path restrictions.
- The existing `testdata/models-table-check.js` runs on captured Go HTTP HTML
  with browser JavaScript disabled: eleven rows/columns, sorted IDs, all six
  value states, separate token values, exact large integer text, escaped hostile
  model content, one-pixel borders and both error pages pass. Zero script
  elements and zero browser `/v1/models` requests.
- Browser fixture files were placed in the tool's allowed project artifact root
  after an initial outside-root file rejection. This was a fixture-path issue,
  not a page/runtime failure.

The browser uses captured HTTP responses; it is not a production DOM result.
The combined Go/fake-upstream/APISIX test container had a 1 GiB allowance, not
separate production service limits. No production-budget load, inference,
independent review or CI result is claimed. The container was automatically
removed and the temporary browser context closed.

## Reproduce

From `AI-gateway`, choose an empty evidence directory:

```sh
repo="$PWD"
evidence="$(mktemp -d)"
(
  cd "$repo/models-enricher"
  MODELS_TABLE_FIXTURE_OUTPUT="$evidence" \
    GOPROXY=off GOTOOLCHAIN=local go test -race -count=1 ./...
  GOPROXY=off GOTOOLCHAIN=local go vet ./...
  CGO_ENABLED=0 GOPROXY=off GOTOOLCHAIN=local \
    go test -c -o "$evidence/catalog-http-fixture"
)
podman run --rm --network none --pull=never --memory=1g --memory-swap=1g \
  --pids-limit=128 -v "$repo:/work:ro" \
  -v "$evidence/catalog-http-fixture:/fixture:ro" \
  -e CATALOG_HTTP_FIXTURE_ROOT=/work --entrypoint /fixture \
  docker.io/apache/apisix:3.18.0-debian \
  -test.run '^TestCatalogHTTPFixture$' -test.v -test.timeout=90s
```

For the existing Playwright tool, create a temporary context with
`javaScriptEnabled: false`. Within that context, intercept only
`http://catalog-fixture.test/**`: serve `/models-table` from `table.html` (200),
`/table-error` from `table-error.html` (502), and `/table-network-error` from
`table-network-error.html` (502), all as `text/html`. Abort other requests.
Navigate to `/models-table`, run the checked-in checker on that page, and close
the temporary context in `finally`. Place fixture files inside the tool's
allowed root; use `route.fulfill({path: absolutePath})`, not Node imports or a
second rendering implementation.

## Handoff

Evidence: `/root/.cache/catalog-table-ssr-20260908/` (baseline, red and fixture
failure logs, regression JSONL, actual HTML, browser wrapper, offline HTTP log).
Browser artifacts: `/root/cpa-plugin-mono/.playwright-mcp/table-ssr/`.

Production deployment was separately approved after these local checks. The
[deployment handoff](models-table-ssr-deployment-2026-09-08.md) records its actual
scope and result. The prepared minimal cutover uses
an image reconstructed from the deployed Go sources plus the reviewed SSR
changes, the added APISIX route, and the sidecar proxy configuration. Recreate
Go, directory APISIX, then sidecar without dependencies/build/pull; preserve
production `pull_policy: never`, the latest source-chain config and all unrelated
routes. Do not deploy the dirty workspace Compose or route files wholesale.
No production operation was part of the local validation itself. No commit,
OpenSpec synchronization or archive was performed.
