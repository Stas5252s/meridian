#!/usr/bin/env bash
# End-to-end API smoke test against a running environment.
set -euo pipefail

BASE_URL="${BASE_URL:-https://checkout.localtest.me:8443}"
CUSTOMER_ID="cust_smoke_$(date +%s)_$$"
IDEMPOTENCY_KEY="smoke-$(date +%s)-${RANDOM}-${RANDOM}"

# The local/staging/prod ingresses use a self-signed ClusterIssuer. Opt out with
# SMOKE_VERIFY_TLS=1 when the endpoint has a publicly trusted certificate.
CURL_OPTS=(-sS)
if [[ "${BASE_URL}" == https://* && "${SMOKE_VERIFY_TLS:-0}" != "1" ]]; then
  CURL_OPTS+=(-k)
fi

for tool in curl jq; do
  command -v "${tool}" >/dev/null 2>&1 || { echo "missing required tool: ${tool}" >&2; exit 1; }
done

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

wait_for_http() {
  local url="$1" attempts=60
  for ((i = 1; i <= attempts; i++)); do
    if curl -fsS --max-time 2 "${CURL_OPTS[@]}" "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for ${url}" >&2
  return 1
}

request() {
  local method="$1" path="$2" expected="$3" body="${4:-}" key="${5:-}"
  local args=("${CURL_OPTS[@]}" -X "${method}" -o "${tmpdir}/body.json" -w '%{http_code}' -H 'Accept: application/json')
  [[ -n "${body}" ]] && args+=(-H 'Content-Type: application/json' -d "${body}")
  [[ -n "${key}" ]] && args+=(-H "Idempotency-Key: ${key}")
  local code
  code="$(curl "${args[@]}" "${BASE_URL}${path}")"
  if [[ "${code}" != "${expected}" ]]; then
    echo "FAIL ${method} ${path}: expected ${expected}, got ${code}" >&2
    cat "${tmpdir}/body.json" >&2 || true
    echo >&2
    exit 1
  fi
  echo "ok   ${method} ${path} -> ${code}"
}

echo "smoke testing ${BASE_URL}"

wait_for_http "${BASE_URL}/healthz"
wait_for_http "${BASE_URL}/readyz"
request GET /healthz 200
request GET /readyz 200

payload="$(jq -nc --arg customer "${CUSTOMER_ID}" '{customerId: $customer, items: [{sku: "sku_smoke", quantity: 2, unitPriceCents: 1500}]}')"
request POST /v1/orders 201 "${payload}" "${IDEMPOTENCY_KEY}"
order_id="$(jq -r '.id' "${tmpdir}/body.json")"
[[ "${order_id}" == ord_* ]] || { echo "FAIL unexpected order id: ${order_id}" >&2; exit 1; }
[[ "$(jq -r '.totalCents' "${tmpdir}/body.json")" == "3000" ]] || { echo "FAIL totalCents mismatch" >&2; exit 1; }
echo "ok   created ${order_id}"

request POST /v1/orders 200 "${payload}" "${IDEMPOTENCY_KEY}"
[[ "$(jq -r '.id' "${tmpdir}/body.json")" == "${order_id}" ]] || { echo "FAIL idempotent replay returned a different order" >&2; exit 1; }
echo "ok   idempotent replay returned the original order"

conflict_payload="$(jq -nc --arg customer "${CUSTOMER_ID}" '{customerId: $customer, items: [{sku: "sku_other", quantity: 1, unitPriceCents: 500}]}')"
request POST /v1/orders 409 "${conflict_payload}" "${IDEMPOTENCY_KEY}"
[[ "$(jq -r '.error.code' "${tmpdir}/body.json")" == "idempotency_conflict" ]] || { echo "FAIL expected idempotency_conflict" >&2; exit 1; }

request POST /v1/orders 422 '{"customerId":"","items":[]}' ""
[[ "$(jq -r '.error.code' "${tmpdir}/body.json")" == "validation_error" ]] || { echo "FAIL expected validation_error" >&2; exit 1; }

request GET "/v1/orders/${order_id}" 200
[[ "$(jq -r '.id' "${tmpdir}/body.json")" == "${order_id}" ]] || { echo "FAIL get order id mismatch" >&2; exit 1; }

request GET "/v1/orders?customerId=${CUSTOMER_ID}" 200
[[ "$(jq -r '.count' "${tmpdir}/body.json")" -ge 1 ]] || { echo "FAIL customer order list is empty" >&2; exit 1; }

echo "smoke test passed"
