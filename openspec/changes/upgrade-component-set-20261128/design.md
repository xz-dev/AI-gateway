## Decisions

- Select published CLIProxyAPI v8.1.8 and AISIX v1.12.1 fork artifacts by digest and revision. Reuse the current AISIX v2 configuration. Verify both the final pair and the intermediate state with only CLIProxyAPI upgraded. Require disk-backed, isolated HTTP and WebSocket checks through 256 MiB with runtime `memory.swap.max=0` for every bounded container.
- Recreate `provider-sidecar`, then `cli-proxy-api`, then `aisix`. Recheck all baselines and registry identity with a fresh guarded preview of each step. Retain the previous image and transaction snapshot. Stop the sequence on a failed gate; inspect live receipts and use guarded rollback only when its preview proves the correct snapshot.
- Check exact running image, readiness, namespace and unchanged surrounding containers after each activation. Use the existing authorized client to verify model inventory and a short, nonempty general Responses completion. Verify the ZCode model is visible and compare its bounded inference result with a fresh pre-change baseline. An identical pre-existing HTTP 429 alone is not a new regression; report it separately and do not claim ZCode inference succeeded.
- Use a fresh live copy of `.env` and both Compose files. Only the selected image entry may differ per guarded plan. Preserve the existing managed AISIX configuration and CPA plugin/credential state; do not hot-reload plugins.

## Risks

A restart can interrupt in-flight requests even if all other container IDs stay unchanged. CPA's scheduler and stream code changed upstream, so exercise the existing native plugin assets and the streaming gate. AISIX changes failure-body telemetry; do not enable content capture as part of this upgrade.
