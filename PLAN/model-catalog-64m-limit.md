# Raise model-catalog limits to 64 MiB

## Scope

Raise only existing model-catalog read, intersection, final-response, and completed-result cache limits from 16/32 MiB to 64 MiB. Keep inference-body and unrelated metadata-source limits unchanged. Do not add streaming or memory optimizations. Serialize all external OpenAI models requests through one APISIX-wide admission slot, independent of caller identity.

## Progress

- [x] Raise Sub2API catalog-read environment default to 64 MiB.
- [x] Raise public APISIX basic/original/final catalog limits to 64 MiB.
- [x] Enforce one global APISIX admission slot across versioned and standard external models requests.
- [x] Raise models-enricher CPA native/API-call reads and completed-result cache to 64 MiB.
- [x] Update existing limit assertions and current README values.
- [x] Build local amd64 models-enricher candidate without running tests.
- [x] Publish candidate under immutable GHCR digest.
- [x] Capture production backups and attempt the scoped `.env`, Compose, APISIX, and image cutover.
- [x] Stop and restore every production file/image reference after Sub2API hit its 256 MiB cgroup ceiling.
- [ ] Verify direct origin and `pi --list-models --refresh`; blocked because the direct-origin gate failed before the Pi refresh. No inference request was sent.

## Accepted risk

Owner explicitly selected a limit-only change and no additional capacity tests. Production evidence now establishes that even the current approximately 31 MiB catalog drives Sub2API beyond its 256 MiB cgroup ceiling during the 64 MiB cutover. The kernel killed Sub2API at approximately 260 MiB RSS, the direct-origin gate returned 502, and the rollout was stopped and fully rolled back. Completing delivery therefore requires a separately authorized memory optimization or capacity change.
