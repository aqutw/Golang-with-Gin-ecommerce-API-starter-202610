#!/usr/bin/env bash
# Run on the Linux Docker host; commerce data lives in a persistent named volume.
set -euo pipefail
image=${1:?Expected immutable image reference}
if [[ ! "$image" =~ ^ghcr\.io/[a-z0-9._/-]+@sha256:[a-f0-9]{64}$ ]]; then
  echo 'Expected ghcr.io image pinned by sha256 digest' >&2; exit 1
fi
env_file=${DEPLOY_ENV_FILE:-/opt/ecommerce/demo.env}
[[ -r "$env_file" ]] || { echo "Missing $env_file" >&2; exit 1; }
lock_file=${DEPLOY_LOCK_FILE:-/opt/ecommerce/deploy.lock}
exec 9>"$lock_file"
flock -n 9 || { echo 'Another deployment is running' >&2; exit 1; }
name=ecommerce-api
candidate=ecommerce-api-candidate
backup=ecommerce-api-previous
had_previous=false
replacing=false
completed=false

healthy() {
  local container=$1
  for attempt in {1..30}; do
    state=$(docker inspect --format '{{.State.Health.Status}}' "$container" 2>/dev/null || true)
    [[ "$state" == healthy ]] && return 0
    [[ "$state" == unhealthy ]] && return 1
    printf 'Waiting for %s (%s/30)\n' "$container" "$attempt"
    sleep 2
  done
  return 1
}
start() {
  local data_mount=(--mount 'type=volume,source=ecommerce-api-data,target=/data')
  if [[ "$1" == "$candidate" ]]; then
    # A health-check candidate must not migrate or seed the live database.
    data_mount=(--tmpfs '/data:rw,noexec,nosuid,uid=10001,gid=10001,size=32m')
  fi
  docker run -d --name "$1" --restart unless-stopped --env-file "$env_file" \
    -e APP_ENV=demo -e HTTP_ADDR=:8080 -e DB_PATH=/data/ecommerce.db "${data_mount[@]}" \
    --read-only --tmpfs /tmp:rw,noexec,nosuid,size=16m \
    --cap-drop ALL --security-opt no-new-privileges --memory 256m --cpus 1 \
    --log-opt max-size=10m --log-opt max-file=3 -p "127.0.0.1:$2:8080" "$image"
}
cleanup() {
  local result=$?
  trap - EXIT
  docker rm -f "$candidate" >/dev/null 2>&1 || true
  if [[ "$completed" != true && "$replacing" == true ]]; then
    if [[ "$had_previous" == true ]]; then
      if docker container inspect "$backup" >/dev/null 2>&1; then
        docker rm -f "$name" >/dev/null 2>&1 || true
        if ! docker rename "$backup" "$name"; then
          echo 'Failed to restore previous container name; host attention required' >&2; exit 1
        fi
      fi
      if docker start "$name" >/dev/null && healthy "$name"; then
        echo 'Deployment failed; previous container restored' >&2
      else
        echo 'Rollback container is not healthy; host attention required' >&2
      fi
    else
      docker rm -f "$name" >/dev/null 2>&1 || true
    fi
    result=1
  fi
  exit "$result"
}
docker pull "$image"
docker rm -f "$candidate" >/dev/null 2>&1 || true
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
start "$candidate" 18080
healthy "$candidate" || { echo 'Candidate health check failed; current service retained' >&2; exit 1; }
docker rm -f "$candidate" >/dev/null
if docker container inspect "$backup" >/dev/null 2>&1; then
  docker rm -f "$backup" >/dev/null
fi
if docker container inspect "$name" >/dev/null 2>&1; then
  had_previous=true
  replacing=true
  docker stop -t 15 "$name" >/dev/null
  docker rename "$name" "$backup"
fi
replacing=true
if start "$name" 8080 && healthy "$name"; then
  completed=true
  echo "Deployed $image"
else
  exit 1
fi
