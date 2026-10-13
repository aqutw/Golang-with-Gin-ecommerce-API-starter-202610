#!/usr/bin/env bash
set -euo pipefail
base_url=${BASE_URL:-http://127.0.0.1:8080}
: "${AUTH_EMAIL:?Export AUTH_EMAIL before running smoke}"
: "${AUTH_PASSWORD:?Export AUTH_PASSWORD before running smoke}"
credentials=$(python3 - <<'PY'
import json, os
print(json.dumps({'email': os.environ['AUTH_EMAIL'], 'password': os.environ['AUTH_PASSWORD']}))
PY
)
status=$(curl -sS -o /dev/null -w '%{http_code}' "$base_url/v1/auth/register" -H 'Content-Type: application/json' -d "$credentials")
[[ "$status" == 201 || "$status" == 409 ]] || { echo "Registration failed ($status)" >&2; exit 1; }
login=$(curl -fsS "$base_url/v1/auth/login" -H 'Content-Type: application/json' -d "$credentials")
access=$(printf '%s' "$login" | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
refresh=$(printf '%s' "$login" | python3 -c 'import json,sys; print(json.load(sys.stdin)["refresh_token"])')
curl -fsS "$base_url/healthz"
curl -fsS "$base_url/v1/products"
payload='{"items":[{"product_id":"keyboard","quantity":1},{"product_id":"mouse","quantity":1}],"coupon":"WELCOME10"}'
# Re-running with the same account replays the order instead of spending stock.
curl -fsS "$base_url/v1/checkouts" -H "Authorization: Bearer $access" \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: starter-smoke-v1' -d "$payload"
curl -fsS "$base_url/v1/checkouts" -H "Authorization: Bearer $access" \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: starter-smoke-v1' -d "$payload"
logout=$(printf '%s' "$refresh" | python3 -c 'import json,sys; print(json.dumps({"refresh_token": sys.stdin.read()}))')
curl -fsS "$base_url/v1/auth/logout" -H 'Content-Type: application/json' -d "$logout"
printf '\nAuthenticated smoke passed\n'
