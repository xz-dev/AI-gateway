# Verification

## Scope and immutable pins

Source-backed offline verification, authorized candidate publication, and a separately authorized production deployment. No live model-provider request or provider failure injection was used for production acceptance.

- Sub2API baseline source commit: `5de5e2bed035d43591a2e10e51f420ef6a84eb98`.
- Candidate source commit: `1c66619497ca7b4ea8ae677e44feb85b02c71e31` on branch `fix/sub2api-error-propagation` (exact 11-file portable candidate).
- Candidate workflow commit: `ec559f48459c48bd7bca6627eb8d0109e0ade6c7` on branch `ci/sub2api-error-propagation-image`.
- Published and deployed candidate image: `ghcr.io/xz-dev/sub2api:0.2.4-error-propagation-1c66619@sha256:8dae4426868616f4e4cb143183aae66317d5111bc51f239d8b6a0a15c9e969b2`.
- Candidate platform descriptor: `linux/amd64` `sha256:ec9f1a929eccd18fdc27835f2039d78d9fad62fd2a89c0b623e303461dc8ba05`.
- Previous production image and current rollback target: `docker.io/weishaw/sub2api:0.2.4@sha256:4a9620931fbb966b04375c34fe3edd01b640e7e6fbbba02537a9a64d9555a59e`; its OCI source/revision/version labels pin `https://github.com/Wei-Shaw/sub2api`, commit `5de5e2bed035d43591a2e10e51f420ef6a84eb98`, and version `0.2.4`.
- Pi: `b0f728b8` (`.155`).
- Frozen SSE fixture SHA-256: `8b931339f084ca89521a44ee24281bfb77ddb12cd7116d710e297c7f611002da`.
- Installed WS adapter SHA-256: `bfdfc06a04489666ea92816b7b9cb3a524552ce8abe907989da62d8af0ae333f`.
- Candidate worktree: `<sub2api-worktree>` = `/home/xz/Code/ai/sub2api-worktrees/fix-error-propagation` after local checkout.

## Verified candidate policy semantics

Affected accounts observed: `1`, `4`, `5`, `6`. The authorized production projection confirms that all four currently use CPA-class `base_url`, `pool_mode: true`, explicit `pool_mode_retry_count: 1`, and broad `pool_mode_retry_status_codes: [400,401,403,404,429,500,502,503,504]`. Custom error controls and configured temporary-unschedulable fields/rules are absent, and no account has an active cooldown. Retry count 1 is a same-account retry allowance, not total attempts. Omitting `pool_mode_retry_status_codes` selects native `[401,403,429]`; explicit `[]` disables status-triggered retries and does not reset defaults. Same-account retry, account switch, protocol recovery, and Pi retry are separate budgets/events.

The loaded `/app/data/config.yaml` predates current container start; no `CONFIG_FILE` or `DATA_DIR` selector is set. Relevant controls resolve from pinned source defaults: `max_account_switches=10`, Gemini switches `3`, `failover_on_400=false`, stream data timeout `180`, stream keepalive `10`, response-header timeout `600`, OpenAI response-header override `0`, normal/high-effort first-output timeouts `0`, and passthrough timeout headers disabled.

Offline update tests used a complete synthetic non-sensitive account object. Immediately before any production write, each current complete authorized object must be read again in the same protected operation. Only owned `pool_mode_retry_status_codes` is removed; credentials, CPA `base_url`, peer fields, `pool_mode`, retry count, model mapping, temporary-unschedulable fields, and unrelated fields are preserved. The projection itself made no live write.

The authorized sanitized pre-deploy read `/tmp/pi-task5-2-predeploy-read/result.json` at remote UTC `2026-09-13T15:57:03.059830+00:00` captured the old-image baseline: image digest `sha256:4a9620931fbb966b04375c34fe3edd01b640e7e6fbbba02537a9a64d9555a59e`; accounts 1, 4, 5, 6 active/schedulable and CPA-class with `pool_mode: true`, `pool_mode_retry_count: 1`, broad statuses `[400,401,403,404,429,500,502,503,504]`, model mapping counts absent/9/4/3, and no active cooldown.

