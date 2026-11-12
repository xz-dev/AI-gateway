## 1. Remove the CPA gate

- [x] 1.1 Reproduce the exact reported 9 MiB memory-charge refusal in a failing test, then delete CPA's headroom refusal and startup cgroup dependency; verify the full bodyspool test suite passes.
- [x] 1.2 Verify concurrent accounting/release, reclamation, API middleware and supported executor/handler spool regressions, and compile the server binary.

## 2. Publish and activate

- [x] 2.1 Build a fixed CPA replacement with auditable source provenance; after the operator-approved offline transfer, verify archive SHA-256, loaded image ID, binary and labels while preserving the deployed runtime base.
- [x] 2.2 Snapshot live inputs, prove an image-only guarded preview, deploy only CPA, and verify unchanged resources/environment and all protected neighbours.
- [x] 2.3 Verify real CPA Responses inference with the original Pi model, healthy state and no new OOM/restart; record the untested full front-door large-prompt replay explicitly, retain rollback instructions, adopt the verified private offline pin without inventing a public registry publication, and pass `openspec validate --all --strict`.
