## Context

The Responses chain is: client → Cloudflare → firewall APISIX → ai-sse-keepalive → keeper → bridge → Sub2API (HTTP/SSE) → apisix-models → AISIX → CPA → Squid → provider. Every inference request after the firewall passes through ai-sse-keepalive and keeper. The restricted Operator entrance joins at keepalive, so it also passes keeper.

**Production host (read 2026-11-04):**
- root filesystem: 31.6 GB ext4, 4.66 GB free;
- RAM: 2.06 GB, MemAvailable about 945 MB (901 MiB);
- swap: 4.7 GB (zram plus disk).
- Containers see the host `/proc/meminfo` (no lxcfs), so a MemAvailable read inside keeper is the host value.
- The keeper store sits on the same disk, with a 1 GiB live-payload cap (`STORE_MAX_BYTES=1073741824`).
- Disk is **best effort** (user decision 2026-11-04): no disk expansion and no guaranteed concurrency.

### Evidence: capacity audit (2026-11-04)

Each hop was run locally against a mock upstream, and its cgroup `memory.peak` was read. Measurements used a 6g limit unless a real limit is shown. "Production limit" is the compose `mem_limit` plus its swap allowance.

| Hop | Production limit | Limit today | Buffering | Peak @16 / 64 / 256 MiB / 1 GiB | Note |
|---|---|---|---|---|---|
| firewall APISIX 3.19 | 320m (+ default swap) | `client_max_body_size 16m` | temp file on disk (1×) | flat, about 57 MiB | the nginx temp path was not measured (`disk_peak` 0) |
| ai-sse-keepalive | 128m (+ default swap) | inspects ≤ 8 MiB, passes larger bodies through | streaming | flat, about 35 MiB | no heartbeat above 8 MiB |
| keeper b49ad9ac | 384m, no swap | `MAX_REQUEST_BYTES` 32 MiB, hard limit 64 MiB | in memory (3.1× anon) | — | fails above 64 MiB |
| keeper P14/P15 (parked) | 384m, no swap | spool-first | disk | anon flat; `memory.peak` 384 MiB at 256 MiB in 384m (file pages) | disk peak 391–479 MiB at 256 MiB, 3.1 GiB at 1 GiB |
| bridge b482 | 256m, no swap | `MAX_MESSAGE_BYTES` / `MAX_BODY_BYTES` 16 MiB | spools to disk | flat, about 16 MiB; 1 GiB OK once raised | WS close 1009 above 16 MiB |
| Sub2API ab6b00e | 320m (+ default swap) | `max_request_body_size` 256 MiB | whole body in memory (about 6.2× at 256) | 272 MB / 434 MB / 1.66 GB / 413 | OOM-killed at 64 MiB without swap (exit 137, empty reply) |
| apisix-models | 128m (+ default swap) | 64m | temp file on disk | projected flat (same engine as the firewall, not measured) | |
| AISIX 1.8.0-pr1387 | 512m, no swap | none (0 = disabled) | in memory (about 3×) | 143 MB / 319 MB / 813 MB / 2.83 GB | OOM above 512m at 256 MiB |
| CPA v8.0.45 / v8.0.51 | 1g + 1g swap | none | in memory (live peak 7–9×) | 158 MB / 554 MB / 1.36 GB / 5.4 GB | the 480m red test OOMs at 64 MiB; production (1g + swap) carries 64 MiB but not 256 MiB |
| Squid egress | 192m + 64m swap | ssl-bump on CPA → provider | streaming (projected) | not measured | |

The CI stack runs with no swap. That is deliberately stricter than production, where CPA, Sub2API and the nginx hops may swap.

### Evidence: Milestone 0 measurement (2026-11-04, local)

Today's **unpatched** hops were measured locally against the mock upstream, one request at a time, with a **fresh container per request**, cgroup `memory.peak` reset per run and `x = (peak − idle) / body`. Production was not touched, and no credentials or paid calls were used. Bodies were exact sizes of 1/16/32/64/256 MiB in two shapes: image-heavy (about 80 % in one base64 data URL) and text-heavy (only strings of 16 KiB or less). The reproducible runner is `m0/run-m0.sh`; task 0.4 promotes it (D10).

**These multiples describe today's whole-body code. They are the "before" state and the red baseline. The post-patch skeleton-path values in D4 and D7 stay acceptance thresholds that CI must meet.**

| Hop | Memory multiple, ≥ 16 MiB (image / text) | Disk footprint | At its production limit |
|---|---|---|---|
| Sub2API ab6b00e | 14–17× at 16 MiB, 7.1 / 5.7× at 256 | 0 (memory only) | 320m: text 16 MiB peaks 316 MiB (inside the 32 MiB margin), 32 MiB at the ceiling, OOM with an empty reply at 64 MiB |
| CPA v8.0.51 | 8–10× to 64 MiB, 5.1 / 5.3× at 256 | 0 | 480m: OOM at 64 MiB; production 1g + 1g swap carries 256 MiB using about 300 MiB of swap |
| AISIX pr1387 | 4–5× at 16 MiB, 3.2 / 3.4× at 256 | 0 | 512m: OOM at 256 MiB |
| keeper b49 | 5.0–6.4 / 3.6–4.4× | 2.0–4.5× peak in flight, 1.5–2.5× retained in its store | 413 above 32 MiB |
| firewall APISIX | flat, about 60 MiB | 1.0× (unlinked temp file) | — |
| bridge (limits raised) | flat, ≤ 17 MiB | 1.0× | production limits: **WS close 1009 at ≥ 16 MiB**, not an explicit error |

- **Body shape does not matter for today's code.** Image and text bodies are within ±25 % at every hop, because every hop holds the whole body.
- **Overlap is a sum.** Every buffering hop holds its copy at the same time, until the upstream response finishes. The simultaneous sum was 18.4× for a 64 MiB image body through the full chain, 20× for 32 MiB of text, and 8.3× (2.23 GB) for 256 MiB from the bridge onward through Sub2API, AISIX and CPA. The gate therefore sums per-hop reservations; it never takes a maximum (D4).
- **Keeper item-count cap.** `maxHistoryItems = 8192` (`working.go:25`). A text body with more than 8192 input items gets a misleading 400 "input must be a string or an array of items". Reproduced at 48 MiB (11,329 items) and 64 MiB; 32 MiB (7,605 items) passes. This is a count cap and contradicts the keeper rules (D5).
- **Upload time.** A 146 s slow client upload (56 KiB/s) returned 200 at keeper, Sub2API, AISIX and CPA, so no hop has a client-read timeout at or below 146 s. With a slowly reading upstream, AISIX returned **504 at exactly 120.0 s** (`error_class="timeout"`): `stream_timeout_ms` covers sending the request body (D8).
- **Loopback wall time at 256 MiB:** Sub2API 21 s (image) and 53 s (text), CPA about 18 s, AISIX about 5 s; 27 s end to end through Sub2API + AISIX + CPA with a fast upstream. Production CPU is slower and has shown steal.
- **Not measured:** apisix-models (same engine as the firewall), keepalive, Squid, real WAN, and the PR #7134 CPA build (its body handling is unchanged).

### Evidence: incident (2026-11-04)

The client sent a 1.55 MB WS continuation delta. Keeper expanded the stored history into one WS frame of about 17.9 MB. The bridge rejected it with close 1009, because the frame exceeded `MAX_MESSAGE_BYTES`. Keeper logged 502 and sent the client only a 4-byte close frame. The client reported `Connection error.`.

### Evidence: streaming studies

- **CPA:** about 15–45 gjson/sjson passes over the whole body. Allocation per request is 20.6× the body on the codex path, 47× on claude and 15.4× on openai-compat. A `bodyspool` prototype (local branch, not wired in) has these results:
  - its round trip is byte-exact, including about 137k fuzz runs;
  - `translate(body) == splice(translate(skeleton))` is byte-identical for the codex, claude and openai-chat translators;
  - ingesting 254 MiB allocates 0.5 MiB;
  - skeletons sent through the unmodified binary in a 480m container peak at 82–98 MiB for 256 MiB bodies and 224–282 MiB for 1 GiB bodies, with no OOM.
  - The prototype externalized an allow-list of media keys (`image_url`, `file_data`, `url`, `data`). Text keys were left inline, because externalizing them needs a review of every consumer.
  - `/responses/compact` was out of the prototype's scope.
- **CPA, third-party copy-reduction fork (JohnsonRan/CLIProxyAPI main, `v8.0.51-4234-g8df2889c2`).** Its branches `request-body-no-copy`, `claude-request-nocopy-reads` and `runtime-memory-bounds`, plus the gjson/sjson → moego ojson migration, remove some whole-body copies but still keep the whole body in memory. Measured with the same red test (64 MiB body, 480m cgroup, mock upstream):

  | Route | Official v8.0.51 | JohnsonRan main |
  |---|---|---|
  | codex | OOM, empty reply | PASS, peak 410 MiB |
  | claude | OOM | OOM |
  | openai-compat | OOM | PASS, peak 480 MiB (at the ceiling) |

  Memory still grows linearly with the body (about 6.4× on codex). The fork is evidence only; this change does not use it (D6).
- **AISIX:** `Json<Value>` extraction, `body_for_target` clones, effort-mapping clones, the outbound `to_vec`, content capture and the token-estimate fallback each add about N. The buffered peak is about 3N (770 MB at 256 MiB). A streaming parse of only the routing fields peaked at 2 MB. serde behaviour that the patch must keep:
  - map keys come out sorted (BTreeMap);
  - numbers are rewritten in canonical form;
  - for duplicate keys, the last value wins;
  - the nesting limit is 127 levels;
  - `1e400` is rejected;
  - every JSON rejection maps to one generic 400.

  Guardrail chains and bridged targets (Bedrock, Vertex, Azure, Anthropic non-native) need the full body.
- **Sub2API (checkpoint):** peaks of 10–11× on the OAuth path and 7–8× on the API-key path. The top allocators are:
  - `normalizeCodex{Automation,Delegation}Bootstrap` (two full map decodes);
  - `ensureReqBody` (map decode plus marshal);
  - `NormalizeCompactionTriggerInputOrder`;
  - `ReadRequestBodyWithPrealloc` (two copies);
  - session-hash seeding;
  - gjson `GetBytes("input")`.

  Error classification (`openai_gateway_upstream_errors.go`): every status ≥ 500 fails over to another account, and an upstream 413 is account-scoped, retried on the next account and rewritten to "Request payload is too large".

### Evidence: provider ceilings (documentation, 2026-11-04)

The table stores exact bytes. Where the documentation says "MB", the decimal value is used, which is the lower reading.

| Provider route | Max request (bytes) |
|---|---|
| Anthropic first-party Messages | 128,000,000; larger gets 413 `request_too_large` |
| Anthropic on Bedrock | 20,000,000 |
| Anthropic on Google-managed platforms | 30,000,000 |
| Anthropic on other partner platforms | 32,000,000 |
| ChatGPT codex backend, third-party relays | unknown; not probed (user decision); the 256 MiB cap applies and the provider's own 413 or error is relayed |

OpenAI documents a 50 MB limit on **combined file inputs** per request. That is not a request-size ceiling, so the gateway does not enforce it. The provider's own error for it is relayed.

### Evidence: body composition (the user's heaviest real sessions)

- Text-heavy sessions: 70–96 % of the bytes are small strings (< 16 KiB). They contain no images.
- Image sessions: about 80 % of the bytes are base64.
- The context window bounds text requests, so they reach a few MB at most. Any request above about 32 MB is dominated by media.

