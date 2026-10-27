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

## Goals / Non-Goals

**Goals:**
- The catalog member set is the same before and after the CPA switch, apart from documented, explained differences.
- Membership does not depend on CPA's `force-model-prefix`, including after a rollback that turns it back on.
- Fewer Management reads, and no new secret exposure.

**Non-Goals:**
- No change to the inner selector, AISIX routes, Sub2API entitlements, the front-door intersection, metadata chains, `include_models` semantics or post-processing.
- No change to the CPA plugin work tracked separately (the `/tmp` exec fix, quota plugin upgrade and auto-reset install). It is sequenced around batch A (see Migration Plan).
- No `@self` metadata for AISIX-only members from CPA bare rows (D5).

## Decisions

### D1 Membership rule

```
members = CPA-admitted (native order)
        ∪ AISIX IDs (AISIX order)
        ∪ static models (config order)

CPA-admitted(id) =
     startsWith(id, P + "/") for some P in CPA prefixes
  or cpa_include(id)                              # include_models matcher
```

- Dedup is by exact bytes, and the first source wins the position. A CPA-admitted ID keeps its CPA row as base, even if AISIX also lists it.
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
  - an empty list, a non-string entry, a bad pattern or an unknown key fails closed, like the rest of `config.ts`.
- The private config sets `["devin/*"]`. The tracked example shows it commented out with the Devin reason.

### D4 Native passthrough, compatibility mode and fallback

- **Passthrough** (emit exactly as CPA returned it) applies to IDs that an OAuth account registers under an inferred prefix, unless a static slug owns the ID. Today's `nativeOnly` behaves the same way.
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

## Risks / Trade-offs

- [#6760 is not in v8.0.44: a `response.metadata` data-only frame followed by `event: error` can merge into a false error] → `stream-bootstrap-buffering` is enabled for upstream's partial fix, and the D6 gate runs before the setting flip. A case outside bootstrap stays exposed; the gate and the Codex SSE checks watch for it. Rollback is the previous image digest.
- [With the setting off, CPA serves slash IDs without a CPA prefix through any credential registering that exact ID, such as `deepseek/deepseek-v4.1-flash` via the channel that lists it] → This is the upstream default and the user's choice. It is recorded in docs. The catalog does not list these IDs (D1), and Sub2API entitlements still apply.
- [An upstream original whose first segment equals its own channel's prefix is admitted bare too: the `openrouter` channel declares `openrouter/auto`, so both `openrouter/auto` and `openrouter/openrouter/auto` qualify. Neither is in the native manifest today] → Both route to the same channel. Hide one with `include_models` if it appears. A unit test pins the behavior.
- [A future OAuth account without a prefix whose originals contain `/` is not admitted] → That is the purpose of `cpa_include`. The models table shows hidden or absent IDs. The docs say to add a glob.
- [Fallback admits upstream slash originals while Management reads are down] → Warning logs plus the read cache's last good responses. It is the same degraded behavior as today.
- [Changing the CPA image also changes the plugin host (v8.0.38 → v8.0.44); `codex-auto-reset` targets v8.0.36] → The noexec `/tmp` plugin failure is fixed before this change (CPA plugin update), so after the image switch both `cpa-quota-estimator` and `codex-auto-reset` must log `plugin loaded`; any plugin load failure is a regression and triggers the batch B rollback.

## Migration Plan

1. **Repository**: implement D1–D5 with tests, update docs, publish the model-catalog image and pin it. Switch the CPA image pin (D6). CI green.
2. **Pre-capture** (read-only):
   - native manifest IDs, AISIX IDs, catalog IDs (standard list and Codex manifest bytes);
   - public `/v1/models` for a test key, Sub2API count, `:9083/models-table`;
   - `GET force-model-prefix`, plugin log lines.
3. **Batch A, model-catalog**: deploy the new image while CPA still forces prefixes (compatibility mode). Verify:
   - the member set, passthrough set and Codex manifest rows equal the pre-capture;
   - public and Sub2API counts are unchanged.
4. **Batch B, CPA image**: recreate CPA on the official image, with the setting still `true`. Verify:
   - health, one CPA-routed and one AISIX-routed inference;
   - the catalog is unchanged;
   - the xAI image route accepts a prefixed ID;
   - the D6 framing gate passes;
   - plugin lines show no new failure.
5. **Batch C, setting**: `PUT force-model-prefix false`, then force-refresh the catalog. Verify:
   - the setting reads `false` and is persisted;
   - the native manifest now has bare IDs;
   - the catalog ID diff against the pre-capture is empty or fully explained;
   - Codex rows are equal, and public and Sub2API counts are unchanged;
   - spot routes: a prefixed ID and a `devin/*` ID reach the same credential (CPA logs), and a bare ID goes to AISIX.
6. **Record** a dated deployment record.
7. **Rollback**:
   - batch C: `PUT force-model-prefix true`; compatibility mode keeps the catalog correct;
   - batch B: the previous CPA image digest;
   - batch A: the previous model-catalog digest.
