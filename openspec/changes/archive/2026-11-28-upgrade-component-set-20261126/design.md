## Decisions

- **Gated artifacts.** Record immutable manifests and use the existing fork publishing and compatibility ladder. Port the actual deployed fork sources, not fork default branches. Keep capacity, terminal SSE, routing/error propagation, plugin ABI, and frontend/middleware compatibility.
- **Single promotion.** Prepare every changed selection, run the ladder on the combined pins, and publish the resulting lock only after a green run. Retain all old images and identify rollback prerequisites. No intermediate push to `main`.
- **Scoped deployment.** Use fresh guarded previews and digests; use backed-up manual windows only where no profile covers the complete change. Switch the AISIX v2 file and the new image together, and restore the old file and image together on rollback. Redis may rebuild its cache; stop it before taking its pre-switch persistence backup, and prove that the old image can open that backup. PostgreSQL and Sub2API migrations require an isolated round-trip that preserves durable data. Preserve other private settings. If registry verification fails, stop, not override.
- **Verification.** Prove selected image IDs, readiness, unaffected service identities, and authorized inference (including terminal stream behavior) after each activation. If a stop gate fires after an apply, follow the approved rollback procedure and publish only observed results.

## Risks

Restarting front components can interrupt active streams. Redis entries written after the version switch may be lost if reverting to the pre-switch cache, as accepted by the operator. PostgreSQL and Sub2API data must survive the planned rollback. A newer upstream fork base may overlap downstream capacity and routing changes; resolve only with focused regressions and a green combined ladder.
