## 1. model-catalog

- [x] 1.1 Config (design D3): add the root `membership` block with `cpa_include`, compiled with the `include_models` matcher. Update the tracked `config.yaml` (commented example with the Devin reason) and the local private config (`membership.cpa_include: ["devin/*"]`). Verify: `tests/config.test.ts` covers absent, valid, empty list, non-string entry, bad pattern and unknown `membership` key, each with the field named in the error; `npm run typecheck` passes.
- [x] 1.2 Authority (D1, D2, D4): in `authority.ts`, replace `readIdentities`/`admitModels` with:
  - prefix discovery: key-channel prefixes, and OAuth `P/x` + `x` pairs from `auth-files/models`;
  - the `force-model-prefix` read;
  - the admission rule, passthrough, compatibility mode and fallback.

  Remove the `oauth-model-alias` and `model-definitions` reads, and rename the generation fields. Verify: `tests/authority.test.ts` covers every scenario in the `codex-models-enricher` delta, including:
  - a prefixed ID;
  - a bare ID;
  - `deepseek/…` rejected;
  - `devin/*` via `cpa_include`;
  - the `openrouter/auto` same-prefix original;
  - compatibility mode, which equals today's admission;
  - fallback with no bare IDs;
  - the force refresh bypassing the new reads.

  The old identity tests are removed or rewritten, not skipped.
- [x] 1.3 AISIX and build (D5): AISIX contributes every ID. `supplementalIDs` is removed and `build.ts` takes members in the D1 order. A CPA-admitted row wins over AISIX, and a non-admitted CPA bare row is never `@self`. Verify: `tests/catalog.test.ts` and `tests/authority.test.ts` cover:
  - an ID in both sources;
  - an AISIX bare ID that CPA also lists bare;
  - duplicates and case-distinct IDs;
  - an AISIX failure that keeps CPA membership.
- [x] 1.4 Parity fixture: add a test built from the shape of the 2026-10-27 production data. It uses synthetic IDs that keep each category and count ratio, not private values. Two CPA variants:
  - a forced-prefix manifest, prefixes only;
  - a default manifest, with bare copies and bare slash originals added.

  Verify: after `include_models`, both variants give the same member set and passthrough set, and the default variant admits no bare or unprefixed slash IDs outside `cpa_include`.
- [x] 1.5 Full suite and image. Verify:
  - `npm ci && npm run typecheck && npm test && npm run build` pass in `services/routing/model-catalog`;
  - `podman build` succeeds;
  - `ops/scripts/validate.sh .env.example` passes, with `compose.override.yaml` moved to `/var/tmp` and restored.

- [x] 1.6 Output order and Codex priority (D7): sort members once per generation in `prepare()` (bare IDs first, then the rest, each in code-point order); set Codex `priority` to the 1-based output position on every row, with a shallow copy for passthrough rows; `/models-table` uses the same comparator. Update `docs/model-catalog.md`. Verify:
  - `tests/catalog.test.ts` covers the comparator (bare first, case, code point beyond the BMP), the standard list and Codex manifest sharing the order, `priority` on compiled and passthrough rows replacing CPA's value, passthrough rows equal to CPA's except `priority` with CPA field order kept, and the generation's native row left unmodified;
  - the parity test still gives equal member and passthrough sets;
  - `npm ci && npm run typecheck && npm test && npm run build` and `podman build` pass.

## 2. CPA pin and docs

- [x] 2.1 Re-resolve the latest official CPA release and its index digest (`eceasy/cli-proxy-api`). Pin it in `compose.yaml`, `.env.example` and the private `.env`, and record the old digest for rollback. Verify:
  - the digest pulls;
  - `ops/scripts/validate.sh .env.example` passes;
  - no tracked file references `xz-dev/cli-proxy-api` outside `docs/records`.
- [x] 2.2 Docs:
  - `docs/model-catalog.md`: membership rule, `membership.cpa_include`, prefix discovery, compatibility mode; remove the identity-admission text;
  - `docs/catalog.md`;
  - `docs/add-openai-compatible-provider.md`: CPA lists both bare and prefixed IDs; the catalog admits prefixed IDs; how to add a `cpa_include` glob;
  - `docs/model-routing.md`: default `force-model-prefix`, and unprefixed slash IDs are served by the credential that registers them;
  - the model-catalog README.

  Verify: the local link check reports 0 broken links, and `git grep -nE 'supplementalIDs|oauth-model-alias|force-model-prefix: true'` hits only `docs/records` and `openspec/changes/archive`.
- [x] 2.3 Commit unsigned and push, then publish the model-catalog image with the current publish workflow and pin its digest. Verify: CI is green for the commit, the publish run succeeds, and the image pulls by digest.
- [x] 2.4 Commit 1.6 unsigned with explicit pathspecs and push, publish the model-catalog image again and pin its digest in `compose.yaml`, `.env.example` and the private `.env`. Verify: CI is green for the commit, the publish run succeeds, the image pulls by digest, and `ops/scripts/validate.sh .env.example` passes (with `compose.override.yaml` moved to `/var/tmp` and restored).

## 3. Production

Run every step on an `openai/*` model, using the Ansible playbooks current at apply time (`standardize-ansible-operations` may have replaced `ops.yml`).

- [x] 3.1 Pre-capture, read-only, to `/var/tmp`:
  - native manifest IDs, AISIX IDs, catalog IDs (standard list and Codex manifest bytes);
  - public `/v1/models` for a test key, Sub2API count, `:9083/models-table` status;
  - `GET /v0/management/force-model-prefix`, and CPA plugin log lines.

  Verify: every capture is non-empty and the setting reads `true`.
