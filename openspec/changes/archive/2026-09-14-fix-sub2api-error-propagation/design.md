## Context

See `proposal.md` for the user-visible problem. Approved read-only inspection found accounts 1, 4, 5, and 6 explicitly using `pool_mode_retry_count: 1` and `pool_mode_retry_status_codes: [400,401,403,404,429,500,502,503,504]`, with custom error-code controls absent. The operator confirmed preserving count 1 and resetting only the broad pool status override to defaults. The original report of 3 referred to an unverified setting; it is not the actual same-account count. The claim that Sub2API suppresses every error remains unproven.

Evidence baseline: Sub2API v0.2.4 source commit `5de5e2bed035d43591a2e10e51f420ef6a84eb98`; the authorized runtime read pinned `docker.io/weishaw/sub2api:0.2.4@sha256:4a9620931fbb966b04375c34fe3edd01b640e7e6fbbba02537a9a64d9555a59e`, whose OCI revision label is the same commit. The running parent Pi executable was independently identified as `.155 / b0f728b8`; `.156 / e88a9b4b` is an additional inspected source version, not this parent's running version. The loaded `/app/data/config.yaml` predates container start, no alternate config selectors are set, and the relevant global switch/timeout/stream controls resolve from pinned source defaults: account switches `10` (`3` for Gemini), `failover_on_400=false`, stream data timeout `180`, keepalive `10`, response-header timeout `600`, OpenAI override `0`, both first-output timeouts `0`, and passthrough timeout headers disabled. Accounts 1, 4, 5, and 6 currently point to the CPA class, retain retry count 1 and the broad status list, have no custom-error or configured temporary-unschedulable controls, and have no active cooldown.

Authorized local implementation workspace: `/home/xz/Code/ai/sub2api-worktrees/fix-error-propagation`, detached at the v0.2.4 source pin. Do not change the older source checkout or publish/deploy without further approval.

### Source facts that constrain the change

| Control or boundary | Verified behavior at the baseline |
|---|---|
| `pool_mode_retry_count` | Default 3, maximum 10; same-account retry allowance, not an end-to-end attempt count (`account.go:1086-1108`). |
| `pool_mode_retry_status_codes` | Native default `[401, 403, 429]`; absent/null selects defaults, explicit `[]` disables status-triggered same-account retries (`account.go:1131-1214`). These are statuses received by the configured account's pool, not permission to retry invalid client credentials. |
| `custom_error_codes_enabled` / `custom_error_codes` | Controls `ShouldHandleErrorCode`; disabled or empty means handle all statuses. It is not the pool retry-status list (`account.go:1060-1069,1217-1250`). |
| Temporary-unschedulable rules | Affect account eligibility/cooldown and must not be cleared as if they were retry statuses. |
| Failover loop | Tracks same-account retries, account switches, and `LastFailoverErr` separately. Native request-specific deadlines/caps and other protocol recovery can differ from the ordinary pool limit (`handler/failover_loop.go`). |
| Sub2API failed-event sanitizer | Baseline removes response content/metadata fields and performs some error-code compatibility mapping; it does not simply delete all error fields (`openai_gateway_response_handling.go:1847+`). |
| Pi Responses parser | A bare `error` formats top-level `code` and `message` directly; `response.failed` reads `response.error`; missing required terminal EOF throws. A null code can still produce the user's exact placeholder (`openai-responses-shared.ts:741-759`). |
| Keepalive boundary | After committing HTTP 200, some proxies can only report later failures in-band and intentionally hide upstream details. Their behavior is a separate attribution boundary. |

## Goals / Non-Goals

**Goals:** preserve safe final failure meaning, end failed requests promptly, and revert confirmed overly broad same-account retry-status overrides to native defaults without replacing recovery machinery.

**Non-Goals:** expose raw upstream responses; disable useful retries; reset all account rules; change AISIX/CPA budgets; rewrite Pi; add a progress protocol or global generation deadline; couple this work to session affinity or context compression.

## Decisions

### 1. Establish a failing boundary test before choosing a patch

Use a small synthetic mock upstream with the actual Sub2API forwarding path. First compare direct mock responses with Sub2API output; then feed the same output through the selected Pi parser and the composed gateway fixture. Cover HTTP failure, bare SSE error, `response.failed`, premature EOF, transport failure, and WS/http_bridge conversion.

Record a safe request reference, error class/code/message, response commitment, attempt number, final-recovery decision, and client terminal time. No real prompts, management dumps, or provider credentials are needed. This distinguishes useful reasons already absent upstream from reasons removed by Sub2API or a later proxy.

