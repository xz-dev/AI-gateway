## ADDED Requirements

### Requirement: One end-to-end request cap lowered by provider ceilings

Every hop that enforces a request-body limit SHALL use one cap of 256 MiB (268,435,456 bytes). The cap SHALL apply to the client body and to keeper-expanded history.

The hop that selects a provider route SHALL enforce `min(256 MiB, provider ceiling)` for that route. It SHALL use a tracked ceiling table keyed by concrete route or executor identifiers, with exact byte values: Anthropic first-party 128,000,000; Bedrock 20,000,000; Google-managed 30,000,000; other partner platforms 32,000,000. Limits that do not bound the request size, such as the OpenAI combined file-input limit, SHALL NOT be enforced as request ceilings. A route whose ceiling is unknown SHALL use the 256 MiB cap and SHALL relay the provider's own 413 or error with its cause. The gateway SHALL NOT probe such routes with oversized requests.

A rejection SHALL be an explicit HTTP error with a JSON body, or a Responses `error` event after a WS turn has started. The rejection SHALL name the limit in bytes and the enforcing hop or route.

The covered set is the paths that SHALL carry 256 MiB:
- CPA codex, claude and openai-compat executors;
- AISIX native targets;
- Sub2API `/v1/responses` and `/backend-api/codex/responses`.

Every other path is a fallback path with a measured fallback limit of at most 16 MiB. `/responses/compact` is a fallback path in this change.

#### Scenario: 245 MB body succeeds end to end
- **WHEN** a client sends a single 245 MB Responses body on a covered path whose route ceiling is unknown or at least 256 MiB, and the admission gate has room for its footprint
- **THEN** every hop forwards it, and the client receives a complete response

#### Scenario: Body on a fallback path
- **WHEN** a body larger than a fallback path's recorded limit reaches that path, for example an unconverted CPA executor, an AISIX guardrail chain or `/responses/compact`
- **THEN** the hop returns 413 `request_too_large` naming the path and its limit, and it does not expand the body in memory

#### Scenario: Body above a known provider ceiling
- **WHEN** a 40,000,000-byte body targets an Anthropic route on Bedrock
- **THEN** the gateway returns 413 `request_too_large`, naming the 20,000,000-byte ceiling and the route, and sends nothing to the provider

#### Scenario: Body above the cap
- **WHEN** a body or an expanded history exceeds 256 MiB
- **THEN** the first hop that sees it rejects it explicitly with the limit, and no later hop receives it

#### Scenario: 1 GiB body in production
- **WHEN** an origin check sends `Content-Length: 1073741824` with `Expect: 100-continue` and no body to the production firewall
- **THEN** the firewall returns the JSON 413 naming the 256 MiB limit; 1 GiB is exercised only by the nightly CI run with a CI-only cap

### Requirement: Host-wide admission gate for large requests

Every request whose body or keeper-expanded history is at least 64 MiB (67,108,864 bytes) SHALL be admitted by a single host-wide gate before any downstream hop receives it. Smaller requests SHALL carry no ticket. Keeper SHALL host the gate, and SHALL authenticate the caller, failing closed, before it admits such a request. Only keeper SHALL be able to write the ledger; every other hop SHALL mount it read-only. Tickets SHALL be written atomically, and the ticket header SHALL be forwarded only to internal upstreams, never to a provider or an egress path.

At admission, the gate SHALL atomically reserve both the request's measured disk footprint across all spooling hops (from the factors recorded in the compatibility lock, not 1×) and its memory footprint, in one ledger shared by every spooling hop. It SHALL admit only if:
- the disk reservation is at most real `statfs` free space minus the 1 GiB reserve, minus the keeper store headroom, minus the reservations of live tickets;
- the memory reservation is at most real MemAvailable minus 200 MiB, minus 32 MiB, minus the reservations of live tickets.

A request that does not fit SHALL be refused immediately and explicitly, with no queueing, no soft check and no change to the thresholds:
- 503 `capacity_busy` with `Retry-After` when live tickets hold the capacity;
- otherwise 507 `insufficient_storage` or 503 `insufficient_memory`.

