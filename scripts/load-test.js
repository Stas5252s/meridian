import http from 'k6/http';
import { check, sleep } from 'k6';
import exec from 'k6/execution';

const BASE_URL = __ENV.BASE_URL || 'https://checkout-staging.localtest.me:8443';

export const options = {
  scenarios: {
    checkout_journey: {
      executor: 'ramping-arrival-rate',
      startRate: 5,
      timeUnit: '1s',
      preAllocatedVUs: 10,
      maxVUs: 60,
      stages: [
        { target: 20, duration: '30s' },
        { target: 20, duration: '1m' },
        { target: 0, duration: '15s' },
      ],
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<300', 'p(99)<750'],
    checks: ['rate>0.99'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  insecureSkipTLSVerify: __ENV.INSECURE_SKIP_TLS === 'true',
};

export default function () {
  const customerId = `cust_k6_${exec.vu.idInTest}`;
  const key = `k6-${exec.vu.idInTest}-${exec.scenario.iterationInTest}`;
  const payload = JSON.stringify({
    customerId,
    items: [{ sku: 'sku_load', quantity: 1, unitPriceCents: 1999 }],
  });
  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': key,
    },
    tags: { route: 'create_order' },
  };

  const created = http.post(`${BASE_URL}/v1/orders`, payload, params);
  check(created, {
    'create returns 201': (r) => r.status === 201,
    'create has order id': (r) => {
      try {
        return typeof r.json('id') === 'string' && r.json('id').startsWith('ord_');
      } catch (_) {
        return false;
      }
    },
  });

  let orderId;
  try {
    orderId = created.json('id');
  } catch (_) {
    orderId = null;
  }

  if (orderId) {
    const fetched = http.get(`${BASE_URL}/v1/orders/${orderId}`, {
      tags: { route: 'get_order' },
    });
    check(fetched, { 'get returns 200': (r) => r.status === 200 });
  }

  const listed = http.get(`${BASE_URL}/v1/orders?customerId=${customerId}&limit=10`, {
    tags: { route: 'list_orders' },
  });
  check(listed, {
    'list returns 200': (r) => r.status === 200,
    'list contains an item': (r) => {
      try {
        return r.json('count') >= 1;
      } catch (_) {
        return false;
      }
    },
  });

  sleep(0.2);
}
