This change does not depend on union-catalog-membership (design D6), so it has no gate task. Run it only while no other production window is open.

## 1. Diagnosis and pre-capture (read-only)

- [ ] 1.1 Diagnose the 7 `axis/*` targets read-only, as design D5 describes. Collect:
  - the channel's enabled flag and model names from the CPA config;
  - the auth and registration state;
  - recent CPA log lines for the `axis` prefix;
  - the upstream model list through CPA management, if available.

  Classify the cause as transient, permanent or unknown, and write the evidence path. Verify: the evidence is in `/var/tmp`, and the classification has one line per ID. If permanent or unknown, ask the user through the supervisor before 2.1, and record the answer.
- [ ] 1.2 Re-sample CPA's native and registered models. Confirm that none of the 15 IDs is registered now; any that is registered is dropped from the set and reported. Verify: a saved list shows that all 15 are absent from both native and registered.
- [ ] 1.3 Pre-capture to `/var/tmp/prune-aisix-<date>/pre/`:
  - AISIX effective inventory;
  - internal catalog, Codex manifest, passthrough set;
  - public standard and Codex lists, using one temporary normal-client key;
  - AISIX and model-catalog health.

  Compute the expected removal set per design D4. Verify: the expected set has 14 IDs, or one fewer for each ID dropped in 1.2. It is saved next to the capture, and the expected public Codex removals are listed.

## 2. Edit and deploy

- [ ] 2.1 Save a mode-0600 backup of the private `services/routing/aisix/resources.yaml` under `/var/tmp/prune-aisix-<date>/`. This is the rollback source, since `rollback-config.yml` has no `aisix` path. Then edit the file per design D3: remove the 15 direct models, and remove their targets from `gpt-5.6-luna`, `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-6-astra`, `gpt-6-luna`, `gpt-6.1-sol`, `grok-imagine-image-2.0`, `kimi-k3` and `kimi-k3-500k`. Keep the `axis/*` entries, unless the user approved their removal in 1.1. Verify:
  - a parsed comparison against the backup shows only these removals;
  - `python3 ops/scripts/host/check-aisix-orphans.py <file> --adopted '[]'` reports no unresolved reference and no empty route;
  - 76 models remain (91 − 15).
- [ ] 2.2 Run `ansible-playbook playbooks/deploy-config.yml -e component=aisix --check --diff`. Verify: the diff contains exactly:
  - the removed objects;
  - the removed target lines;
  - the `max_fallbacks` changes listed in design D3;
  - nothing else.
- [ ] 2.3 Apply `deploy-config.yml -e component=aisix`. Verify:
  - the role's Admin API read-back shows the edited routes in the new order;
  - none of the 15 IDs is in the effective inventory;
  - AISIX is healthy with restarts=0.

## 3. Production verify and record

- [ ] 3.1 Force a catalog refresh, wait for the public cache, and post-capture into `/var/tmp/prune-aisix-<date>/post/`. Verify:
  - the member set after equals the member set before minus exactly the expected set from 1.3;
  - every other row is equal apart from list order and Codex `priority`, and `priority` follows the output-order rule;
  - the passthrough set is unchanged (47);
  - the public standard and Codex lists lose exactly the expected IDs;
  - no AISIX direct model or route target names a newly unregistered ID.
- [ ] 3.2 Run one bare-route inference per edited route family through the public chain, with the temporary key: `gpt-6-astra`, `kimi-k3` and `grok-imagine-image-2.0` (image). Verify: each returns 200, and the AISIX log shows the first working target serving it.
- [ ] 3.3 Run `--check --diff` for `deploy-config.yml -e component=aisix` and `-e component=model-catalog`, and `deploy.yml -e component=aisix`. Delete the temporary key. Verify:
  - every run reports changed=0 failed=0;
  - the key DELETE returns 200, a following GET returns 404, and the local key file is gone.
- [ ] 3.4 Write `docs/records/prune-dangling-aisix-targets-<date>.md`, using model IDs and route names only. It covers:
  - the before and after counts;
  - the removed IDs with their old route positions, for restore;
  - the `axis/*` diagnosis and decision;
  - the three routes left with a single working target;
  - the rollback procedure: restore the backup, then run `deploy-config.yml -e component=aisix`, `--check --diff` first.

  Commit unsigned with explicit pathspecs and push. Verify: CI is green, and `openspec validate prune-dangling-aisix-targets --strict` passes.