Every hop that handles a ticketed request SHALL hold a lease on its ticket for its whole handler lifetime. A ticket SHALL be released only after keeper's request has ended and every lease is gone, without timers. A hop SHALL refuse a body of 64 MiB or more that carries no valid ticket.

#### Scenario: Large request admitted when the host has room
- **WHEN** one 256 MiB request arrives and the host has room for its footprint
- **THEN** it is admitted, a ticket is created, and the request completes

#### Scenario: Concurrent large request refused immediately
- **WHEN** a second 256 MiB request arrives while the first one's ticket holds the capacity
- **THEN** the second request receives 503 `capacity_busy` with `Retry-After` at once, naming the required and available bytes, and no downstream hop receives it

#### Scenario: Host cannot fit the request even when idle
- **WHEN** a 256 MiB request arrives and the host lacks room for its footprint with no live tickets
- **THEN** it receives 507 `insufficient_storage` or 503 `insufficient_memory` naming the floor, and nothing is written downstream

#### Scenario: Release only after bytes are gone
- **WHEN** keeper's request ends while an inner hop still holds its spool or handler
- **THEN** the ticket stays reserved until that hop's lease is gone, and it is then released without any timer

#### Scenario: Crash does not leak a reservation
- **WHEN** keeper or an inner hop is killed during a ticketed request
- **THEN** the kernel drops the dead process's leases, the restarted keeper releases the ticket once no lease remains, and no spool file remains

#### Scenario: Small request bypasses the gate
- **WHEN** a request smaller than 64 MiB arrives
- **THEN** it carries no ticket, and on a covered path it is still bounded by the inline-bytes limit and the per-hop memory charge

#### Scenario: Unauthenticated large request
- **WHEN** an unauthenticated caller sends a 256 MiB body
- **THEN** keeper returns 401 in the caller's API shape, creates no ticket and writes no spool

#### Scenario: Inner hop cannot alter the ledger
- **WHEN** an inner hop tries to create or delete a ticket file
- **THEN** the attempt fails because its ledger mount is read-only

#### Scenario: Target request fits today's host
- **WHEN** one 256 MiB media-dominated request arrives on an idle host with 901 MiB MemAvailable and 4.34 GiB free disk
- **THEN** the gate admits it within both the disk and the memory floors

### Requirement: Inline bytes and per-hop memory are bounded for every request

Every JSON-parsing hop on a covered path SHALL limit the inline bytes left after tier-A externalization to 16 MiB for every request, and to 8 MiB for a ticketed request. Where a hop's measured skeleton multiple requires a lower per-hop value, that value SHALL be reported to the user before deployment and SHALL be applied only with the user's confirmation. Above the limit, the hop SHALL stop reading and return an explicit 413 `request_too_large` naming the limit and the hop. Each such hop SHALL also charge every request, before allocating, against its own cgroup headroom minus outstanding charges and a 32 MiB margin, and SHALL refuse at once with 503 `insufficient_memory` naming the hop when the charge does not fit. It SHALL NOT queue, and SHALL NOT change any threshold.

#### Scenario: Text-heavy body above the inline limit
- **WHEN** a 32 MiB body made of text reaches a covered path
- **THEN** the hop returns 413 `request_too_large` naming the 16 MiB inline-bytes limit, and no hop is OOM-killed

#### Scenario: Concurrent bodies exceed a hop's headroom
- **WHEN** concurrent requests would together exceed a hop's `memory.max` minus 32 MiB
- **THEN** the hop refuses the requests that do not fit with 503 `insufficient_memory`, and it is not OOM-killed

### Requirement: Memory stays flat through the skeleton and blob spool pattern

