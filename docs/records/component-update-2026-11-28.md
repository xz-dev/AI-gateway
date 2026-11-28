# Production component rollout — 2026-11-28 UTC

Three image-only changes completed in one maintenance window, one container at a time. Two earlier attempts stopped at the connection check before any apply. The operator then explicitly approved the active-traffic window and accepted client reconnects. Three established-connection samples were recorded before each apply; the front plane had 26–27 established connections and the model plane had 4–7. Client interruption duration was not measured.

| Component | Previous → live | Apply completed / accepted (UTC) | Acceptance |
| --- | --- | --- | --- |
| ZCode provider sidecar | 4.9.0 → 4.9.1 (`7d6dd25e7089…`) | 13:24:18 / 13:24:37 | Exact image and readiness verified. Model directory still lists `zcode/glm-5.3-flash`. Its production Responses request returned HTTP 429 before switching, immediately after, and after the full rollout. This is not a successful ZCode inference check. |
| CLIProxyAPI | 8.1.7 → 8.1.8 capacity/SSE fork (`88eb8c49ee77…`) | 13:27:43 / 13:27:56 | Source `53ed842d1`; both installed plugins registered in production (`codex-auto-reset` 0.3.0, `cpa-quota-estimator` 0.21.3). |
| AISIX | 1.12.0 → 1.12.1 capacity fork (`379823766537…`) | 13:31:22 / 13:31:33 | Source `8f8e59bd`; existing v2 resources unchanged. The final `glm-5.2` Responses smoke completed with nonempty output. |

Every apply used a new `--check --diff` plan and its own approval digest. Each verified the target image, runtime environment, readiness, namespace owner, and that the other 47 containers kept their identities and state. Authorized `/v1/models` returned 200 and the short `gpt-6.2-astra` Responses stream ended at `response.completed` with nonempty output after every step. Model count stayed 402 during the window; that is not a per-model inventory comparison. Existing client authentication was used; no temporary key was created.

Final readback: 48 containers running, all 13 defined Docker healthchecks healthy, zero restarts on the three new containers, matching controller/live input hashes, writer lock clear, and host recovery running. Fresh previews for all three reported no activation needed. No rollback was needed. Old images and protected guarded snapshots were retained; a rollback must use a fresh preview and respect later transactions, not reuse an earlier digest or restore an entire old `.env` over newer pins.

## Candidate validation

- Standard combined ladder `20261128T114547Z-3639829` and CPA-only intermediate ladder `20261128T115153Z-3702608` passed 10/10 each, through 256 MiB. All seven bounded containers reported `memory.swap.max=0`. The standard compatibility lock is generated from the combined run; the nightly lock was not changed. An earlier green but swap-enabled local run was not used.
- CPA focused Go tests passed; the exact published image loaded and unloaded both installed plugin assets in an offline fixture. [CPA publish run](https://github.com/xz-dev/CLIProxyAPI/actions/runs/46098782951).
- AISIX workspace: 5,434 passing tests. Two unchanged integration targets (`body_spool`, `upload_deadline`) failed in fixture deserialization because their synthetic provider used `secret` instead of `api_key`. Both targets passed 16/16 in a separate candidate worktree with only those fixture keys corrected. No runtime or published image bytes were changed by the diagnostic; repair the fork fixtures separately so the normal suite exercises these paths again. [AISIX publish run](https://github.com/xz-dev/aisix/actions/runs/46102459157).
- The template and strict OpenSpec checks passed. No nginx was deployed: the old selector is not referenced by the running Compose topology. The ZCode selector is private, not a tracked template default.