### Evidence: time

- Local single-hop times at 256 MiB: keeper 11.3 s, CPA 14.6 s, Sub2API 19.2 s (audit logs); M0 measured Sub2API 21 s (image) and 53 s (text), CPA about 18 s, AISIX about 5 s.
- M0 measured that AISIX's timeout covers the request upload: 504 at exactly 120.0 s with a slow upstream reader (D8).
- AISIX wraps `req.send()` in `tokio::time::timeout(stream_timeout)` (`responses.rs:1423-1425`, `stream_timeout.rs:135-147`). The 120 s therefore covers the upload to CPA, CPA ingest, CPA's upload through Squid to the provider, and the provider's time to first byte.
- The host has shown up to 58 % CPU steal.
- Keepalive sends no heartbeat for bodies above 8 MiB.
- Other hop timeouts: the firewall route sets `send` and `read` timeouts of 600 s (`apisix.yaml`). The Sub2API, bridge, keeper and apisix-models upstream timeouts are recorded by task 4.4 (D8).

## Goals / Non-Goals

**Goals**

- A single 245 MB body works smoothly end to end whenever the host has room for it. A 1 GiB body works marginally in the nightly CI run. The production cap of 256 MiB rejects a 1 GiB body explicitly.
- Each route's effective limit is `min(256 MiB, provider ceiling)`, and every rejection is an explicit 413, 503 or 507, or an error event, that names the limit or the reason.
- One host-wide admission gate accounts real disk and memory for every body of 64 MiB or more. A request that does not fit is refused at once and explicitly.
- Anonymous memory per hop stays flat in the body size, and `memory.peak` stays at least 32 MiB under each hop's `mem_limit`.
- Every hop reports over-limit, refusal and inner failures explicitly.
- A CI gate proves the ladder and publishes a compatibility lock.

**Non-Goals**

- Cloudflare edge limits.
- A bigger host or disk, or a topology that bypasses Sub2API, CPA or AISIX.
- Guaranteed concurrency of large requests. Large requests run while the host has room; the rest are refused explicitly (user decision).
- Host-ledger accounting for bodies under 64 MiB. They carry no ticket (user decision). They are still bounded on covered paths by the 16 MiB inline-bytes limit (D2) and by the per-hop memory charge (D4), and on disk by the 1 GiB reserve and the per-write floor (D3). They do not "flow as today": a text-heavy body above 16 MiB of inline bytes now gets a named 413 instead of reaching an OOM.
- An interim state: nothing ships to production before the full design is ready, and everything goes live in one window (不过渡).
- **Tier B (optional, not planned).** Tier B would externalize text keys, or whole subtrees, for bodies made of many small strings or huge plain text. The measured composition shows that such bodies are bounded by the context window to a few MB. Tier B stays recorded in D2 in case real traffic changes.
- Converting the paths listed as fallbacks in D2, including `/responses/compact` (user decision). They keep their in-memory behaviour up to their limit L (at most 16 MiB) and refuse explicitly above it.
- Changing `upstream.stream_timeout_ms` (120000).

## Decisions

### D1. One cap, lowered per route by the provider ceiling

- The end-to-end cap is 256 MiB (268,435,456 bytes). It applies to the bytes a hop receives. For keeper, it applies to the expanded history it would send, not only to the client delta.
- Every hop that enforces a body limit uses this one value. The nginx hops use `256m`, and the bridge, keeper and Sub2API use 268,435,456. The default of the AISIX `request_body_limit_bytes` setting (0) is replaced by the cap. Unlike today's check, it also limits chunked bodies.
- **Per-route ceiling.** The component that picks the provider route applies `min(cap, ceiling)` before it sends upstream. Ceilings live in a tracked table keyed by concrete route identifiers:
  - AISIX route or target keys for its bridged targets (Bedrock 20,000,000; Google-managed 30,000,000; other partner platforms 32,000,000);
  - CPA executor plus credential type (the claude executor with first-party Anthropic credentials: 128,000,000).

  Every row holds an exact byte value. Unknown routes keep the 256 MiB cap, and a provider 413 is relayed as is, with its own `request_too_large` cause.
- **Rejection shape.** Before the response is committed, the hop returns an HTTP status with a JSON error body in the client's API shape. The body has a `code`, the limit or required bytes, and the hop or route that enforced it. After a WS turn has started, the hop sends a Responses `error` event with the same fields and then finishes the turn. A hop never answers with a bare close or an empty reply.
- **Production 1 GiB check.** The check runs at the origin, not through Cloudflare. It sends `Content-Length: 1073741824` with `Expect: 100-continue` and no body, and it asserts the firewall's JSON 413.
- **1 GiB in CI.** The nightly 1 GiB run sets the cap to 1 GiB through the same configuration keys on a throwaway stack.

### D2. Shared pattern: skeleton + blob spool

**Ingest.** A hop that needs the JSON reads the body as a stream with a small tokenizer.

- **Tier A is an allow-list.** Each component has an allow-list of media and file keys, as in the CPA study: `image_url` (string or `.url`), `input_image.image_url`, `input_file.file_data`, Anthropic `source.data`, chat `image_url.url`, plus any string value that is a `data:` URL. A string at an allow-listed key that is at least T (default 64 KiB) is copied into the spool. In the skeleton it is replaced by a fixed-length ASCII marker. All other strings, including large text and tool outputs, stay inline.
- **Nonce.** The marker carries a 128-bit random per-request nonce, chosen before the scan, and a blob index. If the body already contains the nonce prefix, the hop fails closed with an explicit 500 `marker_collision`. Rebuilding is impossible on a streamed body, and the collision probability is 2⁻¹²⁸.
- **Inline-bytes (skeleton) limit.** The skeleton is the inline bytes left after tier-A externalization. Every hop that parses JSON (Sub2API, AISIX, CPA) enforces a limit S on it for **every** request on a covered path (user decision, review N1):
  - **S = 16 MiB** for requests without a ticket;
  - **S = 8 MiB** for ticketed requests (≥ 64 MiB), which keeps the gate's memory reservation small enough to admit on today's host (D4, review N2).
  - Each hop's S must also satisfy `S_h ≤ (mem_limit − 32 MiB − idle) / k_skel,h`. The skeleton path runs today's JSON code on the inline bytes, so the M0 text-heavy 16 MiB multiples, where inline ≈ the whole body, are the measured k_skel: **Sub2API 17× (idle about 45 MiB), CPA 9× (idle about 30 MiB), AISIX 5× (idle about 122 MiB)**. That gives:

    | Hop | `mem_limit` | `S_h` bound | 16 MiB holds? |
    |---|---|---|---|
    | Sub2API | 320m | (320 − 32 − 45) / 17 ≈ **14.3 MiB** | **no**, on today's code |
    | CPA | 1g (480m red test) | (1024 − 32 − 30) / 9 ≈ 107 MiB (46 MiB) | yes |
    | AISIX | 512m | (512 − 32 − 122) / 5 ≈ 72 MiB | yes |

    M0 confirms this: an unpatched text-heavy 16 MiB body peaked at 316 MiB in Sub2API's 320m, inside the 32 MiB margin. **Acceptance threshold:** the patched Sub2API skeleton path must measure `k_skel ≤ 15.1` at 16 MiB (`(320 − 32 − 45) / 16`); the patch removes whole-body copies (`ReadRequestBodyWithPrealloc`, the double map decodes), so this is expected but not yet shown. If CI measures a lower `S_h` for any hop, it is **reported to the user before the window** (review m8, task 7.2) and not applied silently; the user's 16 MiB decision stands until they answer. CI records `S_h` and `k_skel,h` in the lock.
  - The tokenizer counts inline bytes as it streams. When the count passes S, the hop stops reading and returns an explicit 413 `request_too_large` that names the inline-bytes limit and the hop. A text-heavy body is therefore refused by name, never carried into an OOM. Tier B (externalizing text) stays a non-goal.
- **Validation parity.** The tokenizer validates each replaced string exactly as the component's parser would: UTF-8, escapes, surrogate pairs and control characters. The component's own parser then validates the skeleton. So the hop accepts or rejects the same bodies as before.

**Transform.** All existing code (gjson/sjson rewrites, `encoding/json` map decode and marshal, serde `Value` edits, translators, affinity, model rename, overrides) runs unchanged on the skeleton. Because only allow-listed media values are externalized, code that reads text fields sees the original text.

**Egress.** A splice reader streams the transformed skeleton and replaces each marker with its blob, read from the spool. The blob is re-escaped the way that component's serializer writes strings:

- raw original bytes for gjson/sjson;
- Go `encoding/json` escaping, including `\u003c`, `\u003e` and `\u0026`, when the skeleton passed through `json.Marshal`;
- serde escaping for AISIX.

`Content-Length` is computed from the segment list. The output is byte-identical to what the legacy path sends.

**Consumers that hash or measure content.** Session hashes, content seeds, CCH signing, codex compression and token estimates read the splice stream through a streaming hasher, counter or compressor, so their results are unchanged. Compression writes to a spool file, not to memory, and reserves its bytes before it writes them (D3).

**Retries and fallback.** Each attempt clones the small skeleton, applies that target's edits and splices again. The splice reads the spool with positional reads (`pread`), so attempts can overlap. Each hop below keeper (bridge, Sub2API, AISIX, CPA) **closes** its anonymous spool (D3), which frees its blocks, **before it commits its response status** to its caller, because after the commit no further attempt can use it. Keeper is outermost and nothing retries through it. It keeps its spool open until its history record finishes, and that time is covered by its lease. A caller that retries after reading that status therefore never overlaps with the previous attempt's inner spools.

**Fallback paths.** These paths need the full body in memory and are not converted in this change:

| Component | Fallback path |
|---|---|
| CPA | executors other than codex, claude and openai-compat (for example gemini, antigravity, xai, kimi, devin); plugin hooks that receive the body; `/responses/compact` (not converted in this change, user decision) |
| AISIX | guardrail chains; bridged non-native targets (Bedrock, Vertex, Azure, Anthropic non-native) |
| Sub2API | endpoints other than `/v1/responses` and `/backend-api/codex/responses` (for example `/v1/chat/completions`, `/v1/messages`); `/responses/compact` |

Each fallback path keeps today's in-memory behaviour up to its **fallback limit** L. L is the largest body for which the measured peak, `k_path × N` plus idle, stays at least 32 MiB under the hop's `mem_limit` without swap, and **L is never above 16 MiB** (review N3), which is today's effective firewall limit. A fallback path therefore never carries a ticketed body, and every request it carries is charged by the per-hop memory charge (D4). CI measures k and computes L, and the compatibility lock records both. A body above L on a fallback path gets an explicit 413 `request_too_large` that names the path and L. A 245 MB body on any fallback path therefore gets that 413.

**Markers never leave the hop.** Egress fails closed, with an explicit 500 and no upstream send, if any marker is left unresolved. Logs, request-log tees and error capture receive only the skeleton, with markers redacted. Blob bytes are never written to a log sink. Captured prefixes stay the same length as today.

**Differential tests (each component):**

- for every corpus body, `legacy(body)` and `splice(transform(skeleton(body)))` produce the same upstream bytes, `Content-Length`, session hash, client response and status;
- invalid bodies give the same accept or reject decision in both modes;
- fuzzing with random chunk splits compares the escaper and validator against the component's own parser;
- every retry and fallback attempt sends a body with the same hash;
- no spool file remains after success, client abort, upstream early close or panic.

