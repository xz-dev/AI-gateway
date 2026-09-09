# models-table SSR production deployment — 2026-09-08

The user separately approved immediate manual production deployment, including
possible interruption of active inference/WebSocket connections while directory
APISIX was recreated. The change is deployed, not merely staged.

## Scope

Host `<tailscale-ip>`, project `/root/AI-gateway`.

Only these production files changed:

- `.env`: Go image reference only.
- `apisix-models/apisix.yaml`: one sidecar-only `/models-table` route; every
  existing route/upstream remained semantically identical to the fresh baseline.
- `model-catalog-sidecar/templates/default.conf.template`: proxy `/models-table`
  instead of serving the CSR document.

Go, directory APISIX, then sidecar were individually recreated with no
 dependencies, build or pull. No deployment orchestration script was used.
Production Compose and its override, source-chain configuration, credentials,
resources, CPA, Rust and existing inference/WS routes were unchanged.
Production `pull_policy: never` remains intact. The old static HTML and its
read-only mount remain as rollback material; no route serves that file. The
unused local CSR source was separately deleted after explicit confirmation.

## Artifacts

| Artifact | Identity |
|---|---|
| Go image | `localhost/models-enricher:ssr-20260908T063944Z` |
| Docker image ID | `sha256:e8b58607c18e0126653f7a4f57fc4b03acc2c731e94596b976484db8ffbc58d2` |
| Running executable SHA256 | `4dc7188f0cf3f544ab9efd15f1e9fa60d4fca69c76c9431ef6bc0215ec20ca5d` |
| Archive SHA256 | `877938a788adc2ec2a78140ccf2151642d60e6499bdbb20ff5441ef6c937c88c` |
| APISIX template SHA256 | `25f125db919fa76dc0249ced70d04d2d226cb3b87b2304bc7dfb14d2e1b38fcf` |
| Sidecar template SHA256 | `bccd7f5eead1448dc34665a89d67d437c475e05ee7a1313bcdd808402c5d3506` |
| Environment file SHA256 | `2be7527f620a5d4af4fdb8771b496badca67ebed3ec43d7f3690b273f42bbf2f` |
| Unchanged Go configuration SHA256 | `2282345fdd4d705e248641483b338a871aa2dd4f30d74e98d9237477cc0c3aee` |

The image was built offline using the prior deployed source plus reviewed
`main.go`, `table.go`, embedded `models-table.html`, and the unchanged current
production config. No dirty-worktree image or wholesale route/Compose file was
deployed. Archive layers/runtime image configuration, executable hash, mounted
files, rendered APISIX YAML and active sidecar configuration were verified.

## Bounded production acceptance

One HTML request and one same-version JSON request were captured on the
production host. HTML returned HTTP 200, **189 rows / eleven columns**, 110,362
bytes. All cells matched the JSON response, including ordering and missing-value
semantics. The JSON response was 8,711,480 bytes. No script or active model markup
was present. A non-sidecar request with forged forwarding headers returned
HTTP 404, not the complete HTML catalog.

HTML SHA256:
`c9a479742076f134456b5154d4f8ae6e2d67f9769d27178128ae3251a55ebeb6`.

Field-presence counts in this response: output modalities 76, input limit 20,
completion limit 0, output limit 76. This is a later live snapshot than the
previous source-chain deployment's 191 records; no immediate pre-switch catalog
was captured, so that historical difference is not attributed to SSR.

All 48 containers were running; all nine declared health checks were healthy.
Only the three target IDs changed; the other 45 IDs/restart counts were unchanged.
New target restart counts were zero. Environment, limits, security settings and
mount definitions matched baseline. Docker returned two bind lists in a
different order; their complete entries were identical.

The front `apisix` container already had `OOMKilled=true` in the fresh baseline.
It remained healthy, with the same ID and restart count zero before and after.
The first verifier incorrectly required every historical OOM flag to be false;
it was corrected to baseline equality for untouched containers. The three new
targets had no OOM flag. This is not proof of host-wide absence of OOM events.
The verifier also corrected order-sensitive bind comparison. Both corrections
used saved responses/snapshots; no HTTP acceptance request was repeated and no
service was changed to satisfy a verifier assumption.

No production inference, load, extra source fetching investigation or live
browser test was added. JavaScript-disabled browser and error-page behavior were
verified locally as described in [SSR verification](models-table-ssr-verification.md).

## Evidence and rollback

- Local: `/root/.cache/models-table-ssr-deploy-20260908/`.
- Production: `/root/AI-gateway/.models-table-ssr-20260908/`.
- `before/` contains fresh source files and metadata; inspect/effective-config
  snapshots are private and may contain credentials.
- `production-result.json`, `verify.py`, captured HTML/JSON/headers, image data,
  and switch logs retain the bounded evidence.

Rollback, if explicitly needed, restores only the two templates and `.env` from
this deployment's `before/`, preserving owner/mode. Recreate sidecar with its
retained static file, directory APISIX, and Go on
`localhost/models-enricher:output-fields-20260908T045911Z`, without dependencies,
build or pull. Do not restore older Compose, source configuration, CPA or DBs.

Backups and the old image are retained. The task deployment lock is released at
closeout. No commit, push, OpenSpec synchronization or archive was performed.
