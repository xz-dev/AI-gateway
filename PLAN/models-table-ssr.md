# Pure server-rendered models-table

User confirmed the following local implementation and existing-test scope on
2026-09-08, then separately authorized immediate manual production deployment
and its interruption risk. No commit, new service, source/config policy change,
or unrelated WS work was authorized.

## Agreed examples

- GET `/models-table` contains all eleven columns and complete sorted model rows
  in its HTTP HTML body, including with browser JavaScript disabled. No page
  script, hydration, or browser `/v1/models` request.
- Preserve count, fixed `client_version=v0.65.0`, independent token fields,
  missing/null/false/zero/empty-string/empty-array distinctions, and safe text.
- Reuse the existing Go catalog build and in-flight/input-cache behavior. Keep
  `/v1/models` JSON behavior and existing data-source policy unchanged.
- On catalog failure return HTML with the corresponding HTTP error status,
  rather than an empty success page. Restrict the new APISIX HTML route to the
  existing sidecar network peer; full inventory must not become publicly routed.

## Sequential slices

- [x] Add one HTTP-level unmet SSR example using existing catalog fixtures.
- [x] Render with Go `html/template`; replace static-page delivery with a
      sidecar-only APISIX route, without changing networks or runtime resources.
- [x] Verify existing browser states with scripts disabled; exercise the real
      offline APISIX/sidecar fixture, including non-sidecar rejection, JSON
      fidelity and error HTML. Use task-owned, network-none containers only.
- [x] Run the relevant Go regression, record scope/limits, stop task-owned
      processes and deliver. Do not expand verification after these gates pass.

Evidence: `/root/.cache/catalog-table-ssr-20260908/`.

Production handoff: `docs/models-table-ssr-deployment-2026-09-08.md`.
Three targeted services were manually recreated; bounded acceptance passed.
The old local CSR source was deleted only after explicit user confirmation.
