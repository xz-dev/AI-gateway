## Context

The production baseline is the v1.10-based `xz-dev/aisix` fork at `67343036`, not this repository's legacy PR1387 Dockerfile. The routing filter already returns `AllCandidatesUnavailable` on its fail-fast path. `ProxyError::into_response` relays some upstream responses verbatim. At that final serializer, a provider's HTTP status alone cannot distinguish a bad internal target from a policy rejection.

## Goals / Non-Goals

Classify a terminal pre-output failure only after the router knows the caller selected a valid aggregate and the gateway selected the failing target. Do not introduce another retry, cooldown, policy classifier, front-door rewrite, or configuration switch. Keep successful fallbacks successful.

## Decisions

- Use aggregate/attempt context and the observed structured upstream error. Check the baseline envelopes with mock Responses. Do not infer ownership from status, a slash in the requested model, or a substring anywhere in an error body. A policy body mentioning an unknown model is not evidence of a missing internal target.
- Reuse the gateway error response for a missing hop and `AllCandidatesUnavailable` for aggregate unavailability. Leave caller lookup, unrecognized 4xx, policy relay and post-commit stream handling alone. A global `ModelNotFound` or `UpstreamRelay` change would discard the distinction.
- Prove the fallback was actually selected: record attempts in the mock and confirm primary cooldown before the second case. Run the same HTTP assertions on the deployed revision, the fix and a mutant with the new classification disabled.

## Risks / Trade-offs

- Misclassifying policy errors would change Sub2API failover. Preserve both status and envelope for policy 400 and 403 and include an unrelated 400 control.
- Synthetic provider errors may differ from production. Match only verified structured forms; do not extend the classifier on speculation or commit private examples.

## Migration Plan

Use the existing fork CI and immutable publisher. Verify the published platform, source revision and digest, then run the existing compatibility ladder and regenerate its lock. Independent review must bind the source, image, tests and effective pin. Use a fresh guarded `component=aisix` preview and its approval digest. Stop on configuration drift or any other service change. Preserve the old image `ghcr.io/xz-dev/aisix@sha256:9721ff105e2e568bcaa31c5e5ae4231ecce1c74df822619fd51f4e087e3c6108` and grouped snapshot for guarded rollback. No production inference is needed.
