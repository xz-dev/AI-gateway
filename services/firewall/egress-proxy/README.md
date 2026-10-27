# Egress proxy

**Where:** Compose service `egress-proxy`, built from this directory (Debian, pinned Squid and Unbound). CPA and Sub2API reach it through their own relays (`cpa-squid-relay`, `sub2api-squid-relay`); it is the only container with an Internet route (`proxy-egress`).

**What:** a fail-closed allowlist proxy. Squid permits only the destinations, methods and paths in the rendered policy, and bumps or splices TLS per rule; Unbound resolves names for it.

**How:**

- `./ops/scripts/init-egress-proxy.sh` creates `data/egress-proxy/policy.json` from [`policy.example.json`](policy.example.json) and renders it.
- Re-render after edits: `python3 ops/scripts/render-egress-policy.py data/egress-proxy/policy.json data/egress-proxy/generated`.
- Production: `ansible-playbook ops.yml --tags deploy-squid`.
- Policy rules: [egress policy](../../../docs/egress-policy.md).