## Fixed error mapping and wire shape

| Boundary | Exact candidate result |
|---|---|
| Final handler HTTP, status `401` | HTTP `502`; type/code `upstream_error`; `Upstream authentication failed, please contact administrator` |
| Final handler HTTP, status `403` | HTTP `502`; type/code `upstream_error`; `Upstream access forbidden, please contact administrator` |
| Final handler HTTP, status `429` | HTTP `429`; type/code `rate_limit_error`; `Upstream rate limit exceeded, please retry later` |
| Final handler HTTP, status `529` | HTTP `503`; type/code `upstream_error`; `Upstream service overloaded, please retry later` |
| Final handler HTTP, status `500/502/503/504` | HTTP `502`; type/code `upstream_error`; `Upstream service temporarily unavailable` |
| Final handler HTTP, other/unknown | HTTP `502`; type/code `upstream_error`; `Upstream request failed` |
| Immediate WS/http_bridge, status `401` | `response.failed`, fixed `upstream_error`; `Upstream authentication failed`; nested `response.error.status_code` preserves actual status |
| Immediate WS/http_bridge, status `403` | `response.failed`, fixed `upstream_error`; `Upstream access denied`; nested `response.error.status_code` preserves actual status |
| Immediate WS/http_bridge, status `429` | `response.failed`, fixed `rate_limit_error`; `Upstream rate limit exceeded`; nested `response.error.status_code` preserves actual status |
| Immediate WS/http_bridge, status `529` | `response.failed`, fixed `upstream_error`; `Upstream service overloaded`; nested `response.error.status_code` preserves actual status |
| Immediate WS/http_bridge, status `>=500` other than `529` | `response.failed`, fixed `upstream_error`; `Upstream service temporarily unavailable`; nested `response.error.status_code` preserves actual status |
| Immediate WS/http_bridge, unknown status | `response.failed`, fixed `upstream_error`; `Upstream request failed`; nested `response.error.status_code` preserves actual status |

Generated native Responses SSE read failure is top-level `type:error`: `event: error`, then JSON `{"type":"error","sequence_number":0,"code":"<reason>","message":"<reason>","param":null}`, then a blank line. `stream_read_error` is used for read failure; a bare error is not joined to the next data line. The changed immediate-HTTP WS/http_bridge path and the existing bare-error EOF synthesis use `response.failed` with non-empty `response.error.code/message`; the pre-existing transport-error WS path retains its fixed generic `type:error` and remains outside this slice. Newly generated default HTTP/native read-failure/immediate WS outputs do not copy raw upstream code, body, prompt, credential, or private address. Legacy bare-error EOF and direct upstream terminal forwarding may preserve structured upstream `type`/`code`/`message`; those are residual outside this new-output slice.

## Deterministic RED -> GREEN

| Case | RED boundary | GREEN evidence |
|---|---|---|
| Malformed native SSE | Joined data/null-code caused Pi JSON parse/placeholder failure | Separated top-level error gives Pi `stream_read_error` and `stopReason: error` |
| Held-open wait | Error could await another event/keepalive | Terminal delivery closes connected upstream; no extra backoff/heartbeat; disconnected usage drain preserves usage `9/2/cache 1` |
| Immediate WS HTTP failure as bare `type:error` | Pi could not finish failed turn | Converted to `response.failed`; no replay/completed event; `stopReason: error` |
| Immediate WS free prompt/private URL text | Unsafe text could cross adapter | Client frame/Ops/Pi use the fixed status-based message only; the retained internal error diagnostic masks the query value as `access_token=***` |
| Clean EOF/transport failure | Truncation could look successful | Pre-output recovery/failure; post-output no generation replay; valid `response.incomplete` remains distinct |

## Actual attempt, retry, switch, and client-retry counters

