## 1. Pin and prepare publication

- [x] 1.1 Pin PR #1387 to `06e8075266119380844aa4e9fcb9fb80468715eb` and verify the production Responses blob is the old side of its patch.
- [x] 1.2 Prepare the owned publisher and immutable-tag checks; verify its fetched application, dependency, build-script and configuration inputs are the pinned PR head.
- [x] 1.3 Repair the fail-open tag guard and verify 15 offline cases, an old-guard negative control, workflow syntax and `git diff --check`.
- [x] 1.4 Obtain independent approval of the repaired publication delta and retain the actual review report.

## 2. Build and accept the runtime image

- [x] 2.1 Publish the exact reviewed revision through owned native image CI; verify success and record the immutable single-platform linux/amd64 manifest identity without moving unrelated tags.
- [x] 2.2 Inspect the full candidate image and binary version; verify source identity and isolated startup with existing configuration, listener-only loopback remapping, no external network and no published ports.

## 3. Roll out only AISIX

- [x] 3.1 Preserve the exact old image/rollback tag, live configuration and 48-container baseline; fetch three backup artifacts and verify hashes and private permissions.
- [x] 3.2 Adopt a separate private candidate configuration and verify the old-image baseline preview has `changed=0`, `failed=0`.
- [x] 3.3 Change only the candidate AISIX image pin, recheck capacity and review a fresh AISIX-only guarded preview/digest.
- [x] 3.4 Select the scoped low-traffic window, apply the reviewed digest, and verify exact image/environment/readiness/namespace plus unchanged neighbouring services.
- [x] 3.5 Adopt only the accepted AISIX pin back into controller state and verify the final preview and rollback receipt have no unexplained drift.

## 4. Close this slice and resume maintenance

- [x] 4.1 Write a sanitized rollout/rollback receipt and validate with `openspec validate --all --strict`; do not mark unresolved runtime checks complete.
- [x] 4.2 Resume `trim-sub2api-patches-and-upgrade` from its saved security-review/image-publication stage, preserving rerank and the separately approved migration/security scope.