A source suspicion is not a completed root-cause diagnosis. If the same detail is absent before Sub2API, do not invent it or claim a Sub2API patch recovered it. If the necessary change belongs to a different service, stop and obtain scope approval rather than silently edit it.

Alternative rejected: remove every sanitizer based on the generic symptom. That can expose secrets while leaving the real failure boundary unchanged.

### 2. Reuse pool defaults; do not conflate account controls

Reconcile only affected accounts. Keep their verified explicit same-account retry count of 1; do not unset this count to obtain the source default of 3. A simple single-account status-retry case can include one initial attempt plus one retry; account switching, protocol-specific recovery, and upstream/client retries must be counted separately. This is not a global two-attempt bound.

The broad `pool_mode_retry_status_codes` override is confirmed on all four affected accounts; prepare its removal through the supported unset operation. Do not set `[]` to mean default, hardcode a replacement copy of the default list, or broaden the default with a generic `4xx/5xx` rule. If the actual broad field is `custom_error_codes` or a cooldown rule, document its real effect instead of assuming it is the culprit. Leave unrelated policy alone unless the reproducer demonstrates a required change and the operator approves the specific difference.

Configuration updates must preserve the complete credential map and all unrelated fields using the endpoint's verified update semantics. A partial credentials object must not clobber authentication while removing one override. Production mutation remains separately approved.

Alternative rejected: lower all retry limits to hide delay. It treats a multiplier rather than error-loss/finalization and contradicts the chosen recovery policy.

### 3. Preserve a request-local cause through recovery

Use existing failover/error objects and passthrough rules before adding code. Retain the final attempted actionable error in request-local state. If candidate selection later fails without making an upstream attempt, reuse the prior safe cause and add exhaustion context rather than replace it with an empty/no-account wrapper. If no upstream was attempted, report the actual local selection failure; never use stale state from another request.

Intermediate retryable failures remain internal while recovery can still succeed. Once no allowed recovery remains, emit one terminal failure outcome and stop. A protocol-required sequence may contain more than one frame, but must not create duplicate client errors, a contradictory success, or a new generation after output.

Reuse existing native classifications, fixed safe explanations, and intentional compatibility mappings. If no reliable classification is available, emit a stable non-empty generic gateway failure without claiming an unavailable root cause. Unknown upstream code/message fields are not copied into client output. The operator explicitly accepts this fallback: exact upstream wording, finer-grained cause recovery, new classification tables, and growing free-text filtering blacklists are not delivery requirements. Restricted diagnostics may retain the existing original classification; do not invent an upstream HTTP status.

Alternative rejected: immediately forward every intermediate `error` to Pi while continuing to retry. A terminal error ends the client's attempt and cannot later be transparently turned into success.

### 4. Adapt final errors at the existing HTTP/SSE/WS writers

Reuse existing status mappings, failure categories, and code-aware writers. Supply the missing non-empty gateway classification and clear fixed explanation where needed; do not create a classifier, free-text filtering system, or generic error-routing framework.

```text
upstream failure
  |
  +-- native recovery allowed, no generated output, budget left
  |      -> record cause -> permitted retry
  |
  +-- final failure
         +-- HTTP uncommitted -> safe non-success JSON response
         +-- SSE committed    -> conforming terminal error -> finish
         +-- WS turn active   -> conforming failed turn -> finish turn
```

For Responses SSE, assert fields at the locations the real Pi parser reads: bare `error` uses top-level code/message; `response.failed` uses `response.error`. Do not send an incompatible nested-only envelope and rely on the client to guess. Chat Completions and other enabled protocols retain their own error shape rather than copy the Responses envelope.

Once the final failure is known, flush the terminal error and finish without another keepalive interval/backoff. An upstream bare error followed by an idle connection must not force downstream to await a second failed/completed event after the decision is already final. For persistent WS, end the failed turn using existing valid lifecycle rules, not necessarily every reusable connection.

Test clean EOF and transport exceptions separately. Keep legitimate `response.incomplete` outcomes distinct. After generated output, report failure without replay; on cancellation, stop timers/readers/recovery and do not attempt another generation. Do not add an arbitrary short deadline to healthy long reasoning: this change bounds final-failure handling, not all inference duration.

### 5. Safe transparency, not raw passthrough

Keep existing deliberate redaction and protocol mappings. The default exhaustion path returns native classifications and fixed safe explanations, with a stable generic failure for unrecognized data; it does not copy unknown upstream code/message fields. A syntactically valid JSON object or bounded string is not a safety guarantee. Credentials, prompt text, internal hosts/addresses, stack traces, and unsafe identifiers must not enter new client output. Malformed/non-JSON failures use the same honest fallback rather than raw-body extraction. Retain an existing safe gateway correlation reference where available; do not add diagnostic infrastructure for this slice.

