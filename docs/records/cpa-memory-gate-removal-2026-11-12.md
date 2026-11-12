# CPA memory gate removal — 2026-11-12

## Delivered

The operator explicitly requested deleting CPA's application memory-admission
refusal after the earlier GC-target mitigation did not prevent recurrence.
The gate-free CPA started at **02:57:28.656559340 UTC**. This removes CPA's
`503 insufficient_memory` charge refusal; it does not add physical capacity or
remove the container's hard OOM limit.

Only CPA's image changed. RAM remains 768 MiB, additional swap 256 MiB, CPU
1.75, and `GOMEMLIMIT=384MiB`. Body/inline/fallback limits, disk checks, ticket
validation, release/reclamation, other hops and account settings remain intact.
The controller verified all 47 protected neighbours and namespace ownership.

## Source and tests

Source: [45a74cc343ed7cf59cd9df4cc1350bdb61bf0664](https://github.com/xz-dev/CLIProxyAPI/commit/45a74cc343ed7cf59cd9df4cc1350bdb61bf0664),
branch `fix/remove-cpa-memory-admission` in the operator's fork.

The shared CPA charge callback now records bytes without reading cgroup memory
files or refusing growth. Its refusal constructor and startup cgroup dependency
were deleted. Accounting remains for balanced cleanup and reclamation; no new
configuration switch or alternate admission path was added.

Regression tests first reproduced the exact 9,437,184-byte charge with available
values 41,365,504 / 8,695,808 / 25,743,360 bytes, then passed after deletion.
Full `internal/bodyspool` tests, charge/reclaim/fallback race tests, API middleware
and actual spool-named handler/executor tests, formatting and server compilation
passed. The extra local 256 MiB runtime replay was not run: its existing load
guard refused to start at load >=16. This is not claimed as a passing replay.

## Explicitly approved offline activation

GHCR rejected the existing token's write scope. No alternate credential was
tried. The operator explicitly approved offline deployment; the task-scoped
registry credential file was removed and global authentication was unchanged.
The new image uses the previously deployed immutable runtime as its base and
replaces only the server binary.

Artifact identities:

- Uncompressed Docker archive: 278,466,560 bytes; SHA-256
  `e4c76f1894638d4b272d82e1e52432049ce30050ce480181caae9a4044522c8b`.
- Compressed archive: 84,131,334 bytes; SHA-256
  `03856be4237deb2fb4d94216a5c9430c97edeee0cd2c87e11701d0dcc08b0e78`.
- Podman/config SHA-256:
  `b2155dc7c40f08658b9e0c878a1f804e0f3c8bb36e52b3e7806de3bbf6770dd5`.
- Target containerd manifest/image ID:
  `sha256:b187c5e3cbe868a7efb88f4bbcda4570d0070ac9f52a61c6c5c918a4c58db1e4`.
- Running executable SHA-256:
  `3e658e76ab63ea6c72d507eeb7db0ffdf4b2a7246c1682d9d41d3fb346d83ad7`.

The first uncompressed transfer timed out after a partial copy. A compressed,
resumable transfer completed. Loading initially stopped verification because
Podman's config ID differs from containerd's manifest ID; the difference was
investigated, not waived. All eight rootfs layer IDs, the transferred archive,
source labels, runtime command and executable hash were independently matched
before using the target's canonical manifest ID.

The existing guarded preload-only profile was used, not a remote build or a
hand-started application container. The private profile changes only CPA to
`pull: false` and preserves every other default profile. The plan bound the
verified target image ID, reported no environment changes and no pull argv,
and used approval digest
`1416be4967b0558bb3156918693184a3583b1ccafac1c1effde347077403710f`.
Connection samples ten seconds apart were 0/0/8 established, with 0/0/2 on
listening ports. Actual interruption was not measured; the recreation could
interrupt those two connections.

Apply completed with 151 successful tasks and zero failures. Readback,
protected-neighbour checks, baseline commit and owned-lock release completed.
Private desired state was adopted from the verified candidate. Final preview
reported `activation_required=false`, `config_diff=false`, no environment delta,
healthy, lock unheld; 69 successful tasks and zero changes/failures.

The private fixed reference uses the local manifest digest and
`ci-45a74cc343-no-memory-admission` tag. It was **not published to GHCR**. Public
pullable template/CI image locks remain unchanged rather than falsely pointing
to an unpublished artifact. Future component operations on this deployment must
retain the private preload-only profile and locally verified image/archive.

## Runtime acceptance

At 02:59:53 UTC the running executable/source identity matched the tested build.
CPA was healthy with restart count zero, OOM false, `memory.current=113819648`,
swap zero and `memory.events` max/oom/oom_kill all zero. These early readings are
not a long-term capacity or performance result.

One bounded real request through CPA's `/v1/responses`, using the original Pi
model `gpt-6.1-sol`, returned **HTTP 200, status completed, in 3.3 seconds** with
no error code. At 03:15:34 UTC the same container remained healthy with zero
restarts and max/oom/oom_kill events, `memory.current=130760704` and swap
`10780672` bytes. This used an existing configured CPA credential without printing
or creating secrets. It verifies real CPA inference, not a full front-door
replay of the user's large prompt or a concurrent-load guarantee.

Herdr follow-up found one Pi stopped on a separate Codex usage-limit error. One
operator-authorized `/retry` was submitted, without changing model/account/auth.
An independent check found it working with a new successful `toolUse` at
03:05:18 UTC. Other normal sessions were not interrupted.

`openspec validate --all --strict` passed all 23 items before closeout; the narrow
`remove-cpa-memory-admission` change records the explicit CPA exception to the
older `large-request-capacity` plan.

## Rollback and retained evidence

The guarded operation saved the previous CPA configuration/image snapshot.
Rollback requires a fresh `rollback.yml -e component=cli-proxy-api --check --diff`
preview and its reviewed digest, retaining the private preload-only profile.
The old image is preserved. Restoring it also restores the removed memory gate;
use rollback for change-caused unhealthy operation or OOM, not merely to make
this application's former 32 MiB margin appear green.

Private controller evidence is under `/var/tmp/cpa-gate-removal-20261110/`
(the scratch name derives from the original incident); the compressed host
archive is protected under `.ai-ops-state/cpa-remove-memory-admission-45a74cc343/`.
No secret-bearing files, private endpoints or configuration were tracked.
