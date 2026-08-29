#!/usr/bin/env bash
# shellcheck disable=SC2034
# Source this file to get portable container runtime and Compose commands.
#
# Defines:
#   AI_GATEWAY_RUNTIME         docker or podman
#   AI_GATEWAY_COMPOSE         array suitable for "${AI_GATEWAY_COMPOSE[@]}" <args>
#
# Re-detect on every source. Bash arrays cannot be exported, so child scripts
# may inherit a requested AI_GATEWAY_RUNTIME without inheriting the array.

_ai_gateway_use_runtime() {
  local runtime=$1
  command -v "$runtime" >/dev/null 2>&1 || return 1
  "$runtime" info >/dev/null 2>&1 || return 1
  if [ "$runtime" = docker ]; then
    docker compose version >/dev/null 2>&1 || return 1
    AI_GATEWAY_COMPOSE=(docker compose)
  elif podman compose version >/dev/null 2>&1; then
    AI_GATEWAY_COMPOSE=(podman compose)
  elif command -v podman-compose >/dev/null 2>&1; then
    AI_GATEWAY_COMPOSE=(podman-compose)
  else
    return 1
  fi
  AI_GATEWAY_RUNTIME=$runtime
}

case "${AI_GATEWAY_RUNTIME:-}" in
  docker|podman)
    _ai_gateway_use_runtime "$AI_GATEWAY_RUNTIME" || {
      echo "$AI_GATEWAY_RUNTIME and its Compose provider must be available" >&2
      exit 1
    }
    ;;
  "")
    _ai_gateway_use_runtime docker || _ai_gateway_use_runtime podman || {
      echo 'a running Docker or Podman engine with a Compose provider is required' >&2
      exit 1
    }
    ;;
  *)
    echo 'AI_GATEWAY_RUNTIME must be docker or podman' >&2
    exit 1
    ;;
esac

unset -f _ai_gateway_use_runtime
