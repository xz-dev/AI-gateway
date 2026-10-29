# AISIX

**Where:** Compose service `aisix` in the `aisix-netns` namespace. It has no Internet or Squid edge; it reaches CPA only through `aisix-cpa-relay`. Its callers are Sub2API, apisix-models and model-catalog (read-only), each through its own relay.

**What:** [AISIX](https://github.com/api7/aisix) maps logical model IDs to ordered lists of concrete CPA targets, with fallback and cooldown. Its single `*` row is the priority fallback: any ID without an exact row, route or narrower glob goes to CPA unchanged. Compose runs the official release image pinned by `AISIX_IMAGE`. This directory holds the public examples `config.example.yaml` and `resources.example.yaml`, plus a patched source build ([`Dockerfile`](Dockerfile), [`VERSION`](VERSION), [`patches/`](patches/)) that `publish-aisix-images.yml` can publish when a fix is not yet in a release.

**How:**

- `./ops/scripts/init.sh` creates the private `config.yaml`, `resources.yaml` and `caller-keys.json` here (gitignored).
- Edit the private `resources.yaml` with concrete targets and logical routes, then deploy it with `ansible-playbook playbooks/deploy-config.yml -e component=aisix` from `ops/ansible/`.
- `config.yaml` sets `upstream.stream_timeout_ms: 120000`, the longest silence allowed between upstream stream chunks (60 s cut long Claude/Codex generations with 504). No playbook manages `config.yaml`. Change it in a manual window: back it up, edit it, then `docker compose up -d --no-deps --force-recreate aisix`.
- Guides: [upstream configuration](../../../docs/upstream-configuration.md), [add an OpenAI-compatible provider](../../../docs/add-openai-compatible-provider.md).
