#!/usr/bin/env bash
# Source-only Squid policy reconciliation must not activate an unchanged tree.
set -euo pipefail
root=$(cd "$(dirname "$0")/../../.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
eval "$(python3 "$root/ops/scripts/tests/fixture_env.py" shell "$tmp")"
mkdir -p "$tmp/staging" "$tmp/private/data/egress-proxy" "$tmp/remote/generated"
printf '{"services":{}}\n' >"$tmp/private/data/egress-proxy/policy.json"
printf 'old source\n' >"$tmp/remote/policy.json"
printf '# policy-sha256: %s\n' "$(sha256sum "$tmp/private/data/egress-proxy/policy.json" | awk '{print $1}')" >"$tmp/staging/squid.conf"
cp "$tmp/staging/squid.conf" "$tmp/remote/generated/squid.conf"
(cd "$tmp/staging" && find . -type f -print0 | sort -z | xargs -0 sha256sum) >"$tmp/remote/generated/.manifest.txt"
# The render needs podman and the pinned Squid image, so this fixture runs the tree
# deploy and the source sync of ai_ops_squid on a pre-rendered staging directory.
cat >"$tmp/play.yml" <<EOF
- hosts: localhost
  connection: local
  gather_facts: false
  module_defaults:
    community.docker.docker_compose_v2:
      project_src: "$tmp/remote"
      project_name: ai-gateway
  vars:
    ai_ops_squid_staging: "$tmp/staging"
    ai_ops_squid_remote_dir: "$tmp/remote/generated"
    ai_ops_config_dir: "$tmp/private"
  tasks:
    - ansible.builtin.include_role:
        name: ai_ops_squid
        tasks_from: tree
      when: not ai_ops_squid_source_only | default(false) | bool
      tags: [always]
    - ansible.builtin.include_role:
        name: ai_ops_squid
        tasks_from: source
      tags: [always]
EOF
run() {
    local args=()
    [[ $1 == plan ]] && args+=(--check --diff)
    [[ -n ${2:-} ]] && args+=(-e ai_ops_squid_source_only=true)
    ansible-playbook -i localhost, "$tmp/play.yml" "${args[@]}" >"$tmp/run.log" 2>&1 || {
        cat "$tmp/run.log" >&2
        return 1
    }
}
activations() { grep -c '"docker_compose_v2"' "$AI_OPS_SHIM_LOG" || true; }
run plan source-only
[[ $(<"$tmp/remote/policy.json") == 'old source' ]]
[[ $(activations) == 0 ]]
run apply source-only
! grep -Eq 'Build tarball|Extract policy tree|Activate squid policy' "$tmp/run.log" || exit 1
cmp "$tmp/private/data/egress-proxy/policy.json" "$tmp/remote/policy.json"
[[ $(activations) == 0 ]]
[[ $(stat -c '%a' "$tmp/remote/policy.json") == 600 ]]
backups=("$tmp/remote/"policy.json.*~)
[[ ${#backups[@]} == 1 && $(<"${backups[0]}") == 'old source' ]]
before=$(stat -c '%Y:%i' "$tmp/remote/policy.json")
# A check run on the unchanged tree is a true no-op: the local render scratch is not a change.
run plan
grep -Eq 'localhost +: ok=[0-9]+ +changed=0 +unreachable=0 +failed=0' "$tmp/run.log"
run apply
grep -Eq 'localhost +: ok=[0-9]+ +changed=0 +unreachable=0 +failed=0' "$tmp/run.log"
[[ $(stat -c '%Y:%i' "$tmp/remote/policy.json") == "$before" ]]
[[ $(activations) == 0 ]]
backups=("$tmp/remote/"policy.json.*~)
[[ ${#backups[@]} == 1 ]]
printf 'source does not match deployed tree\n' >"$tmp/private/data/egress-proxy/policy.json"
if run apply source-only 2>"$tmp/expected-provenance-failure.log"; then
    echo 'FAIL: source/deployed provenance mismatch was not blocked' >&2
    exit 1
fi
[[ $(<"$tmp/remote/policy.json") == '{"services":{}}' ]]
printf 'unauthorized generated edit\n' >>"$tmp/remote/generated/squid.conf"
printf 'source must not be uploaded\n' >"$tmp/private/data/egress-proxy/policy.json"
if run apply 2>"$tmp/expected-failure.log"; then
    echo 'FAIL: generated-tree drift was not blocked' >&2
    exit 1
fi
grep -q 'REMOTE DRIFT DETECTED' "$tmp/expected-failure.log"
[[ $(<"$tmp/remote/policy.json") == '{"services":{}}' ]]
[[ $(activations) == 0 ]]
echo 'PASS: scoped check/repair, check-mode changed=0, full-deploy idempotence, backup, permissions, provenance/drift refusal; no activation'