The corpus covers escapes, unicode, surrogates, duplicate keys, big integers, `1e3`, deep nesting, affinity tags, signatures, data URLs and the real-shape bodies from `gen.py`.

**Tier B (recorded, not planned).** Tier A keeps memory flat only when the bulk of the body sits in allow-listed media. Tier B would externalize text keys after a review of every consumer (token estimates, guardrails, compaction normalization), or whole subtrees that the component never inspects. The inline-bytes limit makes the absence of tier B explicit: a body that is not media-dominated gets a named 413 instead of an untested path.

### D3. Spool storage

These rules apply to every spool: keeper, bridge, Sub2API, AISIX and CPA.

**Location.** Every spool is on a dedicated named volume on disk. A spool is never on tmpfs or a memory-backed volume. At startup, each component checks the spool directory with `statfs`. If the directory is on tmpfs or ramfs, the component refuses to start and logs an explicit configuration error. CPA (`/tmp` is 64m tmpfs) and AISIX (`/tmp` is 16m tmpfs) get new spool volumes, so they do not use `/tmp`.

**Files.**
- Each spool directory has mode 0700, and each spool file has mode 0600.
- **Every spool file is anonymous** (review n6/m9): it is opened with `O_TMPFILE | O_CLOEXEC` (ext4 supports it) and never has a name. There is no named-file fallback; a component whose spool filesystem rejects `O_TMPFILE` refuses to start with an explicit configuration error. `O_CLOEXEC` keeps the fd out of child processes such as CPA plugin subprocesses.
- An anonymous file's blocks are freed when its **last fd is closed**, not on unlink. Every exit path therefore closes the fd:
  - success: for keeper, after the history record; for the bridge and for Sub2API (which commits its status early to send keepalive pings), at turn end or handler return (its disk stays covered by the ticket lease for the whole turn, and closing earlier would release no credit but could stall an upstream that answers before reading the whole body; M1 bridge deviation, accepted 2026-11-04). This needs one invariant: **no ticketed turn is retried through the bridge while its earlier spool is still open**, because keeper releases the ticket only after every lease is gone and opens a new ticket for a retry. If that invariant cannot be shown in CI, the bridge's `k_disk` is 2. For the other hops below keeper, before the response status is committed (D2);
  - client abort;
  - upstream early close;
  - rejection;
  - panic recovery.
- A crashed process frees its spools when the kernel closes its fds, so a crash leaves no spool behind.
- With anonymous files only, no named spool file can exist. At startup each component asserts that its spool directory holds no regular file and refuses to start with an explicit error naming the file if one does. There is no sweep and no timer (simplified for v1, final review).

**Accounting.** Spool bytes of ticketed requests (64 MiB or more) are accounted by the host admission gate (D4), not by per-component budgets. A component that keeps its own work budget keeps **separate disk and memory budgets** (M1 bridge review M5); one counter must not cover both, and memory is never bounded only by `GOMEMLIMIT`. Bodies under 64 MiB are not in the host ledger (user decision). As a last-line floor for all traffic, a spool write refuses with an explicit 507 `insufficient_storage` when real free space after the write (`statfs` `f_bavail × f_frsize`) would drop below the 1 GiB reserve. This floor is a backstop, not the accounting. A hop never falls back to memory when a spool write is refused.

**Page cache.** Dirty and cached page cache counts against the cgroup. Every spool writer calls `fdatasync` on each written range about every 8 MiB and then `posix_fadvise(DONTNEED)`. It may use pipelined writeback in its place: `sync_file_range(SYNC_FILE_RANGE_WRITE)` on window k, then `WAIT_BEFORE | WRITE | WAIT_AFTER` and `DONTNEED` on window k−1. The bound stays about two windows. The per-window `fdatasync` cost about 0.28 s per 8 MiB on ext2 (keeper review Q3). Leader decision 2026-11-05: spools are anonymous `O_TMPFILE` files that are never durable, so they need no durability point, and the writeback serves only the page-cache bound. Stores with their own commit (keeper's bbolt) keep their own `fsync`. A test shows the bound holds on ext2. Every spool reader (splice, hash and compression passes) also calls `posix_fadvise(DONTNEED)` on each range after reading it. Together these bound the file pages a spool keeps in the cgroup to about 16 MiB. The pass criterion is `memory.peak ≤ mem_limit − 32 MiB` (D10), which includes file pages, so this bound is tested. **Keeper exception** (leader decision 2026-11-04 on the user's behalf, overridable): keeper's bbolt store is mmapped, so its clean file pages fill the cgroup as the store grows and no code change avoids that. Keeper therefore passes when `anon + file_dirty + file_writeback + kernel ≤ mem_limit − 32 MiB` with `oom = oom_kill = 0`. Its spool page cache stays bounded as above. PSI memory some/full and request latency are recorded, and reclaim must not noticeably slow requests.

**Contents.** A spool holds prompts and secrets. The rules are:
- each spool is private to its container;
- spools are not backed up;
- spools are never logged;
- the docs record that request bodies exist on disk only while a request is in flight.

### D4. Host-wide admission gate for large requests

**Covered paths** are the Responses endpoints: `/v1/responses` and `/backend-api/codex/responses` (Codex CLI), over HTTP and WS (leader decision 2026-11-05, keeper review M3). Every other path is bounded at the first hop (keeper) by L with the same named 413 the downstream hop would send. A native non-Responses WS message above L gets the bridge's named `request_too_large` error event and a normal close. A route that legitimately needs bodies above L is not given a limit silently; it goes to the user.

**Scope.** Every request on a covered path whose body, or keeper-expanded history, is at least 64 MiB (67,108,864 bytes) needs an admission **ticket**. Smaller requests carry no ticket; on covered paths they are still bounded by the inline-bytes limit (D2) and the per-hop memory charge (below).

**Where admission happens.** Keeper admits. Every inference request passes keeper (Context), and keeper is the first hop that holds request bytes, because both nginx hops stop buffering request bodies (`proxy_request_buffering off`, D5). Keeper also knows the expanded history size before it sends anything. A sidecar would add a hop and a failure mode without seeing more than keeper does.

**The ledger.** The ledger lives on a small shared named volume, `capacity-ledger`, on the root device.
- **Only keeper writes it** (review N4). Keeper mounts it read-write. Bridge, Sub2API, AISIX and CPA mount it **read-only** (`:ro`), so third-party code (CPA plugins, egress-capable hops) can neither delete a ticket, which would release credit while bytes are live, nor forge one. Inner hops take their lease with `flock(LOCK_SH)` on an `O_RDONLY | O_CLOEXEC` fd, which works on a read-only mount.
- The directory has mode 0750 and ticket files mode 0640, owned by keeper's user and a shared group. Ticket files hold only ids and sizes, never request content.
- A ticket file is named `<ledger>/<id>.ticket`, where `<id>` is the value of the ticket header or member. Every hop opens exactly that name (contract fixed 2026-11-05 after M2 found two naming variants).
- One ticket is one file. It holds the ticket id (128-bit random), N, the reserved disk bytes `R_disk` and the reserved memory bytes `R_mem`.
- A deploy validator checks the mount modes (rw only in keeper) and that the ledger and every spool volume share the root filesystem's device, so one `statfs` describes them all.

**The footprint.** `R_disk = Σ_h k_disk,h × N`, summed over the spooling hops: keeper, bridge, Sub2API, AISIX, CPA, plus CPA compression output. M0 measured overlap as a sum (Context), so the gate sums; it never takes a maximum.

| Hop | `k_disk` used by the gate | Source |
|---|---|---|
| keeper (P15 base) | 2 | measured: P15 spool-first disk peak 391–479 MiB at 256 MiB (1.5–1.9×, audit); unpatched b49 in-flight share ≤ 2.0× outside its store (M0). The store's own growth is reserved as store headroom |
| bridge | 1 | measured 1.0× (M0) |
| Sub2API, AISIX | 1 each | target: today they write 0 bytes (M0); after the patch the spool holds at most the externalized blobs (≤ N) |
| CPA | 2 | target: spool plus compression output |
| firewall, apisix-models | 0 | measured 1.0× today; 0 after `proxy_request_buffering off` (D5), asserted by CI |

That is **7 N, about 1.75 GiB at 256 MiB**. Each hop's measured disk peak must stay at or below `k_disk,h × N` (D10, review m4); a higher measurement raises the factor in the lock before the window.

`R_mem` uses measured skeleton multiples (review N2, T2, m9):

```
R_mem = Σ_h c_h + Σ_{h ∈ overlap} c_h
c_h   = k_mem,h × N + k_skel,h × S_ticket + P_h      S_ticket = 8 MiB, P_h = 16 MiB page-cache bound (D3)
overlap = {AISIX, CPA}                              a Sub2API failover attempt can overlap their teardown
```

Only AISIX and CPA are doubled, and their whole `c_h` is doubled, including `k_mem × N` (review m9): keeper, bridge and Sub2API handle a request once. `k_skel` is the M0 measured value (D2). `k_mem` (anonymous growth with N on the patched path) is an **acceptance threshold** of 0.1 for Sub2API, AISIX and CPA, because no patched build exists yet; keeper (P14/P15 measured flat anon) and bridge (measured ≤ 0.15× at 64 MiB, ≤ 0.05× at 256) use a flat 16 MiB term. CI records the measured factors in the lock, and the gate uses them.

**Fit on today's host (measured k_skel, target k_mem, 256 MiB):**

| Hop | `k_mem × N` | `k_skel × 8 MiB + P_h` | `c_h` | overlap |
|---|---|---|---|---|
| keeper | 0 | 16 MiB (flat) | 16 MiB | — |
| bridge | 0 | 16 MiB (flat) | 16 MiB | — |
| Sub2API | 25.6 MiB | 17 × 8 + 16 = 152 MiB | 177.6 MiB | — |
| AISIX | 25.6 MiB | 5 × 8 + 16 = 56 MiB | 81.6 MiB | 81.6 MiB |
| CPA | 25.6 MiB | 9 × 8 + 16 = 88 MiB | 113.6 MiB | 113.6 MiB |
| **total** | | | **404.8 MiB** | **195.2 MiB** |

`R_mem ≈ 600 MiB`. With MemAvailable 901 MiB (about 945 MB), `mem_avail = 901 − 200 − 32 = 669 MiB`, so **one 256 MiB request fits with 69 MiB to spare**, and a second concurrent one gets `capacity_busy`. Disk: 4.66 GB = 4.34 GiB free − 1 GiB reserve − store headroom (0–1 GiB) = 2.34–3.34 GiB, against `R_disk` = 1.75 GiB, so it fits even with an empty store. Swap (4.7 GB) is not counted: MemAvailable excludes it, and the gate never relies on it. The 69 MiB memory slack is thin: a patched `k_mem` or `k_skel` measured above its threshold could make the target request refuse on today's host, and CI's memory-admission step (D10) catches that before the window. A CI step admits a 256 MiB ticket against a test meminfo source reporting 901 MiB.

**Before, for comparison (M0, unpatched):** the same 256 MiB body held 2.23 GB at once in Sub2API + AISIX + CPA alone, about 2.5 times the host's MemAvailable.

<sup>Earlier projections, superseded by the measurements above: `k_disk` keeper 3 (8 N, 2 GiB total); `k_skel` Sub2API 11, AISIX 3, CPA 9; `R_mem` ≈ 469 MiB with only `w_h` doubled.</sup>

