# Keeper model-first production upgrade — 2026-10-28

## Outcome

Keeper runs the model-first image below, healthy with zero restarts and no OOM.
Keepalive and the WS/SSE bridge remain healthy with zero restarts; their container
identities did not change. The guarded deployment verified **51 protected
containers unchanged**. Only keeper was recreated, starting at **15:22:37 UTC**.
No Sub2API account, routing, plugin or model-catalog configuration changed.

| Identity | Value |
|---|---|
| Keeper implementation | `5a69df7` |
| Published revision | `b49ad9ac6c1eb65b1785d66ecb7249413d4c82bf` |
| Gateway pin | `1dfc027a0319b0fe4560b1033c6aaa0dcbc6fd7c` |
| Image | `ghcr.io/xz-dev/openai-api-cache-keeper:b49ad9ac6c1eb65b1785d66ecb7249413d4c82bf` |
| Deployed image digest | `sha256:eb21638bdfc65ea09c119baf62337bb8f7f7b421890eeb1e63c1815c60030872` |
| Rollback image digest | `sha256:b8e12e6d8a5caf74a17b4615754655df3f409c7ad5721bae40eb3ac768964c0f` |

Keeper CI/image publication run `40699990993` and gateway Validate run
`40701307759` passed before deployment. Implementation evidence includes 383
passing race-test actions, vet/gofmt, HTTP/WS byte goldens, unchanged native
passthrough, and local keeper/bridge model offsets of 1 for 1 KiB/1 MiB/4 MiB
inputs. These are implementation checks, not production memory measurements.

## Guarded image-only window

The private Compose baseline used a literal old image and its environment file
had no keeper image variable. The supervisor approved aligning this one private
Compose line with the tracked `OPENAI_API_CACHE_KEEPER_IMAGE` expression, adding
the new environment pin, and preserving the old pin as a rollback comment.

A whole rendered-Compose comparison found exactly one changed path:
`services.openai-api-cache-keeper.image`. No environment or structural service
change was present. The separate private-versus-tracked Compose audit found 42
existing differing paths (image pins, builds/pull policy, resource settings,
healthcheck and operator-network configuration); none was reconciled here.
The full path-only audit remains in private scratch evidence.

From `ops/ansible/`, `deploy.yml -e component=openai-api-cache-keeper --check
--diff` passed. The reviewed approval digest was
`3eb8acbf6b74da4d9e8dcce6f8d1ea4bca6f42f5aad6ecb3ee61d4ea404b6f60`.
Applying with that digest passed: `ok=147 changed=9 failed=0`. Those task changes
include private-file upload, transaction bookkeeping, image pull and recreation;
they do not mean nine services changed. The immutable transaction snapshot ends
in `front-openai-api-cache-keeper-dfvictgs`.

Final fresh preview passed: **`changed=0 failed=0`**. The deployment did not
require rollback. Rollback would restore the previous private environment pin
and use a fresh guarded component preview/apply, or the component rollback
playbook with its newly reviewed digest. Keeper recreation interrupts active
streams and clears its disposable continuation cache; no persistent-history
migration is claimed.

## Public-chain acceptance

One temporary normal-client key (group 15) exercised `glm-5.3-flash` through the
public hostname. Prompts contained only synthetic acceptance text. Each row was
run before and after the upgrade; a second post-upgrade set supplied correlated
wire evidence.

| Probe | Before | After | Correlated repeat |
|---|---|---|---|
| Plain Responses, non-stream | 200, output present | 200, output present | 200 |
| Responses SSE | 200, 142 frames | 200, 127 frames | 200, 89 frames |
| Parent response | 200, keeper `store:true` | 200, keeper `store:true` | 200 |
| Parent-plus-delta continuation | 200, marker recalled | 200, marker recalled | 200 |
| Chat completion | 200, output present | 200, output present | 200 |

SSE parsing checked JSON event/type agreement and a terminal
`response.completed`; no broken-frame success was accepted. Both continuation
runs used a keeper-owned `resp_` parent and recalled a marker absent from the
second request, with `store:true` retained.

A read-only observer in keeper's network namespace inspected the beginning of
HTTP request bodies on the keeper-to-bridge hop. It wrote only path, framing,
probe correlation, first-key name and offset, never bodies, prompts or headers.
All **four correlated Responses requests** (plain, stream, parent, continuation)
used chunked framing and had **`model` first, quote offset 1**. Initial observer
attempts assumed Content-Length and produced no key evidence; the corrected
chunk-aware observer and repeated probe set supplied the accepted proof.
This does not establish final ordering after Sub2API transformations.

Temporary key **39** was deleted at **15:29:33 UTC**: DELETE **200**, subsequent
GET **404**, credential file absent. No temporary credential was copied into
tracked files or retained on the controller.

## Evidence and limits

Private controller and host evidence: `/var/tmp/ai-keeper-window/`, including
preview/apply/drift logs, rendered image-only diff, health captures, probe
summaries, key lifecycle and `model-first-wire-v3.jsonl`. Registry and local test
evidence: `/var/tmp/keeper-model-first-evidence/summary.md`.

No production large-body/RSS benchmark, live WS acceptance matrix, native-parent
passthrough probe or rollback drill was added in this window. Earlier tests
cover those serialization seams; the separate router spool gate is still
required. Sub2API may reorder fields, so keeper model-first does not remove the
need for late-model scanning and disk spill.
