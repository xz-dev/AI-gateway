# Architecture

How one `cpa-model-sync` round turns upstream inventories into CPA `models` arrays,
and where the process sits in the gateway. Policy keys are defined in
[configuration.md](configuration.md); code of record is `src/lib.rs` and
`src/main.rs`.

## Round flow

```
                  refresh(config)                         [main.rs]
                        |
      +-----------------+-----------------+
      |                 |                 |   for each of MANAGED_KINDS (5)
      v                 v                 v
 GET /v0/management/<kind>  (management key, 8 MiB cap, retry)
      |
      v
 entries[] -> for each entry: prefix  -> skip_channels? -> job
      |                                        |
      |                                        +-> status "skipped" (stderr), no fetch, no write
      v
 jobs.chunks(2)  ->  at most 2 channel workers per batch, rounds never overlap
      |
      v
 sync_channel_with_options(kind, prefix, filter)           [lib.rs]
      |
      +-- 1. GET /v0/management/<kind>        select the entry by exact prefix
      +-- 2. POST /v0/management/api-call     fetch upstream inventory via CPA
      +-- 3. filter                            include / exclude regexes
      +-- 4. canonical compare against fresh CPA models
      +-- 5. PATCH /v0/management/<kind>       only when different
      |
      v
 Outcome: Updated | Unchanged | Unconfirmed
      |
      v
 stdout: one summary line per round
 stderr: one JSON line per channel
```

## Discovery and skip

Each round starts with one management GET per managed kind. The response is
`{"<kind>": [ {...}, ... ]}` and each row's `prefix` is the channel identity. A row
whose `prefix` is missing or empty is kept as a channel-local failure rather than
hidden, so one malformed row cannot silently drop a channel.

`skip_channels` is checked at this point, before any upstream fetch. A skipped
channel is counted in `skipped`, logged with `"status":"skipped","reason":"configured
skip"`, and never fetched or written.

A failed discovery GET for a kind, or a row without a usable `prefix`, is recorded as
a `failed` result and does not stop the other kinds.

## Inventory fetch

For each non-skipped channel, the tool does **not** contact the upstream itself. It
asks CPA to do it with a single call to CPA's forwarding endpoint:

```
POST /v0/management/api-call
  { "method": "GET", "url": "<upstream url>",
    "header": { ... },
    "auth_index": "<CPA auth index>",   (when the credential has one)
    "proxy_url": "<channel proxy>"      (when the credential or channel has one) }
```

CPA performs the request and returns `{"status_code": <n>, "body": "<json string>"}`.

**Credential selection.** For `openai-compatibility` the first `api-key-entries`
element is used, falling back to the entry itself; all other kinds use the entry
directly. When the credential has a non-empty `auth-index`, the header value is the
literal `$TOKEN$` and CPA substitutes the real credential via `auth_index`. Only when
no auth index exists is the channel's `api-key` value used as the header value. This
keeps upstream credentials out of this process and out of its logs.

**Headers by kind.**

| Kind | Header |
| --- | --- |
| `claude-api-key` | `x-api-key: <credential>`, `anthropic-version: 2023-06-01` |
| all other managed kinds | `Authorization: Bearer <credential>` |

Channel-configured `headers` are the base map and these entries are inserted on top.

**Base URL.** `base-url` from the discovery entry, trailing `/` trimmed. For
`claude-api-key` only, a missing or empty `base-url` defaults to
`https://api.anthropic.com`, matching CPA's own `effectiveClaudeBaseURL` behavior.
For every other kind a missing `base-url` is an error.

**Path.** Default `/v1/models`. For `openai-compatibility` and `codex-api-key`,
`?client_version=<percent-encoded client_version>` is appended. A configured
`path` replaces the whole path (normalized to one leading `/`) and is used as-is,
with no `client_version` appended. If `base-url` ends with `/v1` and the path starts
with `/v1/`, every leading `/v1` is stripped from the path
(`trim_start_matches`). Pagination appends `after_id=<cursor>` with `?` or `&`
depending on whether the URL already has a query.