**Authentication before admission** (review N5). Keeper authenticates the caller before it admits a body of 64 MiB or more, using the existing Sub2API billing revalidation that cached reads already use, which fails closed. The check runs outside `ledger.lock`. An unauthenticated caller gets 401 in its API shape, no ticket and no spool, so it cannot take the only ticket the host can fit and hold it with a slow upload. If the billing revalidation itself is unreachable, keeper refuses with **503 `auth_unavailable` naming keeper**, never 401 and never an admission (review m6). For a `Content-Length` body authentication runs at the headers; for a chunked body or a fragmented WS message it runs when the received bytes cross 64 MiB, before admission. Smaller bodies are authenticated by Sub2API as today.

**Admission.** Keeper takes an exclusive `flock` on `ledger.lock`, reads the facts, decides, and writes the ticket **atomically** (review n4): it writes the content to an `O_TMPFILE` in the ledger directory, `fsync`s it and links it in under the final name with `linkat(AT_FDCWD, "/proc/self/fd/N", dirfd, name, AT_SYMLINK_FOLLOW)`, all before it releases the lock. This form needs no capability, which matters because keeper runs with `cap_drop: [ALL]` (`AT_EMPTY_PATH` may need `CAP_DAC_READ_SEARCH`); `EEXIST` gives the `O_EXCL` behaviour (review m2). The critical section is arithmetic plus one small file write; it never waits on the network or on a request. It computes:

```
disk_avail = statfs(f_bavail × f_frsize) − 1 GiB reserve − keeper_store_headroom − Σ R_disk(live tickets)
mem_avail  = MemAvailable − 200 MiB floor − 32 MiB margin − Σ R_mem(live tickets)
keeper_store_headroom = max(0, STORE_MAX_BYTES − on-disk size of the store file)
```

The store headroom uses the store file's **on-disk** size, not its live payload, because bbolt's file is not bounded by `STORE_MAX_BYTES` (metadata, pins, compaction; review n5). Growth beyond it is already visible in `statfs` at the next admission and is stopped by the per-write floor.

The request is admitted only if `R_disk ≤ disk_avail` and `R_mem ≤ mem_avail`. `statfs_free` and `MemAvailable` already reflect bytes that live tickets have written, so these bytes are counted twice. That errs only toward refusing, and the real free space can never drop below the floors.

**Refusal.** Keeper refuses at once, sends nothing downstream, and never queues:
- **503 `capacity_busy`** with `Retry-After` (a fixed hint, not a timer) when the request would fit if live tickets were released;
- **507 `insufficient_storage`** or **503 `insufficient_memory`** when it would not fit even with no live tickets.

Both bodies name the required bytes, the available bytes and the floor. On a WS turn, the same fields go into a Responses `error` event, and keeper finishes the turn.

**Sizes keeper learns late.**
- **HTTP with `Content-Length`:** keeper admits at the headers.
- **Chunked HTTP or a fragmented WS message:** keeper admits when the received bytes cross 64 MiB, reserving the cap (256 MiB) as the worst case. After the full read it lowers the reservation to the real N. Lowering is always safe.
- **WS continuation:** keeper admits on the expanded size, which it computes from stored lengths before sending.

**Propagation.** The ticket id travels with the request:
- keeper → bridge in the `X-AI-Gateway-Capacity-Ticket` header on HTTP. On WS, it travels as a top-level member `ai_gateway_capacity_ticket` of the turn frame, and keeper writes it as the **first** member of the frame object (M1 bridge review B1). The bridge reads it from a bounded in-memory prefix and takes its lease **before it spools any frame byte**, so the lease covers the spool's whole lifetime. A frame of 64 MiB or more whose prefix carries no ticket is refused when it crosses 64 MiB, not after it is spooled in full. The bridge strips that member, and every other byte stays unchanged.
- A hop configured without the ledger (`CAPACITY_LEDGER_DIR` unset) fails closed: it refuses every body of 64 MiB or more with an explicit error and never passes it with only a log line (M1 bridge review M3).
- the bridge, Sub2API and AISIX forward the header **only to their internal upstream** (bridge → Sub2API, Sub2API → apisix-models, AISIX → CPA). apisix-models forwards headers unchanged. No hop sends it on an egress path: Sub2API strips it on its Squid relay path (review n9).
- CPA consumes the header and never sends it to a provider.
- keeper strips any inbound header or member with that name from the client or the Operator entrance. Every path passes keeper, so no other hop strips it (simplified for v1, final review).

**Leases.** Each hop that handles a ticketed request opens the ticket file (`O_RDONLY | O_CLOEXEC`) and takes a **blocking** shared `flock` (`LOCK_SH`), which it holds for its whole handler lifetime. The handler lifetime covers both its spool and its skeleton memory. After the lock is granted the hop checks `fstat` `st_nlink > 0` (review n3); a ticket that was released while the hop waited has no link, and the hop refuses. A hop refuses with an explicit 503 `capacity_ticket_missing` or `capacity_ticket_invalid` when it receives 64 MiB or more:
- without a ticket;
- with a ticket it cannot open, or that has no link.

This guards against misconfiguration. Body growth between hops needs no separate allotment check: "ticket present and linked" plus the 256 MiB cap at every hop bounds it (simplified for v1, final review).

**Release.** When keeper's own request ends, by success, error or client abort, keeper drops its lease. A per-ticket goroutine then blocks on an exclusive `flock` of the ticket file. That waiting is not a timer. When every hop has dropped its shared lock, meaning every spool is closed and every handler has returned, the goroutine deletes the ticket under `ledger.lock`. Only keeper deletes tickets.
- The kernel drops the locks of a crashed process, so a crashed hop cannot hold a reservation forever.
- If keeper itself restarts, it starts one waiter per existing ticket file at startup, which is the same release path, and adds no timer.
- Tickets are linked atomically and no older format exists, so a partial ticket cannot occur. A ticket file that cannot be parsed at startup is treated as corruption: keeper refuses to start with an explicit error naming the file, and the operator follows the runbook (simplified for v1, final review).

