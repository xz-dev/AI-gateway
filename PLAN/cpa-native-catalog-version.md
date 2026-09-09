# Fixed CPA native catalog version

## Decision and scope

The user explicitly requested that Go always request the CPA native directory
with `client_version=1`, rather than allowing caller versions to select CPA's
legacy metadata filtering. The initial authorization was local-only; the user
subsequently explicitly requested deployment followed by further data completion.

CPA v7.2.153 (`934fb7928c42a8dd0aeaf39a321bef6601b55eb6`) filters `max/ultra`
below client version 0.144.0. Its dotted-version parser treats `1` as 1.0.0.
The saved CPA native and enriched Codex reasoning lists agree; the original
table's v0.65.0 request triggers that filter. This does not establish inference
capability and is not a bypass of all CPA logic.

- One `cpaCatalogClientVersion = "1"` constant controls the native request and
  the table caption. `NativeManifest` no longer accepts a caller version.
- Authentication, positive membership admission, source config, independent
  token fields, and missing/empty-version routing remain unchanged.
- Other upstream inventory requests retain JSON callers' versions. The table's
  inventory version stays v0.65.0; only its CPA native directory version changes.
- No XL/Commandcode source additions, OAuth synthesis, Core changes or production
  actions are included. Old callers can now receive declared `max/ultra` values.

## Verification

- Behavioral red: requests tagged v0.65.0, v0.144.0 and arbitrary did not select
  the required CPA version 1; the version-1 case passed before the change.
- Focused green: all caller versions receive the same full CPA declaration;
  unsupported levels are not invented for a second model. Native credentials,
  membership, other-inventory versions and table behavior are checked.
- Bounded race regression: 154 tests/subtests passed, two existing opt-in tests
  skipped, and one old native-URL assertion still expected `fixture`. After
  changing that assertion to the required literal `1`, its race recheck passed:
  155 passing tests/subtests across these commands. `go vet ./...` and
  `git diff --check` pass. Only three runtime files differ from the deployed
  SSR build source; the source configuration SHA256 remains
  `a3ca2b0dcc3145da10cff7809e263b82b6893f51ee66cf8ca4a65d39c1c11d66`.

Evidence: `/root/.cache/codex-native-version-1-20260908/`.

The pre-existing opt-in HTTP/container fixture uses client versions as synthetic
native-response and cache-namespace selectors. That mechanism no longer models
this fixed-version boundary; it has not been adapted or run in this local slice.
No assertions were weakened and no new skip was added to make it pass. A future
use of that fixture needs explicit source-state/cache-expiry control rather than
using the caller version to force a different native response. No new fixture
framework, browser run or inference test was added. The subsequently authorized
production step used the single catalog acceptance recorded below.

## Delivery

The explicitly authorized manual Go-only deployment is complete.

- Image: `localhost/models-enricher:native-version-1-20260908T084641Z`.
- Executable SHA256: `1e2dc5003ba3e7a0862b47ca6a9af7dc4ceac01f27c39784214a34c2fd6b824c`;
  the running container matches the locally built binary.
- Only the image reference changed in effective Compose. All three existing
  `pull_policy: never` entries and all other configuration hashes are unchanged.
- One catalog request through the existing sidecar/APISIX path, using caller
  version v0.65.0, returned 189 models. Of nine Codex records, four declare
  `max`; none declare `ultra`. No inference capability was tested or claimed.
- The first connection attempt used port 80 incorrectly and did not reach HTTP;
  the checked-in template specifies 8080. The corrected request succeeded.
- The other 47 container identities, images, start times and restart counters
  are unchanged. All 48 containers run; nine report healthy. The new Go container
  is running without an OOM-killed flag. The task deployment lock is released.
- Private production evidence/backups:
  `/root/AI-gateway/.codex-native-version-1-20260908/`.

No new data sources were included in this deployment.
