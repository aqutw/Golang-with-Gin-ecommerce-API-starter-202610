#!/usr/bin/env bash
# Exercise host deployment failure paths using a stateful Docker stub, offline.
set -euo pipefail
cd "$(dirname "$0")/.."
test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' EXIT
mkdir -p "$test_dir/bin"
cat > "$test_dir/bin/docker" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
cmd=$1
shift
case "$cmd" in
  pull) : ;;
  rm) rm -f "$STATE_DIR/${@: -1}" ;;
  container) test -f "$STATE_DIR/${@: -1}" ;;
  inspect)
    name=${@: -1}
    test -f "$STATE_DIR/$name"
    contents=$(cat "$STATE_DIR/$name")
    if [[ "$SCENARIO" == candidate-failure && "$name" == ecommerce-api-candidate ]] ||
       [[ "$SCENARIO" == health-failure && "$name" == ecommerce-api && "$contents" == new-running ]]; then
      echo unhealthy
    else
      echo healthy
    fi
    ;;
  run)
    arguments="$*"
    while [[ "$1" != --name ]]; do shift; done
    name=$2
    if [[ "$name" == ecommerce-api-candidate ]]; then
      [[ "$arguments" == *'/data:rw,noexec,nosuid,uid=10001,gid=10001,size=32m'* ]]
      [[ "$arguments" != *'source=ecommerce-api-data'* ]]
    else
      [[ "$arguments" == *'type=volume,source=ecommerce-api-data,target=/data'* ]]
    fi
    if [[ "$SCENARIO" == start-failure && "$name" == ecommerce-api ]]; then exit 1; fi
    echo new-running > "$STATE_DIR/$name"
    ;;
  stop) echo old-stopped > "$STATE_DIR/${@: -1}" ;;
  rename)
    if [[ "$SCENARIO" == rename-failure && "$2" == ecommerce-api-previous ]]; then exit 1; fi
    mv "$STATE_DIR/$1" "$STATE_DIR/$2"
    ;;
  start) echo old-running > "$STATE_DIR/$1" ;;
  *) echo "Unexpected Docker command: $cmd" >&2; exit 1 ;;
esac
STUB
chmod +x "$test_dir/bin/docker"
image="ghcr.io/example/starter/api@sha256:$(printf '%064d' 0)"
for scenario in success candidate-failure start-failure health-failure rename-failure; do
  state_dir="$test_dir/$scenario"
  mkdir "$state_dir"
  echo old-running > "$state_dir/ecommerce-api"
  touch "$state_dir/demo.env"
  result=0
  env PATH="$test_dir/bin:$PATH" STATE_DIR="$state_dir" SCENARIO="$scenario" \
    DEPLOY_ENV_FILE="$state_dir/demo.env" DEPLOY_LOCK_FILE="$state_dir/deploy.lock" \
    bash scripts/deploy.sh "$image" > "$state_dir/output" 2>&1 || result=$?
  if [[ "$scenario" == success ]]; then
    [[ "$result" == 0 && "$(cat "$state_dir/ecommerce-api")" == new-running ]]
    [[ -f "$state_dir/ecommerce-api-previous" ]]
  else
    [[ "$result" != 0 && "$(cat "$state_dir/ecommerce-api")" == old-running ]]
  fi
  [[ ! -f "$state_dir/ecommerce-api-candidate" ]]
  printf 'Deployment scenario passed: %s\n' "$scenario"
done