**Leaked leases** (review n7). A hop that hangs while holding `LOCK_SH` keeps its ticket's capacity reserved, by design: there is no expiry. Keeper therefore exposes the live tickets on its metrics endpoint: count, and per ticket its age, `R_disk`, `R_mem` and the ticket file's inode. It does not report holder PIDs, because locks held in other PID namespaces show PID 0 inside keeper (review m1). The runbook in `docs/operations.md` says: on the host, run `lslocks` (or read the host's `/proc/locks`), match the ticket's inode, map the PID to its container, inspect it, and restart only that hop, which drops its lock through the kernel; never delete a ticket file by hand while a lock is held. No timer is added.

**Per-hop memory charge** (review N3). MemAvailable is host-wide, but OOM kills happen per cgroup. Each JSON-parsing hop (Sub2API, AISIX, CPA) therefore also keeps (the bridge parses only a bounded prefix and has no such check; a check without outstanding charges is soft and is not allowed; M1 bridge review M4) an in-process byte counter of the memory its in-flight requests may still use, and charges **every** request, ticketed or not:
- the charge is `k_skel,h ×` inline bytes for covered paths and `k_path × N` for fallback paths, taken in steps (for example each 1 MiB of inline bytes) **before** the bytes are allocated. A ticketed request is additionally charged `k_mem,h × N + P_h` at admission to the hop, so the per-hop charge equals the hop's `c_h` share of `R_mem` (review T2); at Sub2API that is 45 idle + 177.6 ≈ 223 MiB against 288 MiB;
- before each step it checks `memory.max − memory.current − outstanding charges ≥ step + 32 MiB`, reading its own cgroup files (keeper uses the working set `memory.current − inactive_file` in place of `memory.current`, matching its criterion in D3); `memory.current` already includes bytes in use, so the double count errs toward refusing;
- if the check fails, the hop refuses at once with 503 `insufficient_memory` naming the hop and the bytes, and frees the charge; nothing queues and no threshold moves;
- the charge is released when the handler returns.

This is a per-component byte budget with an atomic counter, so it adds no count cap, no lock beyond the counter and no timer. It is implemented once per language (one shared Go helper for Sub2API and CPA, one Rust module for AISIX), so the upstream PRs stay small. Concurrent 16 MiB text bodies on Sub2API, or concurrent fallback bodies at L on AISIX, are refused explicitly instead of crossing the cgroup limit.

**Keeper transparency.** The gate is a byte budget, so it is allowed under the keeper rules. It adds no count or ID cap, no `TryLock` refusal, no expiry and no request serialization. Only the short ledger arithmetic is serialized.

**What "smooth" means.** A single 245 MB media-dominated request is admitted whenever the host has room for its footprint. On today's host (4.66 GB free, MemAvailable 901 MiB), one 256 MiB request fits on both disk (1.75 of 2.34–3.34 GiB) and memory (600 of 669 MiB) (fit table above), and a second concurrent one gets `capacity_busy`. When keeper's store is empty, its full 1 GiB headroom is reserved, and one 256 MiB request still fits.

### D5. Per-component deltas

**Firewall APISIX and apisix-models.**
- Set `client_max_body_size 256m` and `proxy_request_buffering off` at **server or http level**, not inside a location (review n1). Every inference route uses the `proxy-buffering` setting, and APISIX then jumps to the named location `@disable_proxy_buffering` (`init.lua`, `ngx_tpl.lua`), which a location-level snippet would miss. The firewall already uses `http_server_configuration_snippet`; apisix-models uses the same key. CI's disk-peak-0 check catches a misplaced directive. nginx then streams the body upstream and writes no temp file. It still checks `Content-Length` against the limit before it reads the body, and it counts chunked bytes as they arrive.
- Add a JSON 413 body in OpenAI shape that names the 256 MiB limit.
- No route reads the request body today: the only Lua body reader handles the model-list response. CI asserts disk peak 0 for both hops.
- This change adds no plugins and does not change routing.

**ai-sse-keepalive.** No code change. It inspects bodies up to 8 MiB and passes larger bodies through as a stream, with no heartbeat (D8).

**Bridge.**
- Set `MAX_MESSAGE_BYTES` and `MAX_BODY_BYTES` to 268,435,456. The spool at `SPOOL_DIR` stays and follows D3. Removing the bridge spool is out of scope: the bridge needs the whole frame to set `Content-Length` and to answer an over-limit frame before it sends anything.
- Set the WebSocket library read limit above the cap plus framing overhead, so the library never closes with 1009 by itself. M0 reproduced today's behaviour: at production limits the bridge closes with WS 1009 at 16 MiB or more instead of returning an error, which is the incident's root failure. Our own code counts message bytes, including fragmented messages and the inflated size under permessage-deflate. When a frame or body exceeds the limit, the bridge sends a Responses `error` event (`request_too_large`, with the limit) and then closes.
- An upstream HTTP error before SSE starts becomes an error event in the WS turn.
- The bridge holds the ticket lease, strips the ticket frame member and forwards the header (D4).

**Keeper.** The base is the parked P15 tree, manifest `03fd6345…`. The P-series in-memory fixes fold into this redesign. P14/P15 spool-first input keeps anonymous memory flat. Changes on top of P15:
- `MAX_REQUEST_BYTES` changes from 32 MiB to 256 MiB, and the history hard limit `maxHistoryBytes` changes from 64 MiB to 256 MiB. Expanded history is streamed from the store or spool into the outgoing frame or body. It is never assembled in memory.
- `STORE_PIN_MAX_BYTES` changes from 100 MiB to 256 MiB, so that one maximum history can be pinned during a turn. Pins hold store entries and add no bytes. The store's disk use stays bounded by `STORE_MAX_BYTES`, which the gate counts as headroom (D4).
- Keeper reads its spool with the D3 read-side `fadvise`, so `memory.peak` stays under 384m − 32 MiB.
- **Remove the item-count cap** `maxHistoryItems = 8192` (`working.go:25`). M0 showed it refusing text bodies of more than 8192 input items with a misleading 400 "input must be a string or an array of items", independent of size. History is bounded only by bytes and accounting: the 256 MiB history limit, the store budgets and the gate (no-count-cap rule). Any internal structure that was sized by the item count is sized by bytes instead.
- Before sending, keeper computes the expanded size from stored lengths. If it exceeds the cap or the route limit, keeper answers with an explicit error event, or with a 413 on HTTP. It sends nothing upstream.
- Keeper hosts the admission gate and the ledger release waiters (D4).
- **Error relay.** When the inner WS closes, the inner HTTP fails, or keeper refuses a request locally, keeper sends the client a Responses `error` event. The event carries the inner close code or status, the reason and the hop name. Keeper then finishes the turn. It never forwards a bare close frame, and it never closes without an event.
- **Transparency.** Keeper adds no autonomous timers or expiry, no global serialization of requests, and no count or ID caps. The only limits are byte budgets. Per-WebSocket ordering stays as it is.

**Sub2API** (fork of `ab6b00e`). The patch makes these changes:
- `ReadRequestBodyWithPrealloc` changes to a spool tokenizer.
- The full-map paths (`normalizeCodex*Bootstrap`, `ensureReqBody`, `NormalizeCompactionTriggerInputOrder`) run on the skeleton.
- gjson `GetBytes("input")` returns skeleton bytes.
- The session hash and content seed are computed over the splice stream.
- The forward path sends the splice reader.
- Fallback endpoints (D2), including `/responses/compact`, refuse above their limit L (at most 16 MiB) with a 413.
- The inline-bytes limit S (D2) and the per-hop memory charge (D4) apply to every request.
- **Inner gateway errors pass through unchanged.** Some errors are generated by an inner gateway hop rather than by a provider: `request_too_large` (cap, route ceiling, inline-bytes or fallback limit), `insufficient_storage`, `insufficient_memory`, `capacity_busy` and `capacity_ticket_*`. The patch recognizes them by their JSON `code` together with a `hop` field. Such an error is request-scoped:
  - no failover and no retry on another account;
  - no account or pool statistic is recorded;
  - status, headers and body reach the client unchanged, with no message rewrite.
- **Every provider 413 passes through unchanged, for all request sizes** (user decision, review N6). Today `openai_gateway_upstream_errors.go` gives an upstream 413 account scope, retries it on the next account and rewrites the message to "Request payload is too large". The patch makes any upstream 413 request-scoped in the same way as the inner errors above: no failover, no retry, no account or pool statistic, and the provider's own status, headers and body (for example `request_too_large` with its cause) reach the client unchanged. A large body is therefore never re-uploaded across accounts after a 413.
  - **Known trade-off:** an account whose private ceiling is smaller than another account's no longer fails over to the larger account; the client sees that account's 413. This is accepted by the user's decision.
  - Other provider errors keep today's classification (for example a 5xx still fails over).
- The ticket lease is held and the header forwarded (D4).

Switch: `gateway.body_spool.enabled`, plus the threshold. The existing `max_request_body_size` setting becomes the 256 MiB cap. The account, pool and OAuth state stays unchanged.

This is an explicit exception to the rule in `docs/websocket-termination.md` that forbids patching Sub2API for our own requirements (task 9.3).

**CPA fork.** The base is the production tree: `v8.0.51` plus the PR #7134 backport (`v8.0.51-pr7134-1a196bd`, commit `1a196bd`). The `internal/bodyspool` prototype is wired in:
- ingest in `ReadRequestBody`;
- the request-log middleware and error capture record the skeleton only, with markers redacted, never blobs;
- splice at the codex, claude and openai-compat send points and at the codex WS `NextWriter`;
- codex compression and CCH signing over the splice stream, with compression output reserved and written to the spool;
- fallback paths (D2) refuse above their limit L (at most 16 MiB) with a 413;
- the inline-bytes limit S (D2) and the per-hop memory charge (D4) apply to every request;
- conditional copy sites converted: error capture, interceptor clones, plugin clones and retry loops;
- the ticket lease is held, and the ticket header is never sent to a provider.

**PR #7134 interplay.** PR #7134 classifies a pre-header HTTP/2 `PROTOCOL_ERROR` as transient and retries it across credentials. It also turns an HTML 403 into a 502 with `Retry-After` ≥ 5 s. With a large body:
- each CPA retry re-reads the spool, so no extra memory is used. It does re-upload the body within AISIX's 120 s (D8);
- the CPA retry budget is unchanged;
- Sub2API still fails over on that provider 502, because it is a provider 5xx. Each failover reuses the same ticket, and the inner spools of the previous attempt are already closed (D2). The overlap term in `R_mem` covers the AISIX and CPA handlers of the two attempts (D4).
- A provider 413 is never failed over by Sub2API (D5 Sub2API). **CPA makes a provider 413 request-scoped too** (review m7): it does not rotate to another credential on a 413, so a 245 MB body is uploaded to the provider once, and the provider's 413 is relayed with its cause. The codex executor keeps code `context_too_large` only for context-length signals (`context_length_exceeded` or a context-length message), so clients still compact; every other provider 413, such as a byte-size `request_too_large`, keeps the provider's own code and body (user decision 2026-11-05, full-chain smoke M2).

CI mocks cover an H2 reset, an HTML 403 and a provider 413 on an oversized body. They require bounded attempts and an explicit error, and for the 413 exactly one Sub2API account attempt and exactly one CPA provider attempt.

Switch: a config key that defaults to off upstream. Squid egress and the model-prefix setting stay unchanged. Per-executor ceilings come from D1.

**AISIX fork.** The base is the repo's existing patched build line: `services/routing/aisix` (`VERSION` `1.8.0-pr1387-06e8075-r2`, `patches/`, `Dockerfile`), reusing that build pipeline. The tracked compose pin moves from `1.8.0-rc.1` to the locked fork digest. The upstream PR is a branch rebased on `upstream/main`. The differential tests run on both the PR branch and the deployed branch. The patch makes these changes:
- A `SpooledJson` extractor engages above T. Below T, today's path runs unchanged. The extractor keeps the content-type check and the "failed to read request body" error, and returns 413 at the limit, including for chunked bodies.
- Serde parses the skeleton.
- A streaming outbound writer re-escapes the blobs the way serde does.
- Each attempt splices from the spool with positional reads.
- Content capture runs on the skeleton.
- The token estimator runs on the skeleton unchanged; with a media-only allow-list it never reads a blob (review n11).
- Guardrail and bridged-target paths are fallback paths (D2) and refuse above L (at most 16 MiB) with a 413.
- The inline-bytes limit S (D2) and the per-hop memory charge (D4) apply to every request.
- The ticket lease is held and the header forwarded to CPA (D4).

Switch: T = 0 (off) by default upstream. `upstream.stream_timeout_ms: 120000` stays unchanged.

**Squid.** No change. It is measured in CI with the mock placed behind it (ssl-bump with a test CA).

### D6. Upstream PRs and fork pins

| Component | Base | Switch | Upstream route |
|---|---|---|---|
| Sub2API | `ab6b00e` fork line | `gateway.body_spool.*`, default off | PR on top of the existing fork PR stack (spool and inner-error passthrough as separate PRs) |
| CPA | `v8.0.51` + PR #7134 (production tree) | config key, default off | PR against `router-for-me/CLIProxyAPI`; no translator code changes |
| AISIX | `1.8.0-pr1387-06e8075` line (repo build pipeline) | threshold T, default 0 (off) | issue first (CONTRIBUTING.md), then a PR rebased on `upstream/main` |

How the forks are run:
- Each component runs a digest-pinned fork image built from its patch branch.
- A component returns to an official image only when **every** patch it carries is in an official release. For CPA that means both PR #7134 and the body-spool PR.
- `compose.yaml`, `.env.example` and the compatibility lock name the same digests. The tracked CPA pin is first reconciled with production (official `v8.0.45` in tracked compose → the deployed `v8.0.51-pr7134-1a196bd` digest), so the CI red baseline tests the CPA that production runs.
- **Long-lived forks are allowed** (user decision). If an upstream declines its PR, the fork stays: it remains pinned by digest in the compatibility lock, and the CI gate must pass on every periodic rebase before a new digest enters the lock. AISIX upstream has already reworked the request-body area, so its rebases are expected to conflict.
- The CPA fork carries only reviewed upstream PR backports plus the body-spool patch. Commits from the JohnsonRan copy-reduction fork are not carried. Using any of them would need its own change.

### D7. Host budget, before and after

**Before** is M0 measured on today's unpatched hops (Context). **After** is an acceptance threshold, not a measurement, except where marked measured: the CPA skeleton peaks come from the prototype, and the keeper, bridge and firewall rows are measured flat today. The CI gate records the real numbers, and the lock stores the factors k. N is the body size.

| Hop | `mem_limit` | Before @256 MiB (M0, measured) | After @256 MiB (threshold) | After @1 GiB (CI) | Disk per ticketed request (`k_disk`) |
|---|---|---|---|---|---|
| firewall APISIX | 320m | ~60 MiB flat, 1.0 N on disk | ~60 MiB | ~60 MiB | 0 (no request buffering) |
| ai-sse-keepalive | 128m | ~35 MiB (audit, flat) | ~35 MiB | ~35 MiB | 0 |
| keeper | 384m | 413 above 32 MiB; 5–6.4× anon at ≤ 64 MiB | flat (P14/P15 measured), ≤ idle + 32 MiB | same | 2 N (P15 measured 1.5–1.9 N; store counted as headroom) |
| bridge | 256m | 1009 at ≥ 16 MiB; ≤ 17 MiB flat once raised | ≤ 17 MiB (measured flat) | same | 1 N (measured) |
| Sub2API | 320m | 7.1× image / 5.7× text (1.85 / 1.49 GB at 5g); OOM at 64 MiB in 320m | ≤ idle + 0.1 N + skeleton term | under 320m − 32 MiB | 1 N |
| apisix-models | 128m | not measured (same engine as firewall) | flat | flat | 0 (no request buffering) |
| AISIX | 512m | 3.2–3.4× (851–986 MB at 5g); OOM at 256 MiB in 512m | ≤ idle + 0.1 N + skeleton term | under 512m − 32 MiB | 1 N |
| CPA | 1g (480m in the red test) | 5.1–5.3× (1.33–1.38 GB at 5g); OOM at 64 MiB in 480m; 1g + swap carries 256 MiB with ~300 MiB swap | 82–98 MiB (prototype measured) | 224–282 MiB (prototype measured) | 2 N |
| Squid | 192m | not measured | streaming | streaming | 0 |

The skeleton term is `k_skel,h × S` with the D2 measured `k_skel`. Today's whole-body multiples are not used by the gate: the patched hops must meet the thresholds, and CI fails the window if they do not.

For every hop, `memory.peak`, file pages included, must stay ≤ `mem_limit − 32 MiB` (D10).

**Host totals at 256 MiB.**
- **Before (M0 measured):** the simultaneous sum for one 256 MiB request was 2.23 GB in Sub2API + AISIX + CPA alone (8.3×), more than twice the host's MemAvailable.
- **After:**
  - The ticket reserves 7 N of disk, about 1.75 GiB.
  - Memory: the reserved worst case `R_mem` ≈ 600 MiB (D4 fit table: measured `k_skel` at the 8 MiB ticketed limit, the 0.1 N `k_mem` threshold per patched hop, AISIX and CPA doubled in full). The typical image-heavy case uses less, but the gate reserves the worst case.
  - Admission compares these against `statfs` free space minus the 1 GiB reserve minus the keeper store headroom, and against MemAvailable minus 232 MiB (D4).
  - On today's host, one request fits and a second concurrent one is refused with `capacity_busy`.
- **Below 64 MiB:** not in the host ledger (user decision). On covered paths the inline-bytes limit (16 MiB, D2) and the per-hop memory charge (D4) bound each hop's memory; the per-write disk floor (D3) bounds disk; fallback paths carry at most L ≤ 16 MiB.
- **1 GiB CI run:** about 8 GiB of spool plus the keeper store. The runner needs at least 12 GiB free.

### D8. Time budget

`upstream.stream_timeout_ms` stays 120000 (user decision). Because AISIX wraps `send()` in that timeout (Context), the 120 s covers:

```
t_AISIX→CPA + t_CPA ingest + N / U_egress + TTFB_provider ≤ 120 s
```

M0 confirmed this: with a slowly reading upstream, AISIX returned 504 at exactly 120.0 s (`error_class="timeout"`). A 146 s slow **client** upload passed at every hop, so the client side is not the constraint.

**Measured and computed upload time** (`size × 8 / link rate`; CPA adds about 17 s of local processing inside the window, more on production CPU):

| Body | 10 Mbit/s | 20 Mbit/s | 50 Mbit/s | 100 Mbit/s |
|---|---|---|---|---|
| 64 MiB | 53.7 s | 26.8 s | 10.7 s | 5.4 s |
| 245 MB | 196 s | 98 s | 39.2 s | 19.6 s |
| 256 MiB | 214.7 s | 107.4 s | 42.9 s | 21.5 s |

With about 17 s of CPA time and no first-byte allowance, **245 MB needs at least about 19.6 Mbit/s sustained egress** (256 MiB about 21.5 Mbit/s); with a 20 s first byte, about 23 Mbit/s. At 10 Mbit/s the largest body that fits is about 143 MiB. **If production `U_egress` is below about 20–25 Mbit/s, 245 MB cannot succeed under today's timeout semantics; task 4.4 measures it and reports to the user before the window.**

**Handling (decision pending, see Open Questions).** `stream_timeout_ms` stays 120000 in every option.
- **Default in this change (no semantic change):** the timeout keeps covering the upload. A request that does not fit ends with AISIX's explicit timeout error, relayed by every hop (below). This is safe but can make 245 MB fail on a slow uplink.
- **Proposed AISIX patch (needs the user's confirmation, because it changes what the 120 s covers):** arm the 120 s `stream_timeout` only after the last request-body byte has been written to CPA, so it bounds CPA plus provider time to first byte as its name suggests. The send phase is then bounded by a write-progress timeout that reuses the same 120000 ms value: the send fails explicitly if no body byte is accepted for 120 s. No new knob, no value change, and a hung upload still ends explicitly. CPA's own upload to the provider still sits inside the window, so the patch helps only the AISIX → CPA leg unless CPA gets the same split.

`U_egress` is the host's upload rate through Squid to the provider. Everything before AISIX (the client upload, keeper, bridge, Sub2API, apisix-models) is outside this window. Keeper, bridge, Sub2API and AISIX itself are store-and-forward (the nginx hops now stream), so AISIX starts its timer only after it has received the whole body.
- **Timeouts before AISIX** (review n12). Those hops have their own timeouts: the firewall route's `send`/`read` 600 s (`apisix.yaml`), and the Sub2API, bridge, keeper and apisix-models upstream timeouts. Task 4.4 records each one and checks it against the 245 MB path time measured in CI (D10). A timeout that is shorter is reported to the user before the window; this change does not alter it silently.
- **Example.** With about 15 s local time and a 20 s first byte, a 245 MB body needs `U_egress ≥ 245 MB / 85 s ≈ 2.9 MB/s` (about 23 Mbit/s).
- **Measuring `U_egress`.** It is measured from existing production request logs (bytes and durations of real requests through Squid), with no synthetic upload. The record states the largest body that fits in 120 s at that rate.
- **If the rate is too low for 245 MB,** this is reported to the user before the rollout window. The timeout itself is not changed by this change.
- **When the 120 s elapses,** AISIX returns its timeout error. Sub2API passes it through, and keeper and the bridge relay it as an explicit error event. The client never gets a bare close.
- **Keepalive heartbeats.** Keepalive sends none for bodies above 8 MiB. Until the provider's first byte, the client receives nothing, and a client idle timeout shorter than that wait ends the request on the client side. Adding heartbeats would need `stream` detection without a full parse, which is out of scope. It is recorded as a risk. Cloudflare's own idle limit is out of scope.
- **Measurement in CI.** The ladder records the time at each hop. One CI step throttles the mock egress with `tc` to the measured `U_egress`. It requires 245 MB to complete within 120 s at AISIX, or, if the budget above says that cannot fit, the explicit timeout error.

### D9. Explicit errors at every hop

| Hop | Today | After |
|---|---|---|
| firewall APISIX / apisix-models | nginx 413 HTML | 413 with a JSON error body in OpenAI shape that names the 256 MiB limit |
| keeper | bare close (4 bytes) after inner 1009 or 502 | Responses `error` event carrying the inner code, reason and hop, then the end of the turn; 413, 503 `capacity_busy`, 503 `insufficient_memory` or 507 on HTTP |
| bridge | close 1009 | `error` event (`request_too_large`, limit), then close |
| Sub2API | OOM, empty reply; inner and provider 413 failed over and rewritten | spool path; 413 above the inline-bytes limit or L; 503 `insufficient_memory` from the per-hop charge; inner gateway errors and every provider 413 passed through unchanged with no failover |
| AISIX | OOM | 413 for the cap, a route ceiling or a fallback limit; 503 for an invalid ticket; the timeout error relayed |
| CPA | OOM, empty reply | 413 for an executor ceiling or a fallback limit; 507 when the disk floor is hit; a provider 413 relayed |
| keepalive, Squid | pass-through | unchanged; they forward whatever the inner hop answers |
| any hop | — | a WebSocket library limit set above the cap, so our code, not the library, answers over-limit messages |

### D10. CI capacity gate and compatibility lock

**Harness.** The gate is built from the 2026-11-04 audit harness, promoted into `ops/scripts/capacity/` with no private data. Only the listed harness files are copied, never its `logs/` directory or transcripts.
- `gen.py` generates Responses-shaped bodies in two shapes and pads every body to an exact byte size (review T1):
  - **image-heavy:** media parts plus a **fixed inline part of at most 4 MiB at any size**, matching the context-window bound on text (Context). The earlier audit generator grew text in proportion to the body (1.43 MiB inline at 64 MiB, 5.72 at 256, 23.75 at 1 GiB), which would push the 1 GiB rung over the 8 MiB ticketed limit; the "about 88 % media" figure is withdrawn. The harness asserts that the 245 MB, 256 MiB and 1 GiB image-heavy rungs stay below 8 MiB inline;
  - **text-heavy:** only strings of 16 KiB or less, with more than 8192 items at 32 MiB and above.
- The mock upstream drains the body as a stream and answers with codex/chat SSE. There is also an Anthropic-format mock, plus fault modes: H2 reset before headers, HTML 403, provider 413.
- `wsclient` and `chain_ws.sh` drive WS continuation through keeper and the bridge.
- `harness.py` samples each container's cgroup `memory.peak` (resetting it where the kernel supports that) and `memory.stat` anon and file pages, plus the time at each hop.

**Stack.** One compose project runs the full chain with every production `mem_limit`, no swap, the real image digests, and Squid in front of the mock. The upstream is the mock. The run uses no paid inference and no real credentials. Spool and ledger volumes sit on a size-limited filesystem (a loop-mounted ext4 image), so the gate meets real `statfs` limits.

**Host-level memory in CI** (review n8). The CI runner has far more memory than the host. The stack therefore runs under one parent cgroup (`cgroup_parent`, a dedicated slice) whose `memory.max` is the production RAM (2.06 GB); this is the "host-level cgroup" of the pass criteria, and its MemAvailable equivalent is `memory.max − memory.current`. Keeper reads MemAvailable from a **test-only meminfo source** (a file path set by a CI-only variable) that reports this value, or a fixed value for a step. The production validator rejects that variable, so production always reads `/proc/meminfo`.

The Sub2API compliance acknowledgment is posted automatically only in this throwaway stack and in CI. Production config and the Ansible roles never contain or send it, and a validator rejects it there.

**Ladder rungs (exact bytes)**, over HTTP and over WS continuation, image-heavy shape (inline ≤ 4 MiB):

| Rung | Bytes | Expectation after the change |
|---|---|---|
| 1 MiB | 1,048,576 | passes |
| 16 MiB | 16,777,216 | passes |
| just under the gate | 67,108,863 | passes with no ticket |
| 64 MiB | 67,108,864 | passes with a ticket |
| 245 MB | 245,000,000 | passes |
| 256 MiB | 268,435,456 | passes |
| over the cap | 268,435,457 | explicit 413 at the first hop |
| nightly 1 GiB | 1,073,741,824 | passes, with the CI-only cap |

**Text-heavy rungs** (review N1), inline bytes ≈ the whole body:

| Rung | Bytes | Expectation after the change |
|---|---|---|
| text 16 MiB | 16,777,216 | passes, with every hop's `memory.peak ≤ mem_limit − 32 MiB`, against settled containers (healthy + 30 s), stated in the rung |
| text 16 MiB, fresh Sub2API | 16,777,216 | every outcome is 200 or an explicit 503 `insufficient_memory`; never an OOM, a bare close or an unnamed 5xx (leader decision 2026-11-05) |
| text 32 MiB | 33,554,432 | 413 `request_too_large` naming the 16 MiB inline-bytes limit |
| text just under the gate | 67,108,863 | 413 naming the 16 MiB inline-bytes limit |
| text 64 MiB | 67,108,864 | ticket admitted, then 413 naming the 8 MiB ticketed limit |
| text 16 MiB, > 8192 items | 16,777,216 | passes at keeper (no item-count 400) and at every hop |

Each rung includes a keeper-expanded step that gives the expanded history the rung size.

**Red baseline by entry point** (review n11). The red baseline is produced by the Milestone 0 runner `run-m0.sh`, promoted into `ops/scripts/capacity/` (task 0.4); M0 has already recorded today's per-hop results (Context). The red run on today's pins states its entry point for each expectation, and task 0.4 records what it actually observed:
- **Through the firewall:** a body above 16 MiB gets the firewall's nginx 413 (HTML). An HTTP body of exactly 16,777,216 bytes passes nginx `16m`, so its failure, if any, is further in.
- **At the keepalive entry (CI only, behind the firewall):** the keeper-expanded WS step above 16 MiB gets bridge 1009 and a keeper bare close; a 67,108,864-byte body hits keeper's hard limit, or Sub2API or CPA is OOM-killed with an empty reply; a text body of more than 8192 items gets keeper's item-count 400 (M0).

**Extra steps:**
- **Concurrency.** On a filesystem sized for exactly one 256 MiB footprint, two concurrent 256 MiB requests: one passes, and the other gets `capacity_busy` immediately, with MemAvailable at or above the floor. After the first ends, the ledger is empty, and a retry passes.
- **Memory admission.** With the test meminfo source at 901 MiB, one 256 MiB ticket is admitted (the D4 fit). With it set below `R_mem + 232 MiB` on an idle ledger, the request gets 503 `insufficient_memory`.
- **Per-hop memory charge.** Concurrent text-heavy 16 MiB bodies on Sub2API, and concurrent bodies at L on an AISIX fallback path: each either passes or gets 503 `insufficient_memory` naming the hop; no hop is OOM-killed.
- **Authentication first.** An unauthenticated 256 MiB request gets 401 in its API shape, with no ticket and no spool.
- **Refusal during upload** (review n2). A request that is refused by keeper while the client is still uploading, entering through the firewall (for example chunked, crossing 64 MiB with no room), gets the JSON 503, never a 502 or a connection reset.
- **Ledger permissions.** An inner hop's attempt to create or delete a ledger file fails (read-only mount).
- **Disk floor.** A ticketed request on a filesystem too small even when idle gets 507, and nothing is written downstream.
- **Ticket release.** Kill keeper mid-request, and kill an inner hop mid-request. After restart, no ticket remains once all leases are gone, and no spool remains.
- **Inner errors.** An over-ceiling route through the full chain yields exactly one AISIX attempt and a client error that carries the limit and the route. An inner 507 produces no Sub2API failover. A provider 413 from the mock, on a small and on a large body, reaches the client unchanged after exactly one Sub2API account attempt and exactly one CPA provider attempt.
- **Fallback paths.** For each covered executor (codex, claude, openai-compat) at 64 MiB and 256 MiB, and for one fallback path above its L, the step expects the named 413.
- **PR #7134 faults.** An oversized body with an H2 reset, an HTML 403, or a provider 413: bounded attempts and an explicit error.
- **Time.** One throttled-egress step (D8).

**Pass criteria for each step:**
- HTTP 200 or a complete SSE, or exactly the expected explicit error;
- the mock received bytes identical to the reference;
- no container restart or OOM;
- for each hop, anon within its D7 target and `memory.peak ≤ mem_limit − 32 MiB`;
- for each spooling hop, the measured disk peak ≤ `k_disk,h × N` from the lock (review m4);
- MemAvailable stays at or above 200 MiB in the host-level cgroup;
- no spool or ticket file remains.

**Lock.** When the gate passes, it publishes `compatibility-lock.json` as a release artifact. The lock lists every image digest, the config hashes for the cap, thresholds and switches, the measured peaks, the factors `k_disk`, `k_mem` and `k_skel`, the per-hop inline-bytes limits `S_h`, and the fallback limits L. A deploy uses only digests that appear in a published lock.

## Implementation milestones

Milestones order the work and its proof. **Nothing ships to production before the rollout window** (不过渡). Milestones 0–2 end in local and CI acceptance only.

0. **Milestone 0 (proof first):** the CI harness, the compose capacity stack and the red baseline on today's production pins, recorded before any patch. The local measurement part is done (M0, Context); its runner `run-m0.sh` is the red-baseline deliverable that task 0.4 promotes into CI.
1. **Milestone 1 (hops we own), local acceptance only:**
   - keeper spool on the P15 base with the admission gate and ledger;
   - bridge `MAX_MESSAGE_BYTES` / `MAX_BODY_BYTES` and the APISIX `client_max_body_size` / `proxy_request_buffering` changes;
   - explicit 413, refusal and error relay at every owned hop, never a bare close.

   Acceptance runs through keeper → bridge → mock.
2. **Milestone 2:** the CPA, AISIX and Sub2API patches, including the Sub2API inner-error passthrough and the ticket lease in each hop.
3. **Milestone 3:** the full-chain ladder, the compatibility lock and the single production window.

## Rollout order and acceptance per hop

Rollout runs from the innermost hop outward, so that no outer hop admits bodies that an inner hop cannot carry. Every hop ships with its patch enabled and its cap at 256 MiB. All hops go live in one deployment window, with no interim cap, and no milestone artifact is deployed before that window.

1. **CPA fork** (`v8.0.51` + PR #7134 + spool). Acceptance:
   - the red test at 480m passes 64 and 256 MiB with `memory.peak ≤ mem_limit − 32 MiB`;
   - the differential and fuzz tests pass;
   - per-executor ceilings and fallback limits return a 413 that names the limit.
2. **AISIX fork.** Acceptance:
   - a 512m container with no swap passes 256 MiB and 1 GiB;
   - rejections and differential output match legacy mode;
   - `stream_timeout_ms` is unchanged.
3. **apisix-models.** Acceptance: a 256 MiB body is streamed byte-identical with no temp file, and a larger body gets the JSON 413.
4. **Sub2API fork.** Acceptance:
   - 256 MiB passes in 320m;
   - differential tests match, and session hashes are unchanged;
   - inner gateway errors and every provider 413 pass through without failover;
   - a text-heavy 32 MiB body gets the named inline-bytes 413;
   - the account, pool and OAuth state is untouched.
5. **Bridge.** Acceptance: a 256 MiB frame passes, and a larger frame gets an `error` event before the close.
6. **Keeper (P15 base, gate).** Acceptance:
   - anonymous memory stays flat at 256 MiB;
   - the incident replay (a 1.55 MB delta into about 17.9 MB of history) succeeds;
   - an over-limit expansion yields an error event;
   - the concurrency, release, crash, memory-admission, authentication-first and refusal-during-upload steps pass;
   - ordering and transparency invariants hold.
7. **Firewall APISIX.** Acceptance: the full chain passes 245 MB end to end.
8. **Compatibility lock.** Production deploys the locked digests through Ansible. Post-deploy checks:
   - a realistic 8 MiB production request passes;
   - the origin 1 GiB check (D1: `Content-Length` with `Expect: 100-continue`, no body) gets the JSON 413;
   - the disk reserve and the MemAvailable floor hold, and the ledger is empty when idle;
   - the namespace topology is unchanged;
   - no full-size stress test runs in production.

**Rollback.** Turn off each component's switch, which keeps its fork image and falls back to the in-memory path, and restore the previous lock. A rollback also restores the previous caps and the nginx request buffering, because those caps are what keep the in-memory paths from being OOM-killed.

## Visible to users and clients (M3 change notes)

- A Responses body above the per-route ceiling or 256 MiB, or above the 16 MiB inline-bytes limit (8 MiB when ticketed), gets a named 413 instead of an OOM, a bare close or an empty reply.
- `/backend-api/codex/responses` is a covered path, like `/v1/responses` (leader decision 2026-11-05).
- Non-Responses routes (`/v1/chat/completions`, `/v1/messages`, `/responses/compact`, `/v1beta/*`, `/antigravity/*` and others) get the same named 413 above L = 16 MiB, now already at keeper.
- A native non-Responses WS message above L gets a named `request_too_large` error event and a normal close.
- Provider 413s pass through unchanged at every hop. The exception is CPA's codex executor, which keeps `context_too_large` for context-length 413s (user decision 2026-11-05).
- AISIX no longer re-wraps provider 413s or inner capacity errors on any route, including direct-provider routes (chat, `/v1/messages`, translated, Azure/Vertex/Bedrock, rerank, audio, images, videos); they come back with the provider's status, headers and body (leader decision 2026-11-05).
- Known gap: on a codex size 413, CPA relays the provider's status and body but not its headers.
- Error field `limit` is renamed `limit_bytes` in the bridge's and keeper's error events and bodies; keeper's own refusals now also carry `hop: keeper`. `limit_bytes` is the exceeded byte limit for a 413, the disk reserve for the spool-floor 507, and the bytes the request needed for a memory 503 or a gate `capacity_busy`, the same at every hop.
- Admin routes (`/api/v1/admin/*`) keep today's behaviour: no keeper L, opaque admin WS. They are reachable only from loopback and the tailnet operator relay, never from public ingress (leader decision 2026-11-05; re-checked after deploy, task 9.5).
- Sub2API with legacy risk control enabled refuses a request carrying spooled media with 422 `spooled_media_moderation_unsupported` (off in production today). The prompt guard sees nonce-free placeholders in place of spooled media.
- Inner capacity errors (`insufficient_memory`, `insufficient_storage`, `capacity_busy`, `capacity_ticket_*`) carry `code`, `hop` and `limit_bytes` where defined, and are not retried or failed over.
- A single 16 MiB text request on a freshly restarted Sub2API can get 503 `insufficient_memory` until the container settles (leader decision 2026-11-05; profiling the startup anon spike is an M3 item).

## Risks / Trade-offs

- **Fork maintenance:** three fork lines until the PRs merge, possibly long-lived. AISIX upstream has moved ahead, so expect rebase conflicts. Mitigation: rebase per release, with the CI gate.
- **Hidden content readers:** future code that reads an allow-listed media string would see a marker. Mitigation: the allow-list is limited to media and file keys, unresolved markers fail closed, and the differential tests run on each upgrade.
- **Data on disk:** request bodies sit on disk while in flight. Mitigation: 0600 anonymous `O_TMPFILE` files, closed on every exit path, no backups.
- **Best-effort concurrency:** a second large request is refused while the first runs, and the conservative double count can refuse a request that would just have fit. This is accepted by the user's best-effort decision.
- **Small bodies outside the ledger:** bodies under 64 MiB use disk and memory outside the host ledger. Mitigation: the 16 MiB inline-bytes limit, the per-hop memory charge, the per-write disk floor, the fallback limits L ≤ 16 MiB, and the 1 GiB reserve. This is accepted by the user's decision.
- **Text-heavy bodies above 16 MiB inline:** refused with a named 413 (user decision). Tier B would lift this; it is not planned.
- **Provider 413 is never failed over** (user decision): an account with a smaller private ceiling no longer falls back to a larger account.
- **Leaked leases:** a hung hop holds its ticket's capacity with no expiry. Mitigation: the live-ticket metric and the runbook (D4).
- **Factors:** `k_skel`, `k_disk` for keeper and bridge, and the overlap-as-sum behaviour are measured (M0). `k_mem` and `k_disk` for the patched hops are acceptance thresholds until CI measures the patched builds. The memory fit leaves only 69 MiB of slack, so a patched factor above its threshold can make the target request refuse on today's host; CI catches this before the window.
- **Sub2API inline limit:** on today's code the measured `S_h` for Sub2API is about 14.3 MiB, below the user's 16 MiB. The patch must bring `k_skel` to ≤ 15.1, or the user decides (Open Questions).
- **Ticket plumbing:** the ticket header must survive four forwarding hops. Mitigation: hops fail closed on a missing ticket, and CI covers every hop.
- **Upload time:** measured, AISIX's 120 s covers the upload; 245 MB needs about 20–25 Mbit/s sustained egress. Mitigation: measure `U_egress` and report to the user before the window; the timeout value is unchanged, and the AISIX timer-start patch is a pending user decision (D8).
- **No heartbeat above 8 MiB:** clients with short idle timeouts may give up before the first byte.
- **Unknown provider ceilings:** codex and relay limits are unknown, so a body under 256 MiB can still fail at the provider. Mitigation: relay the provider's 413 or error explicitly. These routes are not probed (user decision).
- **Extra CPU:** each attempt adds one tokenizer and splice pass, on a host that has shown CPU steal.

## Review disposition (b28c7ae6)

All 27 findings of review b28c7ae6 (aggregate `8803f040…`) are accepted. None is rejected.

| Finding | Disposition |
|---|---|
| B1 disk / soft reserve | Live disk read (Context). The host-wide gate uses measured k, the keeper store headroom and a single ledger (D4). Both nginx hops stop buffering. The bridge spool is kept; reason in D5. |
| B2 soft memory / margin | Fallback paths are excluded from ticketed sizes through the measured limits L (D2). The memory ledger uses MemAvailable − 200 MiB − 32 MiB (D4), and every hop must stay at `memory.peak ≤ mem_limit − 32 MiB` (D7, D10). |
| B3 Sub2API failover | Inner gateway errors are passed through unchanged and request-scoped (D5), with a CI step (D10). |
| M1 page cache / metric | Read-side `fadvise` (D3); one metric (D7, D10); added to the keeper and bridge tasks. |
| M2 tier A drift | Tier A is an allow-list of media and file keys, and text stays inline (D2). The skeleton limit makes the boundary explicit. |
| M3 fallback paths | Listed in D2 with named 413s; the spec scenario names the covered set. |
| M4 OpenAI 50 MB | Removed as a ceiling (Context, D1). |
| M5 time budget | D8, with a throttled CI step. |
| M6 concurrency | Gate concurrency step on a size-limited filesystem (D10). |
| M7 M1 vs 不过渡 | M1 is local acceptance only, with nothing deployed before the window (milestones, task 9.2). |
| M8 task order | Milestone 0 holds the CI harness and red baseline. The D3 rules are in the keeper and bridge tasks. There are tasks for the gate, passthrough, time and concurrency, and the open budget question is gone. |
| M9 CPA vs production | Base `v8.0.51` + PR #7134. The tracked pin is reconciled first. The return rule is conditional. The JohnsonRan cherry-pick is dropped. PR #7134 fault mocks are added (D5, D6). |
| m1 real limits | Context table states production limits and swap; CI's no-swap is called deliberately stricter. |
| m2 rollout order | apisix-models now rolls out before Sub2API. |
| m3 1 GiB check | Origin check with `Expect: 100-continue` (D1). |
| m4 keepalive / Squid | Rows added (Context, D7, D9); the mock sits behind Squid (D10). |
| m5 WS 1009 | Library limits above the cap, with our own byte counting (D5, D9). |
| m6 MB / MiB | Exact bytes (Context, D1). |
| m7 nonce | 128-bit nonce, fail closed on collision (D2). |
| m8 log tee | Skeleton only, markers redacted (D2, D5). |
| m9 release / compression | Release after unlink and before commit; compression reserves (D2, D3, D4). |
| m10 AISIX base | Repo build line and pipeline; differential tests on both branches (D5, D6). |
| m11 Sub2API rule | Exception recorded (D5, task 9.3). |
| m12 rung sizes | Exact bytes (D10). |
| m13 CI disk | At least 12 GiB (D7). |
| m14 nginx flat | Marked as projections; CI asserts disk peak 0 (Context, D5, D7). |
| m15 namespace topology | Added to Unchanged and to the 9.1 and 9.2 checks. |

## Re-review disposition (64a0b855)

Scoped re-review 64a0b855 (aggregate `0c4ffa7b…`) found 1 blocking, 5 major and 12 minor findings, and 4 earlier findings only partly resolved. All are accepted; none is rejected.

| Finding | Disposition |
|---|---|
| N1 text-heavy 16–64 MiB, Sub2API OOM | The inline-bytes limit now applies to every request on a covered path: 16 MiB unticketed, 8 MiB ticketed, sized per hop by `S_h ≤ (mem_limit − 32 MiB − idle) / k_skel` (D2, user decision). Text-heavy rungs added (D10). "Flow as today" removed (Non-Goals, D4, proposal, spec). |
| N2 gate memory arithmetic | `w_h` from measured skeleton multiples at the 8 MiB ticketed limit; only AISIX and CPA doubled; fit table on live numbers gives `R_mem` ≈ 469 MiB against 669 MiB (D4, D7). *Superseded by M0: measured `R_mem` ≈ 600 MiB, still fits.* CI memory-admission step against 901 MiB (D10). |
| N3 per-cgroup memory | Per-hop memory charge against the hop's own cgroup for every request, with an explicit 503 `insufficient_memory` (D4). L capped at 16 MiB (D2). AISIX "never OOM-killed" reworded as conditional on these checks (aisix spec). |
| N4 ledger writable by every hop | Only keeper mounts the ledger read-write; inner hops mount it `:ro` and lease with `LOCK_SH` on an `O_RDONLY` fd; 0750/0640; mount-mode validator; CI permission step (D4, D10). |
| N5 admission before authentication | Keeper authenticates through the existing fail-closed Sub2API billing revalidation before admitting ≥ 64 MiB, outside `ledger.lock`; CI step for an unauthenticated 256 MiB request (D4, D10). |
| N6 provider 413 failed over | Sub2API passes every provider 413 through, for all sizes, request-scoped, with no failover, retry or rewrite (user decision); trade-off recorded (D5, Risks, client-visible spec, task 7.3). |
| B2 (partial) | Closed by N1, N2 and N3. |
| B3 (partial) | Closed by N6. |
| m9 (partial) / n6 | Anonymous `O_TMPFILE \| O_CLOEXEC` spools only, no named fallback; "close" before commit, not "unlink" (D2, D3, spec). |
| m12 (partial) / n11 | Red expectations stated per entry point; task 0.4 records observations (D10, task 0.4). Token-estimator wording fixed (D5 AISIX, task 6.2). |
| n1 named location | `proxy_request_buffering off` at server or http level (D5, task 2.2). |
| n2 refusal during upload | CI step through the firewall requiring the JSON 503 (D10, task 3.4). |
| n3 lease after deletion | Blocking `LOCK_SH`, `O_CLOEXEC`, `st_nlink > 0` check (D4). |
| n4 atomic ticket write | `O_TMPFILE` + `fsync` + `linkat` under `ledger.lock`; unparsable tickets charged at cap size until no lease remains (D4). *Charge removed in v1 (simplification 2).* |
| n5 store headroom / statfs | On-disk store size; `f_bavail × f_frsize`; same-device validator (D3, D4). |
| n7 leaked leases | Live-ticket metric and a runbook; no expiry (D4, task 9.3). *Holder PIDs replaced by inode (m1).* |
| n8 memory refusal in CI | Parent cgroup with production RAM and a CI-only meminfo source rejected in production (D10). |
| n9 ticket header on egress | Forwarded only to internal upstreams; stripped on the Sub2API Squid relay path (D4). |
| n10 ticket size check | Per-hop allotment `N_max = N + δ` (D4). *Removed in v1 (simplification 4); the 256 MiB cap bounds growth.* |
| n12 open items | Both open questions resolved (below). Other hop timeouts recorded and checked by task 4.4 (D8). |
| (b) MemAvailable source | Containers read the host `/proc/meminfo` (Context). |

## Final review and M0 disposition (b3f16d7c)

The final review (aggregate `7bdcd31a…`) approved the change with tasks T1–T2 and m1–m9 and five v1 simplifications. The M0 measurement replaced projected multiples. All are folded in; none is rejected.

| Item | Disposition |
|---|---|
| T1 nightly 1 GiB vs 8 MiB inline | Image-heavy shape has a fixed inline part ≤ 4 MiB; 88 % figure withdrawn; inline assertion on large rungs (D10, task 0.1). |
| T2 per-hop charge | Ticketed requests also charge `k_mem × N + P_h` at each hop (D4, task 4.2). |
| m1 holder PIDs | Metric reports inode instead; runbook uses host `lslocks` (D4, task 9.3). |
| m2 `linkat` without capabilities | `/proc/self/fd/N` form with `AT_SYMLINK_FOLLOW` (D4, task 1.3). |
| m3 δ assertion | Moot: the allotment check and δ are removed (simplification 4). |
| m4 `k_disk` assertion | Pass criterion added (D10). |
| m5 `S_ticket` evidence | Task 0.5 measures inline bytes of the owner's heaviest sessions (aggregates only) and reports to the user if any exceeds 8 MiB. |
| m6 auth details | 503 `auth_unavailable` when billing is unreachable; chunked bodies authenticate at 64 MiB (D4). |
| m7 CPA 413 retries | CPA makes a provider 413 request-scoped; CI asserts one CPA provider attempt (D5, D10, task 5.3). |
| m8 `S_h` below 16 MiB | Triggered: today's Sub2API measures about 14.3 MiB. Reported in Open Questions; threshold `k_skel ≤ 15.1` (D2, task 7.2). |
| m9 overlap term | AISIX and CPA double their whole `c_h`, including `k_mem × N` (D4). |
| Simplification 1: holder PIDs | Removed; they read as 0 across PID namespaces. |
| Simplification 2: cap-size charge of unparsable tickets | Removed; atomic linking makes a partial ticket impossible, so a parse failure is a loud startup error. |
| Simplification 3: startup spool sweep | Removed; anonymous files leave nothing to sweep, so one startup assert remains. |
| Simplification 4: per-hop `N_max = N + δ` check | Removed; ticket present and linked plus the 256 MiB cap does the same job without a measured parameter. |
| Simplification 5: firewall stripping the ticket header | Removed; keeper strips it on every path. |
| M0 keeper item-count cap | Removed in M1; history bounded by bytes (D5, task 1.2). |
| M0 AISIX timeout covers upload | Measured upload table and two handling options; user decision pending (D8). |
| M0 bridge 1009 | Confirmed; explicit error event already specified (D5, D9, task 3.2), and in the red baseline. |

## Open Questions

None open.

Resolved by the user on 2026-11-04:
- **AISIX timeout and upload (D8):** adopt the AISIX patch. `stream_timeout_ms` stays 120000 but is armed after the last request-body byte is sent; the send is bounded by a 120 s no-progress timeout with an explicit error (task 4.5, now unconditional);
- **Sub2API inline limit:** keep 16 MiB; the Sub2API patch must reach `k_skel ≤ 15.1` (task 7.2 acceptance). If CI still measures `S_h` below 16 MiB, it goes back to the user before the window;
- `/responses/compact` is not converted in this change; it stays a fallback path that returns a 413 naming its limit L (at most 16 MiB);
- the inline-bytes limit is 16 MiB for every request on a covered path and 8 MiB for ticketed requests; above it the hop returns a named 413;
- every provider 413 passes through Sub2API unchanged, for all request sizes, with no failover;
- no oversized probing of unknown provider routes; they keep the 256 MiB cap and relay the provider's own 413 or error;
- "1 GB marginally" means a nightly CI run only; production keeps the 256 MiB cap and returns 413 for 1 GiB;
- long-lived forks are allowed when an upstream declines a PR (D6);
- Tier B stays an optional non-goal;
- disk is best effort, with a host-wide admission gate for bodies of 64 MiB or more (D4); this replaces the per-component spool budget question;
- Sub2API passes inner 413, 503 and 507 gateway errors through unchanged (D5);
- no interim state: M1 is local acceptance only, and everything deploys in one window.
