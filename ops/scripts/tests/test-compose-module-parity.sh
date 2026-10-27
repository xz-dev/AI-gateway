#!/usr/bin/env bash
# Real-daemon parity of the D12 activation (design D11): community.docker.docker_compose_v2
# with services: [target], recreate: always, dependencies: false, build: never, pull: policy
# and an explicit project name must recreate only the target, never pull or build, leave
# its dependency and neighbour untouched, change nothing in check mode, and run the same
# `up` argv as the old command up to flag spelling.
# Needs a Docker Engine with the Compose plugin at the renderer pin; without one it
# reports "no Docker Engine" and exits 0 (CI is the gate). PARITY_MUTATION=with-deps
# drops dependencies: false (the negative control): the test must then fail. The
# dependency is stopped first, because Compose leaves a running, up-to-date dependency
# alone even without --no-deps but starts a stopped one.
set -euo pipefail
root=$(cd "$(dirname "$0")/../../.." && pwd)
pin=$(sed -n 's/^ai_ops_compose_renderer_version: "\(.*\)"$/\1/p' "$root/ops/ansible/roles/ai_ops/defaults/main.yml")
real_docker=$(command -v docker || true)
if [[ -z $real_docker ]] || ! docker info --format '{{.ServerVersion}}' >/dev/null 2>&1 \
   || docker info --format '{{.Name}}' 2>/dev/null | grep -qi podman || docker version 2>/dev/null | grep -qi podman; then
    echo 'SKIP: no Docker Engine (the CI ansible job runs this test)'
    exit 0
fi
version=$(docker compose version --short)
[[ ${version%%+*} == "$pin" ]] || { echo "FAIL: Compose plugin $version differs from renderer pin $pin" >&2; exit 1; }

tmp=$(mktemp -d /var/tmp/compose-parity-XXXXXX)
project="parity-$RANDOM"
cleanup() { docker compose -p "$project" -f "$tmp/compose.yaml" down -t 0 >/dev/null 2>&1 || true; rm -rf "$tmp"; }
trap cleanup EXIT
image=busybox:1.36
docker image inspect "$image" >/dev/null 2>&1 || docker pull -q "$image" >/dev/null
cat >"$tmp/compose.yaml" <<EOF
services:
  dependency: {image: $image, command: [sleep, infinity], pull_policy: never}
  target: {image: $image, command: [sleep, infinity], pull_policy: never, depends_on: [dependency]}
  neighbour: {image: $image, command: [sleep, infinity], pull_policy: never}
EOF
printf 'UNUSED=1\n' >"$tmp/.env"
docker compose -p "$project" -f "$tmp/compose.yaml" --env-file "$tmp/.env" up -d --quiet-pull >/dev/null
docker compose -p "$project" -f "$tmp/compose.yaml" --env-file "$tmp/.env" stop -t 0 dependency >/dev/null
identity() { docker ps -aq --no-trunc --filter "label=com.docker.compose.project=$project" --filter "label=com.docker.compose.service=$1" | xargs docker inspect --format '{{.Id}} {{.State.StartedAt}} {{.Image}} {{.State.Running}}'; }
snapshot() { for s in dependency target neighbour; do echo "$s $(identity "$s")"; done; }
before=$(snapshot)

# Log the module's docker argv, then run the real CLI.
mkdir -p "$tmp/bin"
cat >"$tmp/bin/docker" <<EOF
#!/bin/sh
printf '%s\n' "\$*" >>"$tmp/docker-argv"
exec "$real_docker" "\$@"
EOF
chmod +x "$tmp/bin/docker"
deps=false
[[ ${PARITY_MUTATION:-} == with-deps ]] && deps=true
cat >"$tmp/play.yml" <<EOF
- hosts: localhost
  connection: local
  gather_facts: false
  module_defaults:
    community.docker.docker_compose_v2:
      project_src: "$tmp"
      project_name: "$project"
      files: ["$tmp/compose.yaml"]
      env_files: ["$tmp/.env"]
  tasks:
    - name: Activate exactly as ai_ops_managed_file does
      ansible.builtin.include_role:
        name: ai_ops_managed_file
        tasks_from: activate
      vars:
        ai_ops_managed_file_activate: {service: target, dependencies: $deps}
EOF
run() {
    PATH="$tmp/bin:$PATH" ANSIBLE_COLLECTIONS_PATH="$root/ops/ansible/collections" ANSIBLE_COLLECTIONS_SCAN_SYS_PATH=false \
    ANSIBLE_ROLES_PATH="$root/ops/ansible/roles" ansible-playbook -i localhost, "$tmp/play.yml" "$@" >"$tmp/run.log" 2>&1 \
        || { cat "$tmp/run.log" >&2; exit 1; }
}

run --check
[[ $(snapshot) == "$before" ]] || { echo 'FAIL: check mode changed a container' >&2; exit 1; }
: >"$tmp/docker-argv"
run
after=$(snapshot)
field() { awk -v s="$1" -v f="$2" '$1==s {print $f}' <<<"$3"; }
[[ $(field target 2 "$after") != "$(field target 2 "$before")" ]] || { echo 'FAIL: target was not recreated' >&2; exit 1; }
for s in dependency neighbour; do
    [[ $(grep "^$s " <<<"$after") == "$(grep "^$s " <<<"$before")" ]] || { echo "FAIL: $s changed" >&2; exit 1; }
done
for s in dependency target neighbour; do
    [[ $(field "$s" 4 "$after") == "$(field "$s" 4 "$before")" ]] || { echo "FAIL: $s image changed (pull/build)" >&2; exit 1; }
done
labels=$(docker ps -aq --filter "label=com.docker.compose.project=$project" | wc -l)
[[ $labels == 3 ]] || { echo "FAIL: project name is not $project" >&2; exit 1; }
up=$(grep -E '(^| )compose .* up ' "$tmp/docker-argv" | tail -1)
python3 - "$up" "$project" "$tmp" <<'PY'
import shlex, sys
argv, project, tmp = shlex.split(sys.argv[1]), sys.argv[2], sys.argv[3]
up = argv.index("up")
head, tail = argv[:up], argv[up + 1:]
def value(flag):
    return head[head.index(flag) + 1]
assert value("--project-name") == project, head
assert value("--project-directory") == tmp, head
assert value("--file") == f"{tmp}/compose.yaml" or value("-f") == f"{tmp}/compose.yaml", head
assert value("--env-file") == f"{tmp}/.env", head
flags, services = tail[:tail.index("--")], tail[tail.index("--") + 1:]
# The old argv: up -d --force-recreate --no-deps --no-build <svc>; the module adds
# --no-color --quiet-pull and the -- separator, and spells -d as --detach.
assert services == ["target"], tail
assert set(flags) == {"--detach", "--no-color", "--quiet-pull", "--force-recreate", "--no-deps", "--no-build"}, flags
assert not any(f.startswith("--pull") or f == "--build" for f in flags), flags
PY
echo "PASS: compose module parity on Docker Compose $version"
