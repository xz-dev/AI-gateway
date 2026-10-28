## Context

See proposal.md for the motivation. Facts below are from read-only production checks on 2026-10-27.

- **CPA**: image `ghcr.io/xz-dev/cli-proxy-api:v8.0.38-pr6760-…`, live config `routing.force-model-prefix: true`, `config-version: 8`.
  - The native manifest lists 331 IDs: 274 start with a CPA prefix, 57 are `devin/*`, none are bare.
  - The Devin OAuth account has no prefix; its originals contain `/`.
  - Every other OAuth account (codex, kimi, xai) and all 18 key channels have prefixes.
- **AISIX** lists 92 IDs:
  - 66 carry a CPA prefix;
  - 26 are bare logical IDs or `devin/*` direct models;
  - 46 of the 92 are also in CPA's manifest.
- **Catalog**: 367 members after `include_models`:
  - 274 prefixed CPA IDs, 57 `devin/*` IDs, 46 AISIX supplement IDs and the static models;
  - minus the image IDs that `include_models` hides.
- **CPA v8.0.44 behavior**: `applyModelPrefixes` (`sdk/cliproxy/service_models.go`) registers each model of a prefixed credential twice when `force-model-prefix` is false: as `x` and as `P/x`.
  - After the switch, the native manifest also lists every bare original, including upstream originals with `/` such as `deepseek/deepseek-v4.1-flash`.
  - Under the current rules, two things would break:
    - `admitModels` admits every ID without `/`, which leaks bare originals into the catalog;
    - `supplementalIDs` drops AISIX bare logical IDs that CPA now also lists.
- **CPA Management API (v8.0.44)**:
  - `GET /v0/management/auth-files` does not return OAuth prefixes;
  - `GET /v0/management/auth-files/models?name=` returns each account's registered IDs;
  - key-channel endpoints return `prefix`;
  - `GET/PUT /v0/management/force-model-prefix` reads and persists the setting.
- **Routing today**:
  - `routing_selector.lua` sends IDs without `/` to AISIX and IDs with `/` to CPA.
  - Sub2API's `cli-proxy-api` alias points at `apisix-models`, not CPA.
  - All 69 AISIX direct models and every logical-route target name a prefixed or `devin/*` ID.
  - No client path sends a bare ID to CPA.
- **Upstream patches**:
  - #6189 (xAI image routing under forced prefixes) is still open, but upstream commits 45ccd9c3 and 8a3e7622 (in v8.0.44) cover the same case: `isXAIImagesModel` accepts an xAI-registered `prefix/canonical` ID.
  - #6760 (Codex SSE blank-line event boundaries) is still open; v8.0.44's `codex_executor_stream.go` has no equivalent. v8.0.39's 9e79552f only holds `response.metadata` while `stream-bootstrap-buffering` is on, and production has it off.
- **First batch B attempt (2026-10-28, rolled back)**: the official v8.0.45 image passed health, plugin, buffering and both SSE gates, but failed "catalog unchanged". Source-proven causes (private diagnosis under `/var/tmp/ai-union-20261028/`):
  - Devin: CPA builds the Devin list from its embedded `internal/registry/models/devin_models.json` (`registry.GetDevinModels()`), not from the account. The file has 56 IDs at v8.0.38 (`0db19025`) and 46 at v8.0.45 (`ad8bdd3f`); after CPA's normalization and the built-in `devin/swe-1-6-slow` they give exactly the captured native Devin sets (57 and 47). The difference is +7/-17.
  - One removed ID, `devin/deepseek-v4-1-flash` (renamed upstream to `devin/deepseek-v4-1-flash-1017`), is still an AISIX direct model, so the catalog changed by +7/-16 and that member lost its CPA base row.
  - `priority`: CPA sets every non-template Codex row to `max(template priority) + 100 * (rank + 1)`, ranked by lowercase `display_name` then `slug` (`applyCodexClientNonTemplatePriorities`, byte-identical in both tags). Any change to the native set shifts the priority of most rows; 273 rows changed only `priority`.
  - A model briefly unavailable in CPA's available-model list (still registered) produced extra transient differences that disappeared on re-sampling.

## Goals / Non-Goals

**Goals:**
- The catalog member set is the same before and after the CPA switch, apart from documented, explained differences.
- Membership does not depend on CPA's `force-model-prefix`, including after a rollback that turns it back on.
- Fewer Management reads, and no new secret exposure.

