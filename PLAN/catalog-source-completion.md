# Zcode, XL and Congee source completion

The user selected and authorized local implementation plus manual production
deployment of this bounded configuration change on 2026-09-08.

## Scope

- Append `models.dev/zai` to Zcode, after both existing sources.
- Append `models.dev/tencent` to XL, after all existing sources.
- Configure Congee with `fetch_models: false` and `[models.dev/openai]`.
- Keep runtime Go logic, other channels, OAuth, GMI, Commandcode mappings,
  static inheritance, SSR, images, resources and credentials unchanged.
- Source metadata may overwrite lower-priority same-key values, including
  context. Coverage and declared limits do not prove channel capability.

## Acceptance

- [x] Existing configuration tests fail before the change, then pass.
- [x] Real source adapters and merge compare old/candidate configs on the same
      captured catalog/cache inputs: thirteen selected exact hits, earlier
      source priority retained, same-key changes visible, membership unchanged.
- [x] Congee makes no own-inventory request; source failure/miss semantics remain
      those already implemented. No cross-token filling or guessed lookup IDs.
- [x] One Go regression: 151 passing tests/subtests under race, one existing
      opt-in container-test skip; vet, formatting and four-file scope checks pass.
- [x] Production config atomically installed; only Go recreated, without build
      or pull. One catalog acceptance passed; receipt and rollback copy retained.
      The task lock was released after saving the deployment receipt.

Evidence: `/root/.cache/catalog-source-completion-20260908/`.
OAuth, GMI, Commandcode mappings, old main-spec cleanup and further SSR checks
are not part of this work.

## Result

The fixed replay keeps all 189 members. Output-limit/modality presence changes
from 76 to 89, input-limit presence from 20 to 26, and no completion-limit value
is synthesized. Exactly thirteen rows change the checked fields. Earlier source
priority and all out-of-scope channel records are retained.

The replay uses the saved admitted/enriched catalog as its channel-metadata
baseline and the real cached bulk-source JSON. An own-inventory cache lookup
miss prevented including that raw response; no new request was made to fill it.
This is a real adapter/merge comparison, not a fresh Management/network replay.
The configured Congee HTTP tests separately verify zero own-inventory requests
and baseline fallback on source failure. A missing logger argument in the first
replay-test compile was corrected; its failed log is retained.

Production acceptance on 2026-09-08 returned HTTP 200 with 189 members and all
thirteen selected completions. Observed field counts are output/modality 89,
input 26, completion 0. Membership matches the saved snapshot; this is not an
atomic pre/post upstream observation or evidence of channel capacity.

All 48 containers are running and nine health checks are healthy. Only Go was
recreated; the other 47 IDs/restart counts are unchanged. Image, binary,
environment, effective Compose, resources and mount definitions are unchanged.
The front APISIX historical OOM flag remains true in both snapshots, not a new
incident attributed to this change.

- Active config SHA256: `a3ca2b0dcc3145da10cff7809e263b82b6893f51ee66cf8ca4a65d39c1c11d66`
- Go container: `aaa77699484526a9ad0c2ced5c53d93f3808d25e096d7f3b5127c2bd38c46ecc`
- Unchanged image: `localhost/models-enricher:ssr-20260908T063944Z`
- Remote evidence: `/root/AI-gateway/.catalog-source-completion-20260908/`
- Rollback, only if needed: restore `config.before.yaml` with original ownership
  and mode, then manually recreate only Go with no dependencies/build/pull.
  Do not restore Compose, other services or databases.
