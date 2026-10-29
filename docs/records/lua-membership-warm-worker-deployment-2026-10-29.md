# Lua membership warm worker production deployment: 2026-10-29

## Result

Production model-catalog now runs the resident membership worker. Initialization has its own 10 s bound, and the 2 s execution bound starts only after the ready handshake. Only the model-catalog container was recreated, through the image-only component path, at 06:08:26 UTC. All five catalog bodies were byte-identical before and after. In the first 64 minutes the service logged no `wall_time` and no `init_timeout` WARN. The several-hour observation in task 6.1 is still open. This record does not archive the change.

## Identities

| Item | Value |
| --- | --- |
| Previous image | `ghcr.io/xz-dev/ai-gateway-model-catalog:v1.3.0-4b2ff32@sha256:60b32f94f17f8ab420a2a6fc056585e3bebb19c7930bf364f84e9be6ca926ee0` |
| Deployed image | `ghcr.io/xz-dev/ai-gateway-model-catalog:v1.4.0-6f595ab@sha256:f98cb5013c337420dd6f98112c053a6ad686185c723cf092062a5b0f541620ad` |
| Source commit / gateway pin | `6f595ab` / `2d71c74` |
| Private config SHA-256, before and after | `84e9159384c2377ef64eca3fffaeafe822699a80c13a09e79b716ae42c857b12` |
| Private script SHA-256, before and after | `fa9a93cbe04726d15dabfe6224f4aac5de68a0546e2f9c2634ddd7dbbf7354db` |
| Component approval digest | `e24d8046dc4c0d7361d828bfd1cd18309ab5a4b6068282384c490f15e2f5da51` |
| Component private snapshot | `.ai-ops-state/front-model-catalog-9za1jkwj` |

## Deployment

1. **Pre-capture, 05:54 UTC.** The capture used one temporary normal-client key (group 15) and was read-only. Counts: native 555, AISIX 76, members 344, internal Codex 344, public standard 344, public Codex 335, passthrough 47. The private `.env`, `compose.yaml`, `config.yaml` and `catalog.lua` were backed up to host scratch.
2. **Private `.env` edit.** Only `MODEL_CATALOG_IMAGE` changed; the old pin is kept as a `# rollback:` comment. The private Compose file already used the `${MODEL_CATALOG_IMAGE:-…}` expression, so it was not edited.
3. **Image gate.** The first `deploy.yml -e component=model-catalog --check --diff` failed at "Verify exact candidate image presence or read-only pullability". The pin names the amd64 platform manifest, but the tag resolves to the OCI index. For that `tag@digest` reference, `docker manifest inspect` reports `manifest verification failed for digest`; a digest-only reference resolves. The fix was to pre-pull the exact `tag@digest` reference on the host. That only adds the image locally; it activates nothing. The gate then verified the local image against the pin. v1.3.0 has the same index layout, but its earlier rollout used a manual window, not this gate.
4. **Preview.** `changed=0 failed=0`. Plan facts:
   - the only candidate file that differed was `.env`;
   - `environment_changes: []`;
   - 51 protected containers.

   A whole rendered-Compose comparison found exactly one changed path: `services.model-catalog.image`.
5. **Apply, 06:01:32–06:10:30 UTC.** Result `ok=147 changed=9 failed=0`. Those task changes are upload, transaction bookkeeping and the recreate; they do not mean nine services changed. Final facts: `protected_containers_unchanged: 51`, healthy, restarts 0, recovery timer active.
6. **Config unchanged.** `deploy-config.yml -e component=model-catalog --check --diff` reported `changed=0 failed=0`. The host digests matched the pre-capture. In-container `validate-config /app/config.yaml --script /app/catalog.lua` exited 0 with the same config and script digests. A final `deploy.yml` preview reported `changed=0` and `activation_required: false`.

## Byte comparison

The post-capture ran at 06:11:30 UTC.