**Non-Goals:**
- No change to the inner selector, Sub2API entitlements, the front-door intersection, metadata chains, `include_models` semantics or post-processing. AISIX resources change only for the stale direct model in D9. Output order and `priority` (D7) are applied after post-processing.
- No change to the CPA plugin work tracked separately (the `/tmp` exec fix, quota plugin upgrade and auto-reset install). It is sequenced around batch A (see Migration Plan).
- No `@self` metadata for AISIX-only members from CPA bare rows (D5).

## Decisions

### D1 Membership rule

```
members = CPA-admitted
        ∪ AISIX IDs
        ∪ static models
        sorted by D7

CPA-admitted(id) =
     startsWith(id, P + "/") for some P in CPA prefixes
  or cpa_include(id)                              # include_models matcher
```

- Dedup is by exact bytes, and the first source (CPA, then AISIX, then static) supplies the base row. A CPA-admitted ID keeps its CPA row as base, even if AISIX also lists it. Position comes from D7, not from the source.
- This replaces `readIdentities`/`admitModels`, which use OAuth model-definitions and alias name pairs, and `supplementalIDs`. The generation field `supplemental` becomes `aisix`, and `identityFallbackCount` becomes `prefixFallbackCount`.
- Alternative rejected: keep the name-pair identity check and only change the AISIX side. It needs two more Management reads per account and the old model-definition semantics, and the user asked for a fixed rule.

### D2 Prefix discovery

- **Key-channel prefixes**: from the existing `discover()` reads. The current empty-prefix and duplicate-prefix conflicts stay configuration errors.
- **OAuth prefixes**: per enabled account, from `auth-files/models`. `P` is a prefix when the account registers both `P/x` and `x`. This holds exactly under CPA's default setting (`applyModelPrefixes`).
- **Reads removed**: `oauth-model-alias` and `model-definitions/<provider>`.
- Alternatives rejected:
  - Downloading auth files to read `prefix`: exposes tokens and breaks secret custody.
  - An operator-written prefix list: drifts from CPA.
  - Treating any first segment as a prefix: admits upstream originals such as `deepseek/…`.

### D3 `membership.cpa_include`

- New root block `membership:` with one key, `cpa_include`, an ordered glob list. It is compiled with `compileModelPattern`, so it has the same semantics as `metadata.include_models`.
- It only admits CPA IDs that have no CPA prefix. Hiding stays with `include_models`.
- Validation:
  - an absent field means no exceptions;
  - an empty list, a non-string entry, an empty pattern or a lone `!` or an unknown key fails closed, like the rest of `config.ts`.
- The private config sets `["devin/*"]`. The tracked example shows it commented out with the Devin reason.

### D4 Native passthrough, compatibility mode and fallback

- **Passthrough** (emit as CPA returned it, with only `priority` replaced by D7) applies to IDs that an OAuth account registers under an inferred prefix, unless a static slug owns the ID. Today's `nativeOnly` behaves the same way.
  - Key-channel IDs and `cpa_include` IDs (Devin) are compiled, as today.
- **Compatibility mode**: each collection reads `GET /v0/management/force-model-prefix`. If it is `true`:
  - CPA IDs containing `/` are admitted, plus `cpa_include` matches;
  - OAuth-registered IDs not matched by `cpa_include` pass through.

  Against today's production data this yields exactly today's member and passthrough sets. It lets model-catalog ship before the CPA switch, and a rollback that turns the setting back on needs no catalog change.
- **Fallback**: a failed Management read with no eligible cached response, including the setting read, leads to:
  - a warning;
  - admission of slash IDs plus `cpa_include` matches, emitted as CPA returned them;
  - no admission of bare IDs.

  This matches today's "identity unavailable, preserve CPA records" behavior.
- The setting read and prefix reads are authority reads: they go into the generation fingerprint and the force refresh bypasses their caches, like the current identity reads.

### D5 AISIX side

- `members` takes every AISIX ID (`decodeAISIXModels` is unchanged). The AISIX phase stays optional and isolated with its own budget, and a failure omits only AISIX members.
- An AISIX ID that CPA lists only bare, such as `gpt-6.1-sol`, stays an AISIX member with no `@self` row. Its output is then byte-identical to today, where CPA did not list it at all.
- Alternative not taken: use CPA's bare row as `@self`. It would change metadata for the 26 AISIX bare logical IDs, and the CPA row describes one credential's model, not the AISIX route.

### D6 CPA image and setting

