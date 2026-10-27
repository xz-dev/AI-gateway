# Catalog size, channel policy and resource gates

Status: **32 MiB candidate withdrawn. Corrected filtering passes local tests;
production acceptance remains blocked and the second rollout was rolled back.**
See [PRODUCTION.md](PRODUCTION.md) for the two distinct attempts.

## Missing policy — corrected

The first deployment incorrectly used `channels: {}`. An absence of explicit
filters in the old Go configuration did not supersede the user's requirements:

- NIM: only `minimaxai/minimax-m3`, `moonshotai/kimi-k3`, and
  `deepseek-ai/deepseek-v4-flash-0731`.
- ShuaiAPI: only Claude models, expressed against the captured IDs as `^claude-`.

`config.example.json` now carries these policies. Inventory tests read that actual
file and verify selection, including exclusion of neighboring/non-Claude names.
Do not replace these rules with an empty deployment policy. The production client
version override remains `v0.65.0`; it is independent of the example version.

The unchanged final-image Rust executable was exercised with the captured
inventories and this policy against a local CPA management fixture. NIM retained
3 of 81 entries; ShuaiAPI retained 13 of 21. The first round updated five channels
and left NIM unchanged; the second reported six unchanged, zero writes and no
failures. These six-channel fixture results are not twelve-channel production
results.

## Frozen native-size evidence

With the exact CPA image, no networking, synthetic credentials and omitted OAuth
files/plugins:

| Isolated native response | Records | Bytes |
| --- | ---: | ---: |
| Before synchronization | 68 | 3,094,957 |
| Unfiltered expansion | 421 | 19,121,714 |
| Corrected policy | 251 | 11,376,360 |

The unfiltered expansion had 221 configured-prefix IDs plus 200 other routing IDs.
Its `model_messages` and `base_instructions` occupied 95.8% of serialized value
bytes. Long metadata repeated across routing records explains the growth without
requiring Go merge duplication. However, **the unfiltered payload did not establish
that the requested filtered deployment needed a larger limit**. These isolated
counts are not an exact production replay.

All three front boundaries remain **16 MiB**, and admission remains two. No
production byte limit, memory, CPU or concurrency setting was changed.

## Corrected local HTTP/resource gate

The actual HTTP fixture carries 251 representative records with long metadata,
including exact numeric values. It returned **11,076,924 bytes**, preserved metadata
and routing identities, and completed two simultaneously admitted requests with
identical hashes. Ordinary precision, entitlement-first, ETag/304, opaque errors,
16 MiB overflow and restricted-path checks also passed.

The four measured services were separated into independent cgroups:

| Component | Budget | Measured peak bytes |
| --- | --- | ---: |
| Front APISIX | 128 MiB, 1 CPU | 123,985,920 |
| Catalog APISIX | 128 MiB, 1 CPU | 100,499,456 |
| Candidate Go enricher | 256 MiB, 0.5 CPU | 113,405,952 |
| Sidecar | 32 MiB, 0.2 CPU | 17,068,032 |

No measured cgroup recorded an OOM event. The Go candidate was the frozen image.
The sidecar used the fixture's OpenResty nginx, not the deployed sidecar image.
Fake CPA, basic authorization and test-client allocations were outside these
budgets. In particular, **this fixture did not exercise real Sub2API memory** and
cannot establish whole-production safety.

Evidence under `/tmp/cpa-sync-deploy.YnAHzK/`:

- `filter_probe.py`, `filtered/rust-round-{1,2}.log`
- `filtered/{synthetic-*.json,native-results.jsonl,native-output/}`
- `filtered-budget-1788696841/result.json` and component logs
- `run_filtered_budget.py`
- `filtered/{policy-tests.log,policy-clippy.log,lua-tests.log}`

## Earlier 32 MiB gate — historical failure, not the current candidate

Before recovering the policy, a roughly 19 MB / 440-record fixture reproduced
502 at 16 MiB. Raising original/final limits to 32 MiB then hit 134,217,728 bytes
and two worker OOM kills in a 128 MiB, 0.5-CPU APISIX container combining front and
catalog legs. That combined workload was not an exact production-front
measurement. Fidelity and two-request completion did not pass that run.

The master survived with `State.OOMKilled=false`; cgroup `memory.events` and
signal-9 worker logs established the OOM. Those failed results remain preserved in
`growth-red.log`, `growth-bounded/`, and `run_growth_bounded.py` under the same
local evidence directory. The limit edits were subsequently reverted.

## Production stop boundary

The corrected-policy rollout observed two scheduler summaries but encountered
Sub2API cgroup OOM kills at 12:40:20 and 12:41:14 UTC. Rust was stopped and the
original deployment/models restored. Candidate public-catalog load acceptance
was not completed before that stop; local measurements must not fill that gap.
The second summary also contained an OpenRouter update, so an all-channel
zero-write production round was not established.

Further rollout is blocked on scoped investigation of Sub2API OOM. Its causal
relationship to this rollout is not established. No Sub2API repair or resource
increase was made. The two upstream fetch failures and external HTTPS limitation
remain unresolved.