Sub2API, CPA and AISIX SHALL externalize allow-listed media and file string values (and `data:` URLs) at or above a threshold into an on-disk spool. They SHALL keep all other strings inline, run their existing JSON code on the resulting skeleton, and splice the blobs back at egress. The upstream bytes, `Content-Length`, session hash and accept/reject decision SHALL be identical to the legacy path. Every retry or fallback attempt SHALL re-read the spool. An unresolved marker or a nonce collision SHALL fail closed with an explicit error. Markers and blob bytes SHALL never reach the upstream, logs or the client; logs SHALL receive the skeleton only. A request whose skeleton exceeds the inline-bytes limit SHALL be refused with an explicit 413 naming that limit. Each patch SHALL have a config switch. Every hop's cgroup `memory.peak`, file pages included, SHALL stay at or below its `mem_limit` minus 32 MiB.

#### Scenario: Differential equivalence
- **WHEN** the shared corpus runs through the legacy mode and the spool mode of a component
- **THEN** both modes produce identical upstream bytes, `Content-Length`, session hash, status and client response, and they reject the same invalid bodies

#### Scenario: Retry re-reads the spool
- **WHEN** a request is retried or falls back to another target
- **THEN** every attempt sends a body with the same hash, and no attempt holds a full copy in memory

#### Scenario: Flat memory at maximum size
- **WHEN** a 256 MiB media-dominated body passes through a patched component at its production `mem_limit` with no swap
- **THEN** its anonymous memory stays within the recorded target, `memory.peak` stays at least 32 MiB under `mem_limit`, and nothing is OOM-killed

#### Scenario: Large text stays inline
- **WHEN** a ticketed body carries more than 8 MiB of inline non-media bytes
- **THEN** the hop returns 413 `request_too_large` naming the 8 MiB ticketed inline-bytes limit

### Requirement: Spools are private, on disk and always cleaned up

Every spool, including the keeper and bridge spools, SHALL be on a disk-backed volume. A component SHALL refuse to start when its spool directory is on tmpfs or ramfs. Spool directories SHALL have mode 0700, and spool files SHALL have mode 0600. Writers and readers SHALL bound the spool's page cache with `fdatasync` and `posix_fadvise(DONTNEED)`. A spool write SHALL be refused with an explicit 507 when real free disk space after the write would fall below the 1 GiB reserve. It SHALL never fall back to memory. Every spool file SHALL be anonymous (`O_TMPFILE`, close-on-exec) with no named fallback, so a crash frees it. Every hop below keeper SHALL close its spool before it commits its response status, so a caller's retry never overlaps it. Keeper SHALL close its spool after its history record. Spool files SHALL be closed on every exit path. `GOMEMLIMIT`, swap and OOM tolerance SHALL NOT substitute for byte accounting.

#### Scenario: tmpfs spool is refused
- **WHEN** a component's spool directory is configured on tmpfs
- **THEN** the component fails to start and reports an explicit configuration error

#### Scenario: Disk floor reached
- **WHEN** a spool write would leave less than the 1 GiB reserve free
- **THEN** the request is rejected with an explicit 507, and no bytes are buffered in memory instead

#### Scenario: Cleanup on abort
- **WHEN** a client aborts, an upstream closes early, a request is rejected, or a handler panics
- **THEN** no spool file for that request remains, and its lease is dropped

### Requirement: Keeper stays transparent while spooling large requests

Keeper SHALL be built on the parked P15 spool-first base, and its anonymous memory SHALL stay flat in the request size. `MAX_REQUEST_BYTES` and the history hard limit SHALL equal the 256 MiB cap. `STORE_PIN_MAX_BYTES` SHALL admit at least one maximum history. Keeper SHALL compute the expanded size before sending and SHALL reject an over-limit expansion explicitly. Keeper SHALL relay inner closes and failures as explicit error events. Keeper SHALL NOT add autonomous timers or expiry, global serialization of requests, or count or ID caps, and SHALL NOT limit the number of input or history items. Its only refusals SHALL be byte budgets. It SHALL preserve per-WebSocket ordering.

#### Scenario: Incident replay
- **WHEN** a client sends a 1.55 MB WS continuation delta whose expanded history is about 17.9 MB
- **THEN** keeper streams the expansion to the bridge, and the turn completes

#### Scenario: Many small input items
- **WHEN** a client sends a 16 MiB text body with more than 8192 input items
- **THEN** keeper accepts it within its byte budgets and does not refuse it by item count

