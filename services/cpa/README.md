# cpa

**Where:** `services/cpa/`. CLIProxyAPI runs as Compose service `cli-proxy-api` in the `cpa-netns` namespace, published on `127.0.0.1:8317`.

**What:** [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) (CPA) holds provider credentials and executes requests against provider pools. It reaches the Internet only through the Squid egress proxy.

| Directory | Contents |
|---|---|
| [`config/`](config/README.md) | Public CPA config sample copied by `ops/scripts/init.sh` |
| [`cpa-model-sync/`](cpa-model-sync/README.md) | Rust worker that keeps the model membership of five API-key channel kinds in sync |

**How:** add provider accounts and channels as described in [upstream configuration](../../docs/upstream-configuration.md) and [add an OpenAI-compatible provider](../../docs/add-openai-compatible-provider.md).
