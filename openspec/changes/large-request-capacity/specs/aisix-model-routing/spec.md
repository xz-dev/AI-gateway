> Open change: requirements below are intended acceptance, not certification
> of full production capacity. See the [2026-11-09 disposition](../../../../../docs/records/maintenance-capacity-closeout-2026-11-09.md);
> residual size checks and public-template enablement remain pending.

## MODIFIED Requirements

### Requirement: AISIX memory admits one maximum Responses body

The AISIX container SHALL have a 512 MiB memory limit with no swap (`memswap_limit` equal to `mem_limit`). AISIX SHALL run the body-spool patch, built from the repository's patched AISIX build line, with its switch enabled and its spool on a disk volume. One 256 MiB Responses request SHALL complete under that limit with flat memory and `memory.peak` at least 32 MiB under the limit. A 1 GiB request SHALL complete in the nightly CI run. Guardrail chains and bridged non-native targets SHALL refuse bodies above their measured fallback limit, at most 16 MiB, with an explicit 413. AISIX SHALL enforce the inline-bytes limit and SHALL charge every request against its own cgroup headroom, refusing with 503 `insufficient_memory` when the charge does not fit. `request_body_limit_bytes` SHALL equal the 256 MiB cap and SHALL also apply to chunked bodies. The tracked AISIX image pin SHALL equal the digest in the published compatibility lock and the digest production runs. Concurrent large requests are admitted only by the host-wide admission gate. AISIX SHALL hold the ticket lease through its read-only ledger mount, forward the ticket only to CPA, and refuse a body of 64 MiB or more that carries no valid ticket. With these limits and charges in force, a request SHALL be refused explicitly rather than drive AISIX into an OOM kill.

#### Scenario: One 64 MiB request succeeds
- **WHEN** a single 64 MiB Responses request passes through AISIX `*` to CPA
- **THEN** it succeeds without an OOM kill, and the peak cgroup memory is recorded

#### Scenario: One 256 MiB request succeeds
- **WHEN** a single 256 MiB Responses request passes through AISIX `*` to CPA
- **THEN** it succeeds without an OOM kill, and the peak cgroup memory is recorded within its target

#### Scenario: Pin drift is visible
- **WHEN** an operator compares tracked `compose.yaml`, `.env.example` and the running AISIX image
- **THEN** all three, and the published compatibility lock, name the same digest until a reviewed upgrade changes them together
