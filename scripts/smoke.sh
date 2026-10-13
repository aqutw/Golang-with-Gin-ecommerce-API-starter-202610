#!/usr/bin/env bash
set -euo pipefail
base_url=${BASE_URL:-http://127.0.0.1:8080}
: "${API_TOKEN:?Export API_TOKEN before running smoke}"
curl -fsS "$base_url/healthz"
curl -fsS "$base_url/v1/products"
# Re-running this script replays the same order instead of spending more stock.
payload='{"items":[{"product_id":"keyboard","quantity":1},{"product_id":"mouse","quantity":1}],"coupon":"WELCOME10"}'
curl -fsS "$base_url/v1/checkouts" -H "Authorization: Bearer $API_TOKEN" \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: starter-smoke-v1' -d "$payload"
curl -fsS "$base_url/v1/checkouts" -H "Authorization: Bearer $API_TOKEN" \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: starter-smoke-v1' -d "$payload"
