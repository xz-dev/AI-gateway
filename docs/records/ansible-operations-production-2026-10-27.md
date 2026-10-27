# Standard Ansible operations: production verification

Scope: `standardize-ansible-operations` tasks 5.1–5.2, run from the Podman controller on a direct OpenAI model. No production prerequisites were installed. Evidence remains in private controller scratch `/var/tmp/ai-prod-20261027/`.

## Check-mode matrix

The complete final matrix ran 2026-10-27 17:29–17:59 UTC: all 14 component profiles, all four config profiles (`aisix`, `egress-proxy`, `cpa-model-sync`, `model-catalog`), and `recovery.yml`. All 19 reported `changed=0`, `failed=0`, `unreachable=0`. Container IDs/names/images/states and the recursive `.ai-ops-state` name/type/size/mtime/mode listing were byte-identical before and after.

`docker-compose` was absent from PATH. The renderer directory was removed before the initial matrix, and again for a retained acquisition check: preflight fetched Compose 2.40.3 itself, verified SHA-256 `dba9d98e1ba5bfe11d88c99b9bd32fc4a0624a30fafe68eea34d61a3e42fd372`, and accepted the host's matching version. The acquisition check also reported `changed=0 failed=0`. No manually downloaded renderer was supplied.

Private check output was scanned against controller environment credentials and explicit API-key/token/password fields from private YAML; no credential values were found. Secret-bearing Compose and environment files emitted no content diff.

### One-time layout-header reconciliation

The first egress check legitimately reported a change: all 188 manifest entries matched except `squid.conf`, whose first generated comment still named `scripts/render-egress-policy.py` instead of `ops/scripts/render-egress-policy.py`. Policy hash and every behavior-bearing byte matched. A reviewed one-time `deploy-config.yml -e component=egress-proxy` applied the new header using the existing container-restart activation; the tooling has no reconfigure path. No renderer compatibility hack or drift bypass was added.

Egress returned healthy, restarts=0. Public CPA-routed `zcode/glm-5.3-flash` and AISIX-routed `glm-5.3-flash` Responses requests both returned 200. Subsequent egress check returned `changed=0 failed=0`, followed by the full no-change matrix above.

## Real model-catalog activation

An identical config apply is intentionally a no-op. To exercise the activation path, the private desired config received one header comment; all parsed configuration remained unchanged. The reviewed diff contained only that line, which remains in desired state (no second activation to remove it).

`deploy-config.yml -e component=model-catalog` succeeded. Only `ai-gateway-model-catalog-1` changed container ID; all other container IDs stayed identical. Model-catalog was healthy, restarts=0. Public models returned 200 with 367 rows; public Codex models returned 200 with 358 rows; trusted loopback `:9083/models-table` returned 200. Counts matched the pre-capture. The following config check reported `changed=0 failed=0`.

A temporary admin-owned Sub2API key in normal-client group 15 was used; no key value or public hostname is recorded here. Its cleanup proof is recorded with the subsequent CPA plugin maintenance record.