#### Scenario: Expansion above the cap
- **WHEN** the expanded history would exceed 256 MiB
- **THEN** keeper sends the client an explicit error event naming the limit and sends nothing upstream

### Requirement: Third-party patches ship as switchable digest-pinned forks

The Sub2API, CPA and AISIX patches SHALL each have a config switch that defaults to off upstream and is enabled in this deployment. Each patch SHALL be offered upstream as a PR, following that project's contribution rules.

The component SHALL run a digest-pinned fork image built from its patch branch. It SHALL return to an official image only when every patch it carries is in an official release. The CPA fork SHALL be based on the production tree (`v8.0.51` plus the PR #7134 backport). The AISIX fork SHALL use the repository's patched build line.

A long-lived fork SHALL be rebased periodically, and a rebased digest SHALL enter the compatibility lock only after the CI gate passes. In every case `compose.yaml`, `.env.example` and the compatibility lock SHALL name the same digest.

These SHALL be unchanged: AISIX `upstream.stream_timeout_ms: 120000`, Squid egress, ingress authentication, the namespace topology, and account, pool and OAuth state.

#### Scenario: Pins agree
- **WHEN** an operator compares tracked `compose.yaml`, `.env.example`, the published compatibility lock and the running images
- **THEN** all of them name the same digest for each component

#### Scenario: Carried backport is kept
- **WHEN** the CPA body-spool PR merges upstream but PR #7134 is not yet in an official release
- **THEN** CPA stays on the fork that carries both

### Requirement: Time budget is explicit

The gateway SHALL keep AISIX `upstream.stream_timeout_ms` at 120000. Because that timeout also covers the upload from AISIX to the provider, the deployment record SHALL state the measured egress upload rate and the largest body that fits the window. A request that exceeds the window SHALL end with an explicit timeout error relayed to the client, never a bare close or an empty reply.

#### Scenario: Upload exceeds the window
- **WHEN** a large body cannot reach the provider and receive a first byte within 120 s
- **THEN** the client receives an explicit timeout error from the relaying hops

### Requirement: CI capacity gate publishes a compatibility lock

CI SHALL run the full chain in a throwaway compose stack with production `mem_limit`s, no swap, Squid in front of a mock upstream, and spool and ledger volumes on a size-limited disk filesystem. The run SHALL use no paid inference and no real credentials.

Every run SHALL test these exact rungs over HTTP and WS continuation, each with a keeper-expanded variant: 1,048,576; 16,777,216; 67,108,863; 67,108,864; 245,000,000; 268,435,456; and the over-cap 268,435,457. It SHALL also test text-heavy rungs of 16,777,216; 33,554,432; 67,108,863 and 67,108,864 bytes. It SHALL also run the concurrency, memory-admission, per-hop memory, authentication-first, refusal-during-upload, ledger-permission, disk-floor, ticket-release, inner-error, provider-413, fallback-path, provider-fault and throttled-egress steps. A nightly run SHALL test 1,073,741,824 bytes with a CI-only cap.

Each step SHALL record every hop's cgroup `memory.peak`, anonymous and file pages, and the time at each hop. When the gate passes, it SHALL publish a digest-pinned compatibility lock as a release artifact. The lock SHALL include the measured footprint factors and the fallback limits. Production SHALL deploy only digests that appear in a published lock. The Sub2API compliance acknowledgment SHALL be submitted automatically only in local throwaway and CI instances, never in production.

#### Scenario: Ladder step passes
- **WHEN** a ladder step runs
- **THEN** it passes only if one of these holds: the mock received bytes identical to the reference, or the expected explicit error arrived. In addition, no container restarted, every `memory.peak` stayed at least 32 MiB under its `mem_limit`, MemAvailable stayed at or above the floor, and no spool or ticket file remains

#### Scenario: Red gate publishes nothing
- **WHEN** any step fails
- **THEN** no compatibility lock is published

#### Scenario: Compliance acknowledgment stays out of production
- **WHEN** production compose, Ansible or private-config templates are validated
- **THEN** validation fails if they contain or submit the Sub2API compliance acknowledgment
