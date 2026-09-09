# CPA compatibility

**Final candidate passes with serialized writes.** Parallel CPA writes exposed
the target's known read-back bug. The synchronizer now preserves parallel fetching
but serializes its fresh-read/compare/write/confirmation phase. The rebuilt release
image passes the full probe; run one active synchronizer per CPA. See
[VALIDATION.md](VALIDATION.md) for final evidence and remaining limitations.

Local implementation target: the repository Compose default, **not a production runtime claim**.

- Gateway baseline: `d216a9f95e3e799ae3643b34117bb34dd35abe92` (clean before implementation).
- Image: `ghcr.io/xz-dev/cli-proxy-api:v7.2.146-xz.1`.
- Immutable digest: `sha256:aaab5960f930abbfd9f1e69b23126c97fb33da93597bd0ab97a561e71ba257ef` (linux/amd64).
- OCI source revision: `0f10a3635c39984d9eae139a4d7b3759f51ec6d0`, `xz-dev/CLIProxyAPI`.
- Earlier official planning reference `5208aec703b5ce7e3445f6e9d91cc13b3e78003a` is a different revision; its behavior is not substituted for this image's results.
- Previously recorded CI runs `34011902824` and `33957075854` both failed `default egress policy differs from the exact control-plane baseline`; this change has not repaired that unrelated failure.

## Isolated results

| Kind | Management GET | models-only PATCH + GET | Sync ownership |
| --- | --- | --- | --- |
| openai-compatibility | 200 | Replaced, disabled preserved | Rust |
| claude-api-key | 200 | Replaced | Rust |
| codex-api-key | 200 | Replaced | Rust |
| xai-api-key | 200 | Replaced | Rust |
| vertex-api-key | 200 | Replaced | Rust |
| gemini-api-key | 200 | HTTP 200 **but models unchanged** | Existing Go behavior retained |
| interactions-api-key | 200 | HTTP 200 **but models unchanged** | Existing Go behavior retained |

The two unsupported PATCH structs omit `models` in the exact source's
`internal/api/handlers/management/config_lists.go` (`PatchGeminiKey`, `PatchInteractionsKey`).
Do not interpret HTTP 200 alone as proof of a models replacement.

Disabled OpenAI-compatible providers are skipped by `synthesizeOpenAICompat` in
`internal/watcher/synthesizer/config.go`; discovery therefore has no live
`auth-index` for them. The approved fallback uses credentials returned by CPA
**only in memory**, in CPA's own `api-call` request headers. It neither enables
the provider nor connects directly to the upstream. Keys must not be persisted,
logged, or put into the synchronizer's policy configuration.

The actual Rust `--once` executable was tested inside the target image with only
synthetic configuration and a loopback mock upstream. Five kinds, including two
OpenAI-compatible providers (enabled/disabled), yielded:

```text
first:           updated=6 unchanged=0 failed=0
second:          updated=0 unchanged=6 failed=0
after CPA restart: updated=0 unchanged=6 failed=0
```

GET snapshots verified other fields and the unmanaged Gemini/Interactions
providers remained unchanged. The restart verifies configuration persistence.
The HTTP tests separately check the native request headers, client_version,
path override, page completion, and filtering semantics.

## Reproduce locally

From this directory (requires local Cargo, Go and Podman):

```sh
cargo build --locked
CGO_ENABLED=0 go build -o target/real-cpa-probe tests/real_cpa.go
podman run --rm --network=none --read-only --cap-drop=ALL \
  --security-opt=no-new-privileges --tmpfs /tmp:rw,nosuid,nodev,size=64m \
  -v "$PWD/target/real-cpa-probe:/compat-probe:ro" \
  -v "$PWD/target/debug/cpa-model-sync:/rust-sync:ro" \
  --entrypoint /compat-probe \
  ghcr.io/xz-dev/cli-proxy-api@sha256:aaab5960f930abbfd9f1e69b23126c97fb33da93597bd0ab97a561e71ba257ef
```

The probe runs **inside the disposable container**, creates random synthetic
credentials in its tmpfs, discards CPA logs, and reports only assertion outcomes.
It has no external network or host port and mounts no real configuration. The
probe initially tests all seven PATCH endpoints to preserve the compatibility
matrix; the Rust executable itself discovers and writes only the five managed
kinds. No Core or production change is involved.
