# Configuration

Policy file for `cpa-model-sync`. It is JSON, read from the path given on the
command line, or from stdin when the path is `-`. Unknown fields anywhere in the
document are rejected (`deny_unknown_fields`), as are trailing commas and comments.

See [config.example.json](../config.example.json) for the checked-in example, and
[docs/architecture.md](architecture.md) for what each value does at runtime.

## Schema

The block below shows the checked-in [config.example.json](../config.example.json)
with the optional keys added. `interval_seconds` and `requests` are absent from the
example and therefore take their defaults there.

```json
{
  "cpa_url": "http://cli-proxy-api:8317",
  "client_version": "0.101.0",
  "interval_seconds": 600,
  "requests": { "retries": 3, "retry_delay_ms": 1000, "timeout_ms": 30000 },
  "skip_channels": ["gmicloud"],
  "channels": {
    "nim": {
      "include": [
        "^minimaxai/minimax-m3$",
        "^moonshotai/kimi-k3$",
        "^deepseek-ai/deepseek-v4-flash-0731$"
      ]
    },
    "shuaiapi": { "include": ["^claude-"] },
    "cline": {
      "include": [
        "^z-ai/glm-5\\.2$",
        "^z-ai/glm-5\\.3$",
        "^z-ai/glm-5\\.3-flash$",
        "^moonshotai/kimi-k3$",
        "^deepseek/deepseek-v4\\.1-flash$"
      ]
    }
  }
}
```

| Key | Type | Required | Default | Validation |
| --- | --- | --- | --- | --- |
| `cpa_url` | string | yes | - | Used as `{base}/v0/management/{kind}`; trailing `/` is trimmed. No scheme validation. |
| `client_version` | string | yes | - | Sent percent-encoded (every non-alphanumeric byte escaped) as the `client_version` query parameter for `openai-compatibility` and `codex-api-key` only. |
| `interval_seconds` | integer | no | `600` | Must be non-zero, and `now + interval` must not overflow. Whole seconds only. |
| `requests.retries` | integer | no | `3` | `0..=30`. Total attempts per operation = `retries + 1`. Checked before the budget multiply, so it can never overflow. |
| `requests.retry_delay_ms` | integer | no | `1000` | `retry_delay_ms * 2^retries` must fit in a 64-bit unsigned integer (checked, not wrapping). |
| `requests.timeout_ms` | integer | no | `30000` | Must be non-zero. Global timeout for each CPA HTTP request, covering the whole request including the body read; not per retry attempt. |
| `skip_channels` | string array | no | `[]` | Entries are compared exactly against discovered CPA prefixes. |
| `channels` | object | no | `{}` | Keys are exact CPA prefixes. Each value is a filter object below. |

`requests` is itself unknown-field-checked and every one of its three keys is
individually optional; `"requests": {}` means all defaults.

### Filter object (`channels.<prefix>`)

| Key | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `include` | string array | no | `[]` | Keep an ID when this list is empty, or when at least one pattern matches. |
| `exclude` | string array | no | `[]` | Drop an ID when at least one pattern matches. Wins over `include`. |
| `path` | string | no | absent | Replaces the upstream inventory path for this channel. |

Every filter key is optional, so `{}` is a valid filter meaning "no filtering".
An absent `channels.<prefix>` entry behaves exactly like `{}`.

## Validation and when it is reported

| Problem | Reported as |
| --- | --- |
| Unreadable or non-JSON policy | startup error, exit 2 |
| Unknown field anywhere | startup error, exit 2 |
| Missing `cpa_url` or `client_version` | startup error, exit 2 |
| `timeout_ms` zero, `retries` > 30, or overflowing retry budget | startup error, exit 2 |
| `interval_seconds` zero or overflowing | startup error, exit 2 |
| Missing or empty `CPA_MANAGEMENT_KEY` | startup error, exit 2 |
| Invalid regex in any `channels.*` filter | **`--preview`: startup error, exit 2.** Daemon/`--once`: a per-channel `failed` result with reason `invalid include regex` / `invalid exclude regex`, exit 1 for `--once` |

The last row is the one asymmetry: `--preview` validates every configured filter
before it reads anything, while the daemon compiles a channel's regexes when that
channel is prepared, so an unused or never-discovered channel's bad regex does not
stop the daemon.

`--preview` is also all-or-nothing about discovery: a failed kind read or a channel
with an unusable `prefix` aborts the whole preview with exit 2, whereas the daemon
records those as per-channel failures and keeps going.

## Filter semantics

1. **Key = exact CPA `prefix`.** The channel key is matched against the discovery
   entry's `prefix` field - not the display `name`, not the provider/vendor. A
   prefix with no entry is unfiltered; an entry whose prefix matches no discovered
   channel is simply unused.
2. **Rust `regex` crate.** Unanchored, so a pattern matches anywhere unless you add
   `^` / `$`. Case-sensitive unless you use an inline flag such as `(?i)`.
   Backslashes must be JSON-escaped: `"^z-ai/glm-5\\.2$"` is the regex
   `^z-ai/glm-5\.2$`.