- **Image**: `eceasy/cli-proxy-api:<latest release>@sha256:<index digest>` in `compose.yaml`, `.env.example` and the private `.env`.
  - At planning time: v8.0.44, `sha256:be365a6b3ca2b54947e9248c7bb7e1cdb974f66c922d2354b85daec2ad5cc201`. Re-resolve it at apply time.
  - The image is upstream's `debian:bookworm` build. It has `/bin/bash`, which the healthcheck and entrypoint need, and it is built with CGO for the plugin host.
- **Setting**: changed with `PUT /v0/management/force-model-prefix {"value": false}`. CPA persists it to `config.yaml` (worker-owned) and hot-applies it, so no file edit races `cpa-model-sync`.
- **#6760 gate**: before the setting flip, one streamed Codex request through CPA's operator port and one through the public chain are captured raw. Pass condition:
  - every `event:` line starts a new frame (preceded by a blank line or at stream start);
  - every frame's `data.type` equals its `event:` name.

- **`stream-bootstrap-buffering: true`** (user decision): v8.0.44 lacks #6760, and upstream's partial fix (v8.0.39, 9e79552f) holds `response.metadata` only while bootstrap buffering is on. It is enabled together with the image switch, through the Management API if CPA exposes the setting, otherwise by a backed-up edit of the live `config.yaml` with `cpa-model-sync` paused so the worker-owned file is not raced. This is the one non-default CPA setting kept.
- If the gate still fails with buffering on, roll back to the previous image digest and stop for a user decision.
- The release was re-resolved at apply time to v8.0.45 (`sha256:ce9583e4…`).

### D7 Output order and Codex priority

- **Rule (user decision)**: every emitted list puts IDs without `/` first, then all other IDs; each group is sorted by Unicode code point. The comparison is case-sensitive and locale-free, so it is deterministic across runs and hosts.
- **Where**: `prepare()` sorts the member array in place once per authority generation, after dedup. The standard list, every Codex projection and the compile set iterate members in that order, so nothing sorts again per request. `/models-table` uses the same comparator, because it appends referenced-only pool rows after the members.
- **Codex `priority`**: `project()` sets `priority` to the 1-based position of each kept row in the final list.
  - Codex lists models by ascending `priority` (smaller first), and CPA's own builder sorts the same way. Upstream template values start at 1, so 1-based keeps every value positive.
  - There are no gaps or ties, so the client order equals the list order.
  - It applies to every row. Passthrough rows get a shallow copy with only `priority` set, so the generation's shared native row is never mutated and CPA's field order is kept. Compiled rows are already projection-owned and are set in place.
