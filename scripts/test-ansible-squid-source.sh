#!/usr/bin/env bash
# Source-only Squid policy reconciliation must not activate an unchanged tree.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/staging" "$tmp/private/data/egress-proxy" "$tmp/remote/generated"
printf '{"services":{}}\n' >"$tmp/private/data/egress-proxy/policy.json"
printf 'old source\n' >"$tmp/remote/policy.json"
printf '# policy-sha256: %s\n' "$(sha256sum "$tmp/private/data/egress-proxy/policy.json" | awk '{print $1}')" >"$tmp/staging/squid.conf"
cp "$tmp/staging/squid.conf" "$tmp/remote/generated/squid.conf"
(cd "$tmp/staging" && find . -type f -print0 | sort -z | xargs -0 sha256sum) >"$tmp/remote/generated/.manifest.txt"
cat >"$tmp/play.yml" <<EOF
- hosts: localhost
  connection: local
  gather_facts: false
  vars:
    ai_squid_staging: "$tmp/staging"
    ai_squid_remote_dir: "$tmp/remote/generated"
    ai_ops_config_dir: "$tmp/private"
    ai_activate_cmd: "touch $tmp/activated"
  tasks:
    - ansible.builtin.include_tasks: "${AI_SQUID_TASKS:-$root/ansible/roles/ai_ops/tasks/deploy-squid-tree.yml}"
      tags: [always]
EOF
run() {
    local tags=()
    if [[ -n ${2:-} ]]; then tags=(--tags "$2"); fi
    ansible-playbook -i localhost, "$tmp/play.yml" -e "ai_ops_mode=$1" "${tags[@]}" >"$tmp/run.log" 2>&1 || {
        cat "$tmp/run.log" >&2
        return 1
    }
}
run plan sync-squid-source
[[ $(<"$tmp/remote/policy.json") == 'old source' ]]
[[ ! -e "$tmp/activated" ]]
run apply sync-squid-source
! rg -q 'Build tarball|Extract policy tree|Activate squid policy' "$tmp/run.log"
cmp "$tmp/private/data/egress-proxy/policy.json" "$tmp/remote/policy.json"
[[ ! -e "$tmp/activated" ]]
[[ $(stat -c '%a' "$tmp/remote/policy.json") == 600 ]]
backups=("$tmp/remote/"policy.json.*~)
[[ ${#backups[@]} == 1 && $(<"${backups[0]}") == 'old source' ]]
before=$(stat -c '%Y:%i' "$tmp/remote/policy.json")
run apply
[[ $(stat -c '%Y:%i' "$tmp/remote/policy.json") == "$before" ]]
[[ ! -e "$tmp/activated" ]]
backups=("$tmp/remote/"policy.json.*~)
[[ ${#backups[@]} == 1 ]]
printf 'source does not match deployed tree\n' >"$tmp/private/data/egress-proxy/policy.json"
if run apply sync-squid-source 2>"$tmp/expected-provenance-failure.log"; then
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
rg -q 'REMOTE DRIFT DETECTED' "$tmp/expected-failure.log"
[[ $(<"$tmp/remote/policy.json") == '{"services":{}}' ]]
[[ ! -e "$tmp/activated" ]]
echo 'PASS: scoped plan/repair, full-deploy idempotence, backup, permissions, provenance/drift refusal; no activation'
