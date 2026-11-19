# Superseded: stabilize-responses-path

Status: **closed as superseded, 2026-10-23**. This is not a claim that every
historical task or acceptance item passed. No old delta is synced to main specs.
The files stay in place so existing evidence links remain usable.

## Replacement

The deployed path is public APISIX → keepalive → pure cache keeper → stateless
WS/SSE bridge → Sub2API HTTP/SSE → transparent apisix-models → AISIX → selected backend.

- The keeper does not classify models or own provider credentials.
- Sub2API account WebSocket mode is off; no inner `/rk` ingress is revived.
- Current routing follows [model-routing.md](../../../docs/model-routing.md):
  apisix-models does not inspect bodies; AISIX selects exact → most specific glob →
  single `*` row to CPA. The former slash-based CPA/AISIX rule is historical,
  retired by the [2026-10-29 deployment](../../../docs/records/aisix-first-routing-fallback-2026-10-29.md).
  No slash/Lua inference selector, catalog lookup or `routing_lkg` fallback selects a backend.
- The retired `deploy-responses-path`/inner keeper operations must stay rejected.
  New single-service Ansible profiles do not reinstate those operations.

Authority: [WS termination decision and deployment evidence](../../../docs/websocket-termination.md),
[model routing](../../../docs/model-routing.md), and the current main specs.
The 2026-10-23 cold-start fix sets bridge `PORT=8086` to match `LISTEN_ADDR`;
it does not establish a second VM reboot or a rollback drill.

## Surviving work is separate

- Disk-full/slow-disk checks (#49/#55) must target the current pure cache keeper,
  including zero-as-unlimited capacity settings; the old routed candidate is not
  acceptance evidence for that implementation.
- Pi/plugin paired-host release and continuation acceptance remain with their
  release owner. Historical task 5.2/6.5 checkboxes are not changed here.
- Catalog refresh ECONNRESET/latency (#52) and quota acceptance (#10) require
  their own current-topology evidence.
- Long soak, production CPU profile, explicit rollback and VM cold-boot drills
  remain unperformed unless separately recorded. A real VM reboot is not
  authorized by this supersession.