3. **Matched against the raw upstream model ID**, before any CPA prefix is added.
   An upstream `claude-opus-5` on prefix `shuaiapi` is matched as `claude-opus-5`,
   never as `shuaiapi/claude-opus-5`.
4. **Keep rule:** `(include is empty OR any include matches) AND NOT any exclude matches`.
   `exclude` wins over `include`.
5. **Empty result is written as `models: []`.** This means zero *configured* models,
   not necessarily zero runtime models: CPA's native fallback can still expose
   models. An *upstream* inventory that is empty is an error, never an empty write.
6. **Order is not a policy concern.** The desired set is sorted before it is written,
   so policy order and upstream order do not affect the written array or the
   comparison result.

### `skip_channels`

Listed prefixes are discovered but never fetched and never written. Skipping is not
a filter: it does not disable the channel, does not clear existing models, and does
not remove them from CPA. A skipped channel is counted and logged as `skipped`, never
as updated, unchanged or failed. The name is a list of prefixes, so it is exact -
`"gmi"` does not skip `gmicloud`.

### `path`

Replaces the whole upstream request path. The value is normalized to a single
leading `/`. Default path is `/v1/models`, plus `?client_version=<value>` for
`openai-compatibility` and `codex-api-key`. If the channel `base-url` ends with
`/v1` and the path starts with `/v1/`, every leading `/v1` is stripped from the
path, so `base-url: https://host/v1` with `path: /v1/special` requests
`https://host/v1/special`. A `path` containing a query string is sent as-is; no
`client_version` is appended to an explicit `path`.

## Worked examples

Each row is one upstream inventory evaluated under one filter.

| Filter | Upstream ID | Result | Why |
| --- | --- | --- | --- |
| `include: ["^A$"]` | `A` | keep | anchored include matches |
| `include: ["^A$"]` | `AA` | drop | `$` blocks a prefix match |
| `include: ["^A$"]` | `a` | drop | case-sensitive |
| `include: ["^A$"]` | `A:preview` | drop | `$` blocks a suffix match |
| `include: ["^A"]` | `A:preview` | keep | unanchored tail is fine |
| `include: ["^claude-"]` | `claude-opus-5` | keep | prefix match |
| `include: ["^claude-"]` | `my-claude-opus-5` | drop | `^` requires the start |
| `include: ["-preview$"]` | `A-preview` | keep | unanchored head, anchored tail |
| `include: ["-preview$"]` | `preview-A` | drop | tail anchor not satisfied |
| `include: ["^A"], exclude: ["-preview$"]` | `A` | keep | include matches, exclude does not |
| `include: ["^A"], exclude: ["-preview$"]` | `A-preview` | drop | exclude wins over include |
| `include: ["^A"], exclude: ["-preview$"]` | `B-preview` | drop | exclude alone is enough |
| `include: [], exclude: [".*"]` | anything | drop | exclude matches everything; writes `models: []` |
| `include: ["(?i)^claude-"]` | `CLAUDE-Opus-5` | keep | inline flag makes it case-insensitive |
| `include: ["^shuaiapi/claude-"]` | `claude-opus-5` | drop | filters see raw IDs, never prefixed ones |
| `{}` (or no entry) | anything | keep | no filtering configured |

## Common mistakes

- **Putting `include`/`exclude` in `models-enricher`.** The TypeScript enricher has no
  channel `include`/`exclude`; `validate-config` and startup reject them as legacy
  fields. Membership filtering belongs here. The enricher only enriches members Rust
  has already configured.
- **Confusing `models-enricher`'s `metadata.include_models` with this policy.** Its `!`
  entries hide *public* IDs from the catalog after admission. They do not change
  membership, do not stop a model from being configured, and do not affect
  routing.
- **Expecting `skip_channels` to remove models.** It only stops fetching and writing.
  Existing `models` stay in CPA and the channel stays enabled. To actually remove
  models, filter them out (`exclude`), which writes `models: []` or a smaller set.
- **Forgetting `^` and `$`.** Patterns are unanchored: `include: ["claude"]` also
  keeps `my-claude-clone` and `claude-x-preview`. Anchor the include list.
- **Matching prefixed IDs.** The policy never sees `prefix/model`. A pattern like
  `^nim/` matches nothing.
- **Forgetting JSON escaping.** A single `\.` in JSON is an invalid escape; write
  `\\.` for a literal dot in the regex.
- **Assuming an empty result disables the channel.** `models: []` is a full
  replacement with zero configured models; CPA's native fallback and the channel's
  enabled state are untouched.
- **Editing credentials, `disabled`, `prefix`, `base-url`, `proxy-url` or
  `excluded-models` here.** None of those exist in this schema and none are written;
  the PATCH replaces only `models`.
- **Relying on policy order or upstream order.** The written array is sorted by
  model record, so reordering the policy or the upstream response changes nothing.
