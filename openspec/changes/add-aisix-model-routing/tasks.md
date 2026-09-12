## 1. Baseline and production configuration

- [x] 1.1 Inventory current production routes, the full selected account state, active sessions, capacity headroom, and the CPA model-router plugin export (12 aliases); verify a redacted mapping of logical names to concrete targets and approved ordering/cooldown policy is recorded and the private rollback snapshot is restorable.
- [x] 1.2 Generate the production AISIX `resources.yaml` from the approved mapping (one CPA provider pool, direct models per concrete target with opt-in cooldown, routing models per logical name) and private env references; verify config parsing, schema shape against the fixture-proven gotcha list, and that tracked repo files contain no production secrets.

## 2. Additive candidate deployment

- [x] 2.1 Present the exact candidate objects (AISIX container, dedicated relays, three directed network pairs, private files) and their addresses/aliases; after deployment approval, deploy additively without stopping or recreating existing owners, and record the Compose hash drift.
- [x] 2.2 Verify isolation and bounds: no default route, denied unintended egress, management on private binding only, memory within fresh host headroom, and the old production path untouched (container IDs, health, inference smoke).
- [x] 2.3 Configure the 12 logical models from the approved mapping; verify redacted read-back, target coverage, and a real-CPA smoke through the relays for at least one failover alias.

## 3. Isolated acceptance

- [x] 3.1 Create an isolated Sub2API test account/group/key in `http_bridge` mode pointing at the AISIX entry; verify normal production selection cannot choose it and existing accounts are unchanged.
- [x] 3.2 Run the acceptance matrix against real CPA: chat + non-stream Responses, streaming Responses SSE event sequence verbatim, model rewrite only, cooldown isolation (A-429 -> B same request, next request skips A, unrelated model unaffected), Retry-After honored and clamped, all-cooling fast-fail; record evidence with bounded timeouts and secret-safe clients.
- [x] 3.3 Adapt the models-enricher optional source from New API naming to AISIX (same difference algorithm, disabled by default); verify with `go test -race ./...` plus focused fixture tests, then optionally activate and verify entitlement intersection, exact difference, and optional-outage resilience through the real catalog path.
- [x] 3.4 Present the acceptance evidence, fresh capacity measurements, exact production mutation, observation window, and stop thresholds; obtain explicit cutover approval or leave production unchanged.

## 4. Reversible cutover

- [x] 4.1 Capture fresh complete rollback state and migrate external accounts 4 "jiaxin" and 5 "旋律" first; verify safe read-back, unrelated fields preserved, request-path propagation to AISIX, and E2E generation.
- [x] 4.2 Apply the operator-approved full migration: integrate AISIX under the main Compose project with three direct internal edge networks and private OSS admin UI; deploy the catalog supplement; migrate every CPA-backed account; verify real client keys, Responses, routing, catalog, bindings, and zero routed traffic through the old router edge; repair or roll back on failure.
- [x] 4.3 Remove candidate account/group/key, standalone candidate project, AISIX test/candidate files, and the superseded model-router edge after verification; deliver the final redacted active-routing, rollback, UI, resource, and retained-component record.

## 5. Page-only AISIX operator status

- [x] 5.1 Implement the minimal independently versioned server-rendered status service over fixed AISIX Admin API paths; verify focused HTTP tests cover configured target order, deterministic failover candidate, cooldown/unavailable details, HTML escaping, generic upstream failure, no JavaScript, no secret output, and rejection of every non-status path.
- [x] 5.2 Add the dedicated internal management network and status relay, move the AISIX Admin listener off all host/Tailscale publications, and update initialization and validation without changing the three direct data networks; verify the base Compose render has page-only private binding, internal-only Admin reachability, no default-route regression, pinned images, resource limits, and least privilege.
- [x] 5.3 After explicit deletion approval, remove the unshipped AISIX status-page source patch and its image wiring while retaining only the pinned upstream deadlock fix; verify the exact patch stack applies cleanly, focused deadlock regression passes, and the AISIX release build succeeds without local UI code.
- [x] 5.4 Run an isolated Docker acceptance against the exact AISIX and status images; verify `/status` renders complete HTML with correct live exclusions, Admin/Scalar/playground/metrics/inference paths are not exposed on the page listener, internal authenticated model/status reads work, secret markers are absent, and AISIX inference/readiness remains unchanged.
- [x] 5.5 Perform the bounded production management cutover without changing routing resources or account configuration; verify the page through loopback and `100.94.238.35:3001/status`, no standing host route reaches the Admin API, the temporary maintenance-access procedure closes cleanly, service restart counts remain acceptable, and Docker supplies all acceptance evidence.
- [x] 5.6 Update operator documentation and redacted production evidence with final image IDs, topology, page semantics, accepted lack of authoritative last-served history, and maintenance access; verify strict OpenSpec validation and repository validators pass without exposing credentials or overwriting unrelated production changes.
