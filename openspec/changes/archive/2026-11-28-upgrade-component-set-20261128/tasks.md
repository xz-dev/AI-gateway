## 1. Candidate gates

- [x] Verify live scope, upstream releases, fork sources and exact published image digests; exclude unused nginx selector.
- [x] Exercise the exact CPA image with the currently installed plugin assets and run focused terminal, plugin-host and capacity tests.
- [x] Pass standard capacity ladder on the intermediate and combined digests without swap; generate the combined standard lock from the passing run.
- [x] Complete AISIX regression verification and validate the tracked pins and the scoped private previews.

## 2. Guarded rollout

- [x] Confirm the interruption window and apply ZCode, CPA, then AISIX using fresh per-component plans and readback.
- [x] Compare final production and controller state, document verification and known exceptions, and run strict validation.

Promote the tested result to `main` in one push after validation CI and OpenSpec archive.