Sub2API's client authentication/quota failures remain distinct from an upstream provider credential failure. The public APISIX opaque authentication/not-found policy remains unchanged; do not convert every provider 401 into a public client-authentication 401 or disable the perimeter rule just to show more detail.

If safe native error-passthrough configuration alone meets the fixtures, use it. A catch-all raw-body passthrough is not an acceptable shortcut. Where code still loses details or builds a malformed frame, fix the common failure boundary in an authorized Sub2API workspace and keep unrelated protocols/accounts compatible.

### 6. Measure finalization separately from retry and client waiting

The acceptance fixture records:

- upstream attempts and per-layer retry/switch decisions;
- time when the gateway decides failure is final;
- time the terminal frame/HTTP response is flushed and the downstream operation ends;
- Pi's error result and whether a new client retry starts.

Use fake clocks or controllable mock barriers for backoffs and finalization; assertions must prove no additional backoff/heartbeat is awaited after the final decision, not require an arbitrary production millisecond SLO. Disable Pi auto-retry when testing one gateway attempt, then use the existing retry-policy fixture to distinguish a new client attempt from a still-open gateway request. Do not start a live Pi inference to prove this.

## Risks / Trade-offs

- **Defaults are not the entire retry policy** -> Preserve protocol-specific logic, record actual calls, and do not claim a global three-attempt bound.
- **Retry and handling controls coexist** -> The pool override is confirmed; preserve absent custom error controls and do not clear cooldown configuration by name similarity.
- **More detailed errors leak sensitive data** -> Verify code/message extraction with synthetic secret-bearing payloads and retain deliberate perimeter opacity.
- **Another proxy still replaces the error** -> Run the composed fixture; treat required out-of-scope changes as blockers, not a passing end-to-end result.
- **Failure details arrive only after an initial bare error** -> Reuse available current-request information; do not wait indefinitely for richer details or manufacture them.
- **Removing a broad override reduces opportunities for eventual success** -> Verify selected-version defaults and expected final errors offline; do not silently change target routing or credentials to compensate.
- **Client retries still show a spinner** -> Verify each gateway failure completed and identify the separate client policy; do not promise no waiting while retaining automatic recovery.

## Migration Plan

1. Pin actual Sub2API/client versions and inspect a sanitized account-policy baseline in the later approved implementation phase. Build the deterministic boundary reproducer before changing runtime policy or code.
2. Prepare the minimal default-policy reset and verify it with mock errors. If existing safe passthrough rules satisfy the final-error contract, prefer configuration; otherwise add focused Sub2API fixes in an explicitly authorized source workspace.
3. Run the full HTTP/SSE/WS and Pi-parser matrix, including before/after commitment, success-after-retry, terminal error without connection close, clean EOF, cancellation, and secret redaction. Preserve red/green evidence and measured attempt/finalization results.
4. For code changes, publish an exact verified candidate through GitHub Actions/GHCR after publication approval; record source revision, fixed readable tag and digest. Do not build on the small production host.
5. Present the exact image and configuration diff for separate production approval. Preserve prior image references and changed policy fields, apply through established safe procedures, and verify service health plus permitted sanitized observations. No provider failure-injection requests are part of this plan.
6. If authorized application regresses service, restore the approved prior image/policy pair. Do not delete candidates, reset unrelated account rules, or change the other OpenSpec changes.

## Source References

- [Sub2API pool retry defaults and distinct custom error controls](https://github.com/Wei-Shaw/sub2api/blob/5de5e2bed035d43591a2e10e51f420ef6a84eb98/backend/internal/service/account.go#L1060-L1250)
- [Sub2API failover counters, delays, and last error](https://github.com/Wei-Shaw/sub2api/blob/5de5e2bed035d43591a2e10e51f420ef6a84eb98/backend/internal/handler/failover_loop.go)
- [Sub2API stream failure handling and sanitization](https://github.com/Wei-Shaw/sub2api/blob/5de5e2bed035d43591a2e10e51f420ef6a84eb98/backend/internal/service/openai_gateway_response_handling.go)
- [Sub2API WS HTTP bridge](https://github.com/Wei-Shaw/sub2api/blob/5de5e2bed035d43591a2e10e51f420ef6a84eb98/backend/internal/service/openai_ws_http_bridge.go)
- [Pi Responses parser, pinned .156 source](https://github.com/xz-dev/pi/blob/e88a9b4b9a26d73042defa261ab486d7c4e15093/packages/ai/src/api/openai-responses-shared.ts#L741-L759)