- **Why rewrite passthrough `priority`**: CPA's value depends on the whole native set (Context, first batch B attempt), so it shifts on every CPA version or `force-model-prefix` change even when the row is otherwise identical. Owning it makes acceptance and client ordering independent of CPA's ranking. This is the only field model-catalog changes on a passthrough row.
- **Scope**: model-catalog outputs, and through the front-door intersection (which keeps model-catalog's order) the public Codex manifest. The unparameterized front-door `/v1/models` is answered by Sub2API and keeps Sub2API's order; changing Sub2API is out of scope.
- Alternative rejected: keep CPA's `priority` and only sort. The Codex client would then reorder by CPA's ranks and the user's order would not reach the picker.

### D8 CPA upgrade acceptance ledger

- **User decision**: accept the upstream v8.0.38 → v8.0.45 Devin catalog change. It is upstream data that any v8.0.45 build (including a custom rebuild) brings with its embedded `devin_models.json`.
- The batch B gate "catalog unchanged" becomes a ledger gate:
  - the native-manifest difference equals the embedded `devin_models.json` difference between the two tags, after CPA's normalization, exactly (+7/-17);
  - the catalog difference equals the same ledger minus the IDs that AISIX still lists: +7/-16 before the D9 removal, +7/-17 after it;
  - every other member, the passthrough set and every non-`priority` field of every other Codex row equal the pre-capture;
  - `priority` only has to satisfy D7.
- Before attributing any other difference, re-sample: a model registered in `auth-files/models` but missing from CPA's available list is transient and is compared again after the authority cache refreshes.
- An unexplained difference still triggers the batch B rollback. All other batch B hard gates are unchanged.

### D9 Stale AISIX direct model

- **User decision**: delete the AISIX direct model `devin/deepseek-v4-1-flash` from the private `services/routing/aisix/resources.yaml`. CPA v8.0.45 no longer registers it, so requests to it would fail.
- Removed: the direct model `devin/deepseek-v4-1-flash`, and its target in the logical route `deepseek-v4.1-flash`, whose `max_fallbacks` drops from 5 to 4 (the file's `targets − 1` convention, confirmed idempotent by the role's normalizer). The route keeps its other five targets. No replacement target (`devin/deepseek-v4-1-flash-1017`) is added.
- **Deployment**: `deploy-config.yml -e component=aisix` (`--check --diff` first) in the batch B window, right after the CPA image switch and before the ledger comparison. Rollback: `rollback-config.yml -e component=aisix`, together with the CPA rollback.

## Risks / Trade-offs

- [#6760 is not in v8.0.44: a `response.metadata` data-only frame followed by `event: error` can merge into a false error] → `stream-bootstrap-buffering` is enabled for upstream's partial fix, and the D6 gate runs before the setting flip. A case outside bootstrap stays exposed; the gate and the Codex SSE checks watch for it. Rollback is the previous image digest.
- [With the setting off, CPA serves slash IDs without a CPA prefix through any credential registering that exact ID, such as `deepseek/deepseek-v4.1-flash` via the channel that lists it] → This is the upstream default and the user's choice. It is recorded in docs. The catalog does not list these IDs (D1), and Sub2API entitlements still apply.
- [An upstream original whose first segment equals its own channel's prefix is admitted bare too: the `openrouter` channel declares `openrouter/auto`, so both `openrouter/auto` and `openrouter/openrouter/auto` qualify. Neither is in the native manifest today] → Both route to the same channel. Hide one with `include_models` if it appears. A unit test pins the behavior.
- [A future OAuth account without a prefix whose originals contain `/` is not admitted] → That is the purpose of `cpa_include`. The models table shows hidden or absent IDs. The docs say to add a glob.
- [Fallback admits upstream slash originals while Management reads are down] → Warning logs plus the read cache's last good responses. It is the same degraded behavior as today.
- [Changing the CPA image also changes the plugin host (v8.0.38 → v8.0.44); `codex-auto-reset` targets v8.0.36] → The noexec `/tmp` plugin failure is fixed before this change (CPA plugin update), so after the image switch both `cpa-quota-estimator` and `codex-auto-reset` must log `plugin loaded`; any plugin load failure is a regression and triggers the batch B rollback.

- [Rewriting `priority` changes the Codex picker order for every client] → This is the requested order (D7). Positions are 1-based without gaps, so no row ties with another.
- [The unparameterized public `/v1/models` keeps Sub2API's order] → Recorded scope limit (D7); Codex clients use the parameterized manifest.
- [A future CPA upgrade changes embedded model data again] → The D8 ledger is source-derived per upgrade; any difference it does not explain still rolls back.

## Migration Plan

1. **Repository**: implement D1–D5 and D7 with tests, update docs, publish the model-catalog image and pin it. Switch the CPA image pin (D6). CI green. D7 is a second publish after the first batch B attempt.
2. **Pre-capture** (read-only):
   - native manifest IDs, AISIX IDs, catalog IDs (standard list and Codex manifest bytes);
   - public `/v1/models` for a test key, Sub2API count, `:9083/models-table`;
   - `GET force-model-prefix`, plugin log lines.
3. **Batch A, model-catalog**: deploy the new image while CPA still forces prefixes (compatibility mode). Redone with the D7 image. Verify:
   - the member set and passthrough set equal the pre-capture, and every Codex row equals it except `priority`;
   - the order and `priority` follow D7;
   - public and Sub2API counts are unchanged.
4. **Batch B, CPA image**: recreate CPA on the official image, with the setting still `true`, then deploy the D9 AISIX change. Verify:
   - health, one CPA-routed and one AISIX-routed inference;
   - the D8 ledger holds;
   - the xAI image route accepts a prefixed ID;
   - the D6 framing gate passes;
   - plugin lines show no new failure.
5. **Batch C, setting**: `PUT force-model-prefix false`, then force-refresh the catalog. Verify:
   - the setting reads `false` and is persisted;
   - the native manifest now has bare IDs;
   - the catalog member and passthrough sets equal the post-batch-B capture;
   - every non-`priority` field of every Codex row equals it, and the order and `priority` follow D7;
   - public and Sub2API counts are unchanged;
   - spot routes: a prefixed ID and a `devin/*` ID reach the same credential (CPA logs), and a bare ID goes to AISIX.
6. **Record** a dated deployment record.
7. **Rollback**:
   - batch C: `PUT force-model-prefix true`; compatibility mode keeps the catalog correct;
   - batch B: the previous CPA image digest, and `rollback-config.yml -e component=aisix` if D9 was deployed;
   - batch A: the previous model-catalog digest.