- [x] 3.2 Batch A: deploy the new model-catalog image with its private config. Verify:
  - healthy, restarts=0;
  - the member set and passthrough set equal the pre-capture, and Codex manifest rows match row for row;
  - public and Sub2API counts are unchanged;
  - logs report compatibility mode.

  Done 2026-10-28 with `v1.1.0-548d855`; 3.2.1 repeats it with the 1.6 image.
- [x] 3.2.1 Batch A re-run: refresh the pre-capture (3.1 list), then deploy the 2.4 model-catalog image. Verify:
  - healthy, restarts=0, logs report compatibility mode;
  - the member set and passthrough set equal the pre-capture, and every Codex row equals its pre-capture row in every field except `priority`;
  - the standard list and Codex manifest follow the D7 order, and every Codex `priority` equals its 1-based position;
  - public and Sub2API counts are unchanged.
- [x] 3.3 Batch B: recreate CPA on the official image with `stream-bootstrap-buffering: true` (D6), the setting still `true`. Right after the image switch, deploy the D9 AISIX change with `deploy-config.yml -e component=aisix` (`--check --diff` first; the diff removes only `devin/deepseek-v4-1-flash` and its route target). The first attempt on 2026-10-28 was rolled back on the old "catalog unchanged" gate. Verify:
  - CPA reports `stream-bootstrap-buffering` on;
  - CPA healthy; one CPA-routed and one AISIX-routed inference succeed, and the `deepseek-v4.1-flash` route still answers;
  - D8 ledger:
    - the native Devin difference equals the embedded `devin_models.json` difference between the old and new tags exactly (+7/-17 for v8.0.38 → v8.0.45);
    - the catalog difference is +7/-16 before the AISIX deploy and +7/-17 after it; every AISIX target registered by CPA at pre-capture remains registered after B except the accepted ledger removals handled by D9, and no new unregistered target appears. Record pre-existing unregistered targets separately; this change does not delete unrelated stale or disabled-channel targets;
    - every other member, the passthrough set and every non-`priority` field of every other Codex row equal the pre-capture;
    - the order and `priority` follow D7;
    - a model missing from CPA's available list but still registered is re-sampled after the authority cache refresh before it counts as a difference;
  - public and Sub2API counts change only by ledger IDs;
  - a `supergrok/grok-imagine-image` images request reaches the xAI upstream, with no CPA `not supported`/`unknown provider` error. Use the cheapest request that proves routing;
  - both `cpa-quota-estimator` and `codex-auto-reset` log `plugin loaded` (a load failure is a regression: roll back);
  - the D6 SSE framing gate passes on the CPA operator port and on the public chain.

  - [x] Re-run evidence (2026-10-28): official image healthy/restarts=0; buffering true; both plugins loaded; native +7/-17 ledger, catalog +7/-16 then +7/-17; unchanged passthrough and all other non-priority fields; D7 order; no new dangling AISIX targets; CPA, AISIX and DeepSeek route probes 200; xAI image request 200; operator/public SSE 11/10 valid frames. Devin credential remains registered and returns its upstream quota failure, not a successful inference.
  - [x] Devin success after reset: at 2026-10-28 08:06:46 UTC, `devin/kimi-k2-7` returned 200 with content through the public chain. CPA's selector log identifies the same sole Devin credential as the pre-B, B and C captures. The earlier 429 quota responses were not counted as success; this fresh probe closes the explicitly deferred gate. Temporary key 35 was deleted (DELETE 200, GET 404, credential file absent).

  If the SSE gate fails with buffering on, or any difference is not explained by the ledger, roll back to the previous CPA digest (and `rollback-config.yml -e component=aisix` if it was deployed) and stop for a user decision.
- [x] 3.4 Batch C: `PUT /v0/management/force-model-prefix {"value": false}`, then `POST :9083/models-table/refresh`. Verify:
  - the setting reads `false` and `config.yaml` persists it;
  - the native manifest now contains bare IDs;
  - the catalog member and passthrough sets equal the post-3.3 capture, with any difference listed and explained;
  - every Codex row equals its post-3.3 row in every field except `priority`, and the order and `priority` follow D7;
  - public and Sub2API counts are unchanged;
  - CPA logs show the same credential for one prefixed ID and one `devin/*` ID as before;
  - a bare ID request reaches AISIX.
  - [x] Re-run evidence (2026-10-28): setting false through API and persisted config; native 554 IDs including 157 bare; catalog 357, public standard/Sub2API 357 and public Codex 348; member/passthrough sets and all non-priority fields unchanged; D7 order/priority; prefixed credential identity unchanged, Devin registered identity unchanged with the same quota failure, and bare request logged by AISIX.
  - [x] Devin success after reset: the fresh temporary normal-client key 35 served `devin/kimi-k2-7` with 200 and content at 2026-10-28 08:06:46 UTC, with force-model-prefix=false. CPA's selector log matches the original Devin credential identity. Key deletion was verified; see the record's "Devin success after reset" section.
- [x] 3.5 Write `docs/records/union-catalog-membership-2026-10-28.md` with the captures summary, digests, timings, the D8 ledger, the D9 removal, the first batch B rollback and rollback commands. Delete the temporary test key with proof, and confirm `--check --diff` reports changed=0 for cli-proxy-api, model-catalog and the configs. Commit unsigned and push. Verify: CI is green, and `openspec validate --all --strict` passes.
