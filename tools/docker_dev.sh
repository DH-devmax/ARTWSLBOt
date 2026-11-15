#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf '%s\n' \
    'Usage: bash tools/docker_dev.sh COMMAND' \
    '  web         Build and activate Docker frontend HMR on the existing stack.' \
    '  web-build   Rebuild the development frontend image after dependency changes.' \
    '  api --confirm-api-restart' \
    '              Build and restart only API after checks, schema compatibility' \
    '              and unresolved-operation review; native connections may pause.' \
    '  status      Show the existing local Compose services.'
}

fail() {
  printf 'docker_dev: %s\n' "$*" >&2
  exit 1
}

action=${1:-help}
if [[ $# -gt 0 ]]; then
  shift
fi
case "$action" in
  help|-h|--help)
    usage
    exit 0
    ;;
  web|web-build|status)
    [[ $# -eq 0 ]] || fail "Unexpected arguments for $action."
    ;;
  api)
    [[ $# -eq 1 && $1 == --confirm-api-restart ]] \
      || fail "API restart requires completed checks and schema/operation review, then --confirm-api-restart."
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
project=${COMPOSE_PROJECT_NAME:-wsl-platform}
for file in compose.yaml compose.local.yaml compose.dev.yaml .env; do
  [[ -f "$root/$file" ]] || fail "Missing $file in $root; restore the existing local configuration first."
done
command -v docker >/dev/null || fail "Docker Engine and Docker Compose are required."
docker compose version >/dev/null

compose=(docker compose --project-directory "$root" --env-file "$root/.env"
  --project-name "$project" -f "$root/compose.yaml" -f "$root/compose.local.yaml")
dev_compose=("${compose[@]}" -f "$root/compose.dev.yaml")

require_project_owner() {
  local containers container owner
  containers=$(docker ps --all --filter "label=com.docker.compose.project=$project" --format '{{.ID}}')
  [[ -n "$containers" ]] || fail "Existing Compose project $project was not found."
  while IFS= read -r container; do
    owner=$(docker inspect --format '{{ index .Config.Labels "com.docker.compose.project.working_dir" }}' "$container")
    if [[ -d "$owner" ]]; then
      owner=$(cd -- "$owner" && pwd -P)
    fi
    [[ "$owner" == "$root" ]] \
      || fail "Compose project $project belongs to a different workspace; use its owning checkout."
  done <<< "$containers"
}

require_healthy_api() {
  local api state
  api=$("${compose[@]}" ps --quiet api)
  [[ -n "$api" ]] || fail "Frontend activation requires the existing healthy API."
  state=$(docker inspect --format '{{.State.Running}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' "$api")
  [[ "$state" == 'true healthy' ]] || fail "Frontend activation requires the existing healthy API."
}

require_project_owner
"${compose[@]}" config --quiet
case "$action" in
  web|web-build)
    "${dev_compose[@]}" config --quiet
    if [[ "$action" == web ]]; then
      require_healthy_api
    fi
    "${dev_compose[@]}" build web
    if [[ "$action" == web ]]; then
      require_project_owner
      require_healthy_api
      "${dev_compose[@]}" up -d --no-deps --no-build --wait web
    fi
    ;;
  api)
    WSL_API_IMAGE=wsl-api:dev "${compose[@]}" build api
    require_project_owner
    WSL_API_IMAGE=wsl-api:dev "${compose[@]}" up -d --no-deps --no-build --wait api
    ;;
  status)
    "${compose[@]}" ps
    ;;
esac