**Pagination.** Only `has_more: true` triggers a follow-up request, and only for
`claude-api-key`; the cursor is `last_id`. The loop is capped at 128 pages, and every
cursor is remembered so a repeated cursor is `repeated pagination cursor` rather than
an infinite loop. `has_more` absent or `false` ends the page loop; any other
combination is `invalid pagination response`. A failed page aborts the whole
inventory; there are no nested per-page retries. The inventory as a whole is retried
(`retries + 1` attempts, each starting again at page one), as is the discovery GET and
the fresh read in the write protocol. The write itself has its own attempt budget -
see [Write protocol](#write-protocol).

**Response parsing.** `data` or `models` is the row array. Each row's ID is `id`,
then `slug`, then `name`; a row with none of them is `model ID missing`. The
accumulated body bytes are checked against the inventory cap as pages arrive.

**Limits.**

| Boundary | Limit | Reason string when exceeded |
| --- | --- | --- |
| Management GET / PATCH response | 8 MiB | `invalid or oversized CPA response` |
| Single inventory response | 32 MiB | `invalid or oversized CPA response` |
| Accumulated inventory body bytes | 32 MiB | `inventory exceeds limit` |
| Claude inventory pages | 128 | `inventory page limit exceeded` |

An empty upstream inventory is an error (`empty upstream inventory`), never an empty
write: the tool must not interpret "upstream returned nothing" as "remove everything".

HTTP status from the upstream is classified, never logged verbatim:
`upstream rejected credentials` (401/403), `upstream rate limited` (429),
`upstream request failed` (anything else, including a missing status).

## Filtering and canonical comparison

Filtering produces the desired array directly as `{"name": <id>, "alias": <id>}`
records, sorted. See [configuration.md](configuration.md) for the exact keep rule.

Comparison runs against a freshly read CPA state, never a local cache, and both
sides pass through the same canonicalization:

- A `null` or absent `models` is treated as an empty array.
- Every record must have a non-empty `name`.
- An absent, `null` or empty `alias` is replaced with `name`, so CPA omitting a
  same-name alias still compares equal.
- Records are sorted by their serialized JSON, so **outer order is ignored**.
- Everything else is compared strictly: other fields, field types, duplicate
  counts and nested array order all matter. `"10"` and `10` are different;
  `[1,2]` and `[2,1]` are different; a duplicate record is not the same as one copy.
  Numbers are compared at full precision (`arbitrary_precision`).

If the canonical current array equals the canonical desired array, the result is
`Unchanged` and nothing is written - including after a process or CPA restart,
because the comparison starts from CPA's live state.

## Write protocol

```
 CPA_WRITER (process-wide mutex)
      |
      +-- fresh GET <kind>            re-select the entry, re-derive the selector
      +-- selector changed?  -> Err("channel identity changed")
      +-- canonical compare   -> equal -> Unchanged (no write)
      +-- PATCH <kind>  { <selector>, "value": { "models": [...] } }
      +-- response lost?      -> reconciliation read on the next attempt
      +-- attempts exhausted  -> Unconfirmed
```

- **Selector.** `openai-compatibility` uses `{"name": <name>}`; the other four kinds
  use `{"match": <api-key>}`. Selection at discovery time requires exactly one entry
  with the requested `prefix` and exactly one entry carrying that identity value;
  otherwise `channel identity missing or ambiguous` or `write selector is ambiguous`.
- **Body.** The selector plus `"value": {"models": [...]}`. That is a full replacement
  of the `models` array and nothing else. Credentials, `disabled`, `prefix`,
  `base-url`, `proxy-url` and `excluded-models` are never sent.
- **Mutex.** The fresh read, the comparison, the PATCH and any confirmation read all
  happen under one process-wide mutex (`CPA_WRITER`). CPA's management writer cannot
  safely process concurrent updates even for different channels. **Run one active
  synchronizer per CPA** - this mutex does not coordinate other processes or external
  management writers.
- **Retry and reconciliation.** One initial attempt plus up to `retries` more. Before
  each retry attempt a reconciliation read runs inside that attempt's budget: if the
  channel's models already equal the desired set, the result is `Updated` without a
  second PATCH. A retry delay is `retry_delay_ms * 2^(attempt-1)` - 1 s, 2 s, 4 s with
  defaults - and there is no sleep before the first attempt.
- **Outcomes.** `Updated` (write accepted, or reconciliation confirmed it),
  `Unchanged` (nothing to do), `Unconfirmed` (attempts exhausted with an ambiguous
  result). `Unconfirmed` is never reported as success and never triggers a
  whole-config restore.

The default `ureq` agent sets a global timeout of `timeout_ms`, keeps no idle
connections, follows no redirects, and uses no proxy. Proxy use is per channel: a
`proxy-url` on the credential or on the discovery entry is forwarded to CPA as
`proxy_url`.

## Concurrency and scheduling

- Channel jobs are processed in batches of **at most two** workers; fetches for two
  channels can overlap, writes cannot (the mutex serializes them).
- A round is fully joined before the next batch and before the next round, so rounds
  never overlap.
- `--once` runs exactly one round. Daemon mode sleeps until the next strictly future
  tick of `interval_seconds` measured from process start: after a slow round, missed
  ticks are discarded rather than replayed.
- `interval_seconds` is **not** an end-to-end staleness guarantee; operation time,
  failures, CPA propagation and downstream caches also contribute.

## Preview mode

`--preview -` reads the policy from stdin, validates every configured filter, then
does discovery and inventory reads for every discovered channel and prints one JSON
object to stdout. It never PATCHes CPA. A skipped channel is still reported, using
its current CPA models for both `source` and `desired` and without any upstream fetch.

Top-level fields:

| Field | Meaning |
| --- | --- |
| `version` | Preview format version, currently `1` |
| `policy_digest` | SHA-256 of the exact policy bytes read from the file or stdin |
| `image_identity` | `CPA_MODEL_SYNC_IMAGE_IDENTITY`, empty string when unset |
| `current_state_digest` | SHA-256 over the per-channel current-set digests |
| `source_state_digest` | SHA-256 over the per-channel source-set digests |
| `desired_state_digest` | SHA-256 over the per-channel desired-set digests |
| `approval_digest` | SHA-256 over the version, policy digest, image identity, the three state digests and the per-channel digest list |
| `channels` | Array, sorted by kind then prefix |

Each `channels[]` entry contains `kind`, `prefix`, `skipped`, `current_count`,
`source_count`, `desired_count`, `additions` (desired but not current), `removals`
(current but not desired), `unchanged` (count of current entries that survive),
`current_digest`, `source_digest` and `desired_digest`. Full model lists are not
printed; the digests are. The management key and upstream credentials never appear.

`approval_digest` binds the policy bytes, the image identity, the current CPA sets,
the source inventories and the desired sets. Recomputing it after an approval is how
an operator detects that any of those changed between plan and apply.

## Logging

- **stdout**: one JSON summary per round -
  `{"updated":N,"unchanged":N,"failed":N,"unconfirmed":N,"skipped":N}`. In daemon
  mode this is also the progress signal used by operations tooling.
- **stderr**: one JSON line per channel -
  `{"kind":...,"prefix":...,"status":...,"reason":...,"max_attempts":N}`. `status` is
  `updated`, `unchanged`, `unconfirmed`, `failed` or `skipped`. `reason` is `null` for
  `updated`, `unchanged` and `unconfirmed`, `"configured skip"` for `skipped`, and one
  of the static strings below for `failed`. `max_attempts` is `retries + 1` (the
  configured budget), not an observed attempt count; skipped lines omit it.
- Errors deliberately exclude request URLs, response bodies and credentials; upstream
  failures are reduced to the status classes in the table below.

## Error reasons

Every failure reason is one of these static strings, so a log line never contains
upstream data.

| Reason | Raised by | Trigger |
| --- | --- | --- |
| `operation failed` | retry wrapper | initial value of the last-error slot; unreachable in practice because every attempt overwrites it |
| `unmanaged channel kind` | discovery / sync / current-models | kind outside the five managed kinds |
| `invalid request timeout or retry budget` | validation | zero timeout, `retries > 30`, or overflowing budget |
| `invalid discovery response` | discovery, target selection | response lacks a `<kind>` array |
| `required string missing` | discovery, identity, inventory, compare | empty or non-string `prefix`, `name`, `api-key`, `base-url`, `body`, `last_id`, or model name/ID |
| `channel identity missing or ambiguous` | target selection | zero or more than one entry with that exact `prefix` |
| `write selector is ambiguous` | target selection | more than one entry shares the identity value |
| `channel identity changed` | write protocol | selector differs between prepare and fresh read |
| `CPA writer unavailable` | write protocol | the process-wide mutex is poisoned |
| `invalid include regex` / `invalid exclude regex` | filter compile | a pattern does not compile |
| `invalid current models` | canonical compare | CPA's `models` value is neither `null` nor an array |
| `invalid current model` | canonical compare | a current model record is not an object |
| `invalid upstream JSON` | inventory | the `body` string is not valid JSON |
| `invalid upstream inventory` | inventory | no `data` or `models` array |
| `model ID missing` | inventory | a row has no `id`, `slug` or `name` |
| `empty upstream inventory` | inventory | the completed inventory has zero models |
| `invalid pagination response` | inventory | `has_more` is true on a non-Claude kind, or not a boolean |
| `repeated pagination cursor` | inventory | a `last_id` cursor repeats |
| `inventory page limit exceeded` | inventory | 128 pages fetched without a terminal page |
| `inventory exceeds limit` | inventory | accumulated body bytes over 32 MiB |
| `invalid or oversized CPA response` | transport | body over 8 MiB (management) or 32 MiB (inventory), or invalid JSON |
| `CPA request failed` | transport | connection, timeout, TLS or non-2xx from CPA |
| `missing request body` / `unsupported operation` | transport | internal misuse; not reachable from the policy |
| `upstream rejected credentials` | inventory | upstream status 401 or 403 |
| `upstream rate limited` | inventory | upstream status 429 |
| `upstream request failed` | inventory | any other upstream status, including missing |
| `channel worker failed` | round | a worker thread panicked |
| `invalid configuration` / `cannot read configuration` | startup | policy not parseable or not readable |
| `invalid refresh interval` | startup | `interval_seconds` zero or overflowing |
| `CPA_MANAGEMENT_KEY is required` / `CPA_MANAGEMENT_KEY is empty` | startup | missing or empty management key |
| `usage: cpa-model-sync [--once\|--preview] CONFIG.json` | startup | wrong arguments |

## Placement in the gateway

```
  upstream provider API
          ^
          |  POST /v0/management/api-call  (CPA performs the request)
          |
  +-------+--------------------------+
  |  CPA (cli-proxy-api)             |  source of providers, credentials, native catalog
  +-------+--------------------------+
          ^  PATCH models
          |
  +-------+--------------------------+
  |  cpa-model-sync (this project)   |  one active synchronizer per CPA
  +----------------------------------+
          |
          v
  CPA native /v1/models  (current configured membership)
          |
          v
  model-catalog (TS)   enrich metadata only; include_models `!` globs hide public IDs
          |
          v
  APISIX /v1/models  intersection with the Sub2API group entitlement list
          |
          v
        client
```

Notes on the diagram:

- The synchronizer's only outbound target is CPA. Upstream calls are made by CPA
  through the same credentials CPA already holds.
- `model-catalog` never changes membership for the five managed kinds; it reads CPA's
  current members and enriches them. Its `metadata.include_models` (`!` entries) hides
  public IDs from the public catalog only.
- The APISIX front intersects the enriched catalog with the caller's Sub2API group
  entitlement, so a configured model can still be invisible to a given caller.
- The write is a `models`-only replacement. Any model-level fields a channel's
  `models` records carried beyond `name` and `alias` are replaced by the two-field
  records this tool writes; on the stock CPA image a models round trip is also known
  to drop `max-output-tokens` on `openai-compatibility` records. See
  [RECOVERY-2026-09-07.md](records/RECOVERY-2026-09-07.md).