| Body | Bytes | SHA-256, pre and post | `cmp` |
| --- | ---: | --- | --- |
| Internal standard | 18,070 | `8409f5b2302f9f86c7a3117aeb9414d7811a9192c032a34340a2dd63213e60c6` | identical |
| Internal Codex, client `0.101.0` | 2,789,229 | `96bfa49429e7834398cbd6926432305728c85c3a19b31dac4ec9dc279527b462` | identical |
| Public standard | 29,078 | `3de725e1edb72834fdf53564dfbbc68d70b866165157d16b5f6a734fbbbaddb1` | identical |
| Public Codex, client `0.101.0` | 2,784,872 | `b62027741f1800728c20d16de59ce1f774ed2cefb5e83039b4cc69ef6690a82e` | identical |
| Sub2API standard | 29,078 | `3de725e1edb72834fdf53564dfbbc68d70b866165157d16b5f6a734fbbbaddb1` | identical |

- **Inputs:** native and AISIX ID sets were unchanged, and passthrough stayed at 47.
- **Containers:** the inspection showed exactly one changed container ID, model-catalog.

At 07:14 UTC, after the separate AISIX-first routing window had restarted CPA, the internal standard body was still identical. The internal Codex body differed by one row, `aihub/gpt-6-astra`. That native model had come back: native 555 → 556, the only added ID, nothing removed. Its minimal row became a full native passthrough row. Membership stayed at 344. It is the same native-availability effect recorded for the 2026-10-28 rollout, and it came after the deployment gate had passed.

## Observation

A host-side sampler read the model-catalog cgroup, thread count, RSS, restart count and log counters once a minute, without exec into the container. In the 64 minutes from 06:08:26 to 07:12:48 UTC:

| Measure | Value |
| --- | --- |
| Authority collections, every 5 min | about 12 |
| Forced refreshes | 3 (06:16:52, 06:43:10, 07:03:56) |
| Scheduled table refreshes, every 30 min | about 2 |
| `membership script failed` / `wall_time` / `init_timeout` | 0 / 0 / 0 |
| Thread count | 12 steady (13 briefly during a forced refresh) |
| cgroup `memory.peak` | 282,390,528 bytes (269.3 MiB) of 384 MiB |
| cgroup current, settled | 58–115 MB |
| `oom` / `oom_kill` | 0 / 0 |
| Restarts | 0 |

The peak includes the in-container Node processes the capture and refresh probes spawn, so it is an upper bound for the service itself. The earlier rollout's peak was 229.9 MiB under the same probe method.

- **Before the deployment:** the v1.3.0 container logged 23 `membership script failed … class=wall_time` WARNs between 13:59 UTC on 2026-10-28 and 04:59 UTC on 2026-10-29, about one every 41 minutes, or roughly 12% of the 5-minute collections. Two of them fell in the two hours before the deployment. At that rate, about 1.5 were expected in the 64-minute observation; 0 were seen. That is consistent with the fix but not yet the several-hour proof that task 6.1 requires.
- **During the routing window:** the AISIX-first routing window restarted CPA, AISIX and apisix-models around 07:02 UTC, and was later rolled back (`4ca68c9`). In those samples model-catalog logged only upstream metadata WARNs: 10 `read cache stale fallback`, 8 `ollama /api/show miss`, 2 `channel fetch failed`. There was no membership failure, and those samples are not counted against the fix.

Temporary key 40 was deleted after validation: DELETE 200, then GET 404, and the key file is absent. The sampler keeps running read-only until about 09:36 UTC and writes `samples.txt` in host scratch.

## Local evidence (tasks 1.7 and 3)

- **Contention, 0.5 CPU and 384 MiB:** pinned to one CPU with 23 burners, old v1.3.0 failed 9 of 20 collections with `wall_time`; the new code failed 0 of 20 in three runs.
- **Memory, 150 rounds overlapping builds:**
  - peak 248.9 MiB;
  - last-10 settled 245.8–246.7 MiB;
  - slope 0.1 MiB/round;
  - membership worker limits 32 MiB old / 8 MiB young.
- **Injected `wall_time` every 10 rounds:** 7 thread IDs, peak 247.2 MiB, no growth.

## Rollback

On the operator machine, restore the private `.env` line to the commented `# rollback:` pin. Then run a fresh guarded component preview and apply:

```sh
cd ops/ansible
ansible-playbook playbooks/deploy.yml -e component=model-catalog --check --diff
ansible-playbook playbooks/deploy.yml -e component=model-catalog \
  -e ai_ops_component_approved_digest='<digest from the preview>'
```

Alternatively, use `playbooks/rollback.yml -e component=model-catalog` with its reviewed digest. The private config and script did not change, so nothing else has to be restored. The v1.3.0 image is still present on the host.