- Default `429 -> success`: upstream calls `[73020,73020]`; one `500ms` fake-time same-account retry; no switch.
- Repeated `429`: `[73010,73010,73011]`; one same-account retry, one switch, four selector calls, final `429`.
- Default-ineligible `502`: `[73020,73021]`; no same-account retry/backoff; immediate switch with `0` fake-time gap.
- Pi `maxRetries=0`: provider fetch `1`; handler `[start,finish]`; upstream `[88101,88102]`.
- Pi `maxRetries=1`: provider fetch `2`; handler `[start,finish,start,finish]`; upstream `[88101,88102,88101,88102]` (second completed client request, not an open gateway request).
- SSE: provider fetch `1`.
- WS: provider `1`, adapter fallback `0`, connection/create `1`; frames `2` for bare-error path or `1` for immediate path.

## Finalization, cancellation, and usage observations

On verified final-delivery paths, final failure flushes and terminates without another retry backoff or keepalive. Permitted same-account recovery still waits through the verified `500ms` fake-time backoff; no bound is claimed on healthy long generation. Cancellation during retry wait returns `FailoverCanceled`, performs no further attempt, and stops obsolete readers/timers. Post-output failures stay on current turn and do not replay generation. Disconnected stream cleanup drains authoritative usage; no synthetic success or duplicate terminal error was observed.

## Sanitized local reproduction commands

Set `<ai-gateway-worktree>` to the current AI-gateway root and `<sub2api-worktree>` to the local candidate path (`/home/xz/Code/ai/sub2api-worktrees/fix-error-propagation` here) before running commands; replace both placeholders, not only one. Commands use unit/mock fixtures; they contain no credentials, provider calls, or production endpoints.

```sh
cd <ai-gateway-worktree>
openspec validate fix-sub2api-error-propagation --strict

cd <sub2api-worktree>/backend
go test -tags unit ./internal/service -run '^TestOpenAIErrorPropagationRed$' -count=1 -v
go test -tags unit ./internal/service -run 'Test.*(Retry|HeldOpen|Flush|EOF|StreamRead|Security)' -count=1
go test -tags unit ./internal/handler -run 'Test.*(Failover|Candidate|NativeDefault|Error|Pi|Matrix|WebSocket)' -count=1
go test -tags unit ./internal/service ./internal/handler -run 'Test.*(OpenAI|WS|Bridge|Responses|Cancellation|Usage|Incomplete|EOF)' -count=1 -timeout 600s
git diff --check
```

`openspec validate` must be run with current AI-gateway root as working directory; the Go commands run in `<sub2api-worktree>`. Tester WS close `1006` and malformed acceptance-report formatting were infrastructure failures, excluded from acceptance; parent reruns supplied the passing rows.

## Security and public-opacity evidence

Offline synthetic secret-bearing, prompt-bearing, malformed, private IPv4/IPv6/overlay-address, and query-secret cases remained fixed/redacted in newly generated or changed outputs. Provider `401/403` remain upstream authentication/access failures, not client authentication. APISIX source-only inspection retains public opaque authentication/not-found behavior; no live plugin or deployment behavior is claimed.

## Candidate publication and verification

