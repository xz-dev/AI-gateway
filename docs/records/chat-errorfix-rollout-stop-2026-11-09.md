# Chat error-fix release: merged, activation stopped before mutation

UTC: 2026-11-09T17:46:37Z. The terminal release attempt stopped before production activation. Previous healthy Sub2API remains active; the error-fix patch is pending deployment. No cleanup was performed.

## Release

PR22's exact reviewed head `629b17367532b68f83a20c5ebbe0ff0d987ae193` targeted `main` and changed only the compatibility lock and source record. All nine checks completed successfully, including both Ansible checks and capacity run42662872958. Authorized merge SHA: `0141f54b6ff01bcb300bfc28b04e25d1c34cce92`, merged17:43:00Z. No source repairs, direct main push, force push or tag.

The lock was independently fetched from that merge and verified to name `ghcr.io/xz-dev/sub2api@sha256:56557012dc75e96feb22e26fdb36fce057ab1e660cba8f907bf4b3d85472472a`. The published image was pulled once; its RepoDigest and OCI revision `02918ea957f0639db932b23e7e5d00a6f2298308` matched. Build42660300885 was successful. Integration20/20, capacity ladder10/10 and source558+73 test passes are inherited release evidence, not rerun in this operation.

## Admission refusal

Fresh preflight verified48healthy services,47unrelated identities,48namespace inodes,204security fingerprints, unchanged mounts and topology. Three samples ten seconds apart passed the unchanged245000000-byte keeper gate, with empty tickets and26established Sub2API connections each.

After the image pull, the gate refused at17:45:43Z: memory available939945984B, disk available3435384832B; memory slack+79248480B and disk slack **−69173952B**. The200MiB memory floor,1GiB disk floor,32MiB margin, declared charges, store headroom and live-ticket accounting were not changed. Hard floors held. Available disk fell177037312B from the last pre-pull sample; this includes concurrent filesystem activity and is not a logical image-size measurement.

The attempt stopped before candidate private-env staging, independent candidate Compose renders, fresh guarded preview/approval, live upload or recreation. No prior approval was reused. No deployment, rollback or baseline adoption occurred. Cleanup was not used to force admission.

## Actual running state and observation

Sub2API still runs `ghcr.io/xz-dev/sub2api@sha256:340273920f7f2abf3a6eb23fd1593fcc6206d60a4b113850ef43f31c8a232160`, OCI revision `569aeca2c4c87b2a806cf82139b327881e4207d3`. Docker memory512MiB, combined memory+swap832MiB, actual cgroup swap allowance320MiB, CPU1.5 and GOMEMLIMIT288MiB remain unchanged. Health is healthy, restart count0, no OOM events. All47unrelated identities,48namespace inodes,204security fingerprints and target mounts remain unchanged.

Live and controller source SHA256 values remain:

| File | SHA256 |
|---|---|
|compose.yaml|`7d207705230c015271249c5ec8a5b6c3f0d17c6dcd093f884572d14c08760606`|
|compose.override.yaml|`3aeeca33720b1ff7afd570bef2ec4865fcc2605738bc1ec98b8d30f4f595605c`|
|.env|`7d1613cc080678150a1416868fa170e65462d11ac2c05ddbc46fdfd8785ba915`|

Final three ten-second samples17:46:15–17:46:37Z recorded memory948035584/947716096/923967488B and disk3435360256/3435356160/3435356160B. Floors held; tickets were empty. The245MBgate remained disk-short, final slack−69202624B. Service availability is healthy, but capacity admission is not a PASS.

Existing traffic only,300seconds ending17:46:37Z, old runtime throughout: front1Chat200 and no parsed logged5xx; Sub2API1Chat200+2Responses200 and no parsed logged5xx. One existing Chat body_bytes field was543139B. No embeddings/rerank completion was observed. The sparse window cannot establish broad behavior, provider-terminal success or any new-patch production result.

Realistic8MiB production success remains unobserved. The corrected overcap guard is locally proven and loaded, but production-unretested; the canonical single-attempt marker was not replayed. Historical77exact error codes remain unknown and were not reopened. No paid inference, large synthetic calls, credential search, business/security/account/route edits, other component recreation, cleanup, task tick or archive.

## Retention and next owner

Protected before files, inspect/netns/render snapshots, image-pull verification, gate refusals and stopped closeout are retained in the private final-errorfix controller and remote state directories. No image rollback was necessary. Any future rollout requires a new current512MiB snapshot and fresh guarded approval; historical320MiB rollback resources must not be restored.

The writer stopped after this one attempt. The parent owns exact-manifest cleanup/retention decisions and any future deployment authorization. No background monitor or automatic retry was left running. Full capacity acceptance and production error-fix rollout remain incomplete.

## Supersession — 2026-11-09

This is a historical receipt, not the final deployed image or current admission
guarantee. Preserve its failed/stopped attempts and original evidence limits.
The [final deployment, cleanup and three residual capacity checks](maintenance-capacity-closeout-2026-11-09.md)
supersede its current-state/pending-deployment statements. No earlier unverified
rollout task is completed retroactively by this reconciliation.