- Upstream-compatible source branch `fix/sub2api-error-propagation` at commit `1c66619497ca7b4ea8ae677e44feb85b02c71e31` (parent baseline `5de5e2bed035d43591a2e10e51f420ef6a84eb98`). The commit contains an exact 11-file portable candidate, excluding untracked `.pi/` state and four local Pi/Bun fixtures.
- Source CI runs: CI run 34765327457 succeeded (https://github.com/xz-dev/sub2api/actions/runs/34765327457); Security Scan run 34765327448 succeeded (https://github.com/xz-dev/sub2api/actions/runs/34765327448).
- Dedicated CI branch `ci/sub2api-error-propagation-image`: workflow-only commit `ec559f48459c48bd7bca6627eb8d0109e0ade6c7` (parent `1c66619497ca7b4ea8ae677e44feb85b02c71e31`). The workflow checks out exact `SOURCE_SHA` and pins action revisions; does not touch default branch, `VERSION`, releases, or tags.
- Image CI runs: Build candidate run 34766089916 succeeded (https://github.com/xz-dev/sub2api/actions/runs/34766089916), plus CI run 34766089898 and Security Scan run 34766089897.
- GHCR image verification: readable tag `ghcr.io/xz-dev/sub2api:0.2.4-error-propagation-1c66619`; canonical immutable index reference `ghcr.io/xz-dev/sub2api@sha256:8dae4426868616f4e4cb143183aae66317d5111bc51f239d8b6a0a15c9e969b2`; one runnable `linux/amd64` manifest descriptor `sha256:ec9f1a929eccd18fdc27835f2039d78d9fad62fd2a89c0b623e303461dc8ba05`; one unknown/unknown BuildKit provenance attestation; anonymous manifest fetch returns HTTP 200; OCI labels verify `org.opencontainers.image.source=https://github.com/xz-dev/sub2api`, `org.opencontainers.image.revision=1c66619497ca7b4ea8ae677e44feb85b02c71e31`, and `org.opencontainers.image.version=0.2.4-error-propagation-1c66619`.
- Publication incident and recovery: the initial source push inherited local `push.followTags` and unintentionally pushed 11 existing version tags (`v0.1.180..v0.1.185`, `v0.2.0..v0.2.4`); audit proved no tag-triggered Actions runs or GitHub Releases were created. Following explicit owner authorization, all 11 remote tag refs were deleted via API, and parent verification confirmed zero remote tags remain. The subsequent CI branch push used `--no-follow-tags`. This was a recovered publication side effect, not a product or deployment failure.
- Rollback reference: image `docker.io/weishaw/sub2api:0.2.4@sha256:4a9620931fbb966b04375c34fe3edd01b640e7e6fbbba02537a9a64d9555a59e`; accounts 1, 4, 5, 6 old status list `[400,401,403,404,429,500,502,503,504]` and retry count 1.
- Applied production diff: image old digest -> new candidate digest; for accounts 1, 4, 5, 6 only credentials key `pool_mode_retry_status_codes` was deleted (not set to `[]`); explicit `pool_mode_retry_count=1`, full credential remainder, CPA `base_url`, model mapping, and all peer-owned fields were preserved at the guarded transaction boundary.

## Production deployment and independent verification

The first two corrected production attempts timed out safely at the strict Redis quiescence gate after 180 seconds and wrote `STOPPED.json`; neither stopped Sub2API nor changed `.env` or PostgreSQL. Active-slot ages and churn identified only API key 7, named `hindsight`, using account 6. After the operator explicitly authorized a short Hindsight-only interruption, the successful run required six consecutive two-second samples in which all current account, user, API-key, live-turn, and WS-ingress entries belonged only to the authorized Hindsight identity. The final boundary contained four authorized slots and zero unexpected entries.

At remote UTC `2026-09-13T23:57:42Z`, the procedure stopped only Sub2API with Docker timeout 15 seconds. Pinned v0.2.4 reached its five-second handler-drain limit and logged the forced-shutdown path; this was inside the explicit Hindsight-only interruption authorization. A post-stop Redis classification recorded two remaining authorized Hindsight TTL-backed entries and zero unexpected entries. No Redis record was deleted and no Hindsight or sibling service was modified.

The transaction then locked the latest complete account rows, reasserted the approved predicates, and deleted only `credentials.pool_mode_retry_status_codes`. In-transaction comparisons proved `to_jsonb(row) - 'credentials'` unchanged, the credential remainder exactly equal, the key absent, and `pool_mode_retry_count=1`. Compose changed only `services.sub2api.image` and recreated only Sub2API. The new container reached healthy state and returned `{"status":"ok"}` from its local `/health` endpoint.

Parent verification independently established:

- running container ID `5a073078cb61a9ac9d96e8bbd66697dc473e2e58edd42afd2ab0542baf867ee7`, restart count 0, healthy, configured with the exact candidate digest;
- candidate OCI source `https://github.com/xz-dev/sub2api`, revision `1c66619497ca7b4ea8ae677e44feb85b02c71e31`, version `0.2.4-error-propagation-1c66619`, `linux/amd64`;
- accounts 1/4/5/6 omit only the retry-status key, retain retry count 1, and match the deployment-time non-credential and credential-remainder hashes; current credentials remain unchanged. After service recovery, Hindsight use changed only account 6 runtime fields `last_used_at` and `updated_at`;
- rendered Compose configuration differs from the pre-deploy copy only in the Sub2API image, and `.env` differs only in its `SUB2API_IMAGE` assignment while retaining ownership/mode;
- all 51 sibling containers retain the same IDs and remain running and non-unhealthy;
- the prior image remains locally available for rollback;
- all 13 protected rollout assets pass `SHA256SUMS`, including `rollback.sh`, an 81,517,055-byte PostgreSQL custom dump, and a 1,208-line verified TOC.

The successful receipt and rollback bundle are under `/root/rollouts/sub2api-task5-3-20260913T235704Z/`. Production verification used container metadata, local health, PostgreSQL reads, Redis metadata, protected snapshots, and checksums only; provider request count was zero.

## Evidence index

- `proposal.md`, `design.md`, `specs/client-visible-upstream-errors/spec.md`: scope and contract.
- `tasks.md` checked results for 1.2, 2.1-2.2, 3.1-3.4, 4.1-4.2, 5.2: exact counters, fixtures, test summaries, and publication references.
- Candidate source: `backend/internal/handler/openai_gateway_handler.go`, `backend/internal/service/openai_gateway_response_handling.go`, `backend/internal/service/openai_ws_http_bridge.go`.
- Candidate tests: `backend/internal/{handler,service}/*error*test.go`, `*candidate*test.go`, `*retry*test.go`, `*bridge*test.go`.
- Candidate commits: source `1c66619497ca7b4ea8ae677e44feb85b02c71e31` on `fix/sub2api-error-propagation`; workflow `ec559f48459c48bd7bca6627eb8d0109e0ade6c7` on `ci/sub2api-error-propagation-image`.
- Published candidate image: `ghcr.io/xz-dev/sub2api:0.2.4-error-propagation-1c66619@sha256:8dae4426868616f4e4cb143183aae66317d5111bc51f239d8b6a0a15c9e969b2`.
- Actions runs: 34765327457 (CI), 34765327448 (Security Scan), 34766089916 (Build candidate), 34766089898 (CI), 34766089897 (Security Scan).
- Frozen fixture and run logs cited in checked task entries under `/tmp/pi-subagents-uid-1000/async-subagent-runs/`.
- Owner-authorized sanitized runtime policy/account projections and OCI image pin: `/tmp/pi-task1-1-authorized-inspect-v2/result.json`, `image-pin.json`, and pre-deploy read `/tmp/pi-task5-2-predeploy-read/result.json` (remote UTC `2026-09-13T15:57:03.059830+00:00`).
- Production receipt and rollback assets: `/root/rollouts/sub2api-task5-3-20260913T235704Z/`; parent verifier `/tmp/sub2api-task5-3-parent-verify.py`.

## Limitations

The original `/proc/1/environ` projection failed at a coarse permission/authentication-class boundary and did not prove SSH or PostgreSQL authentication failure. The owner subsequently authorized a narrower `docker inspect .Config.Env` projection restricted to allowlisted keys; that read completed and superseded the earlier unknown-policy state. No claim is made about no waiting during permitted retry or healthy long generation. Post-HTTP-200 read/socket failures remain outside provider-acquisition retry; absent terminal delivery can destroy partial output. APISIX opacity evidence is source-only. Local Pi/adapter/Bun fixtures are nonportable. Production behavior was not validated by provider failure injection; acceptance relies on the exact deployed provenance, local health and state invariants, plus the offline mock/Pi parser matrices. The maintenance stop intentionally interrupted only observed Hindsight activity under explicit operator authorization.
