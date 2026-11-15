# Service level objectives

Meridian treats reliability as a release blocker, not a dashboard decoration. The SLOs below are encoded in `deploy/charts/checkout-api/templates/prometheusrule.yaml`, visualised in the bundled Grafana dashboard (`uid: meridian-checkout-api`), and used by the Argo Rollouts canary gate.

## Service level indicators

| SLI | Definition | Source |
| --- | --- | --- |
| Availability | `1 - (5xx requests / all requests)` over the evaluation window | `http_requests_total{service="checkout-api"}` scraped from the application |
| Latency | p95 request duration for API routes | `http_request_duration_seconds_bucket{service="checkout-api"}` |
| Canary success | `1 - (canary ingress 5xx / all canary ingress requests)` over 2m | `nginx_ingress_controller_requests` |
| Canary latency | p95 duration of requests routed to the canary ingress over 2m | `nginx_ingress_controller_request_duration_seconds_bucket` |

Health probes and `/metrics` are excluded from request metrics by the application router, so probe traffic cannot inflate availability or latency.

## Objectives

| Environment | Availability objective | Latency objective | Window |
| --- | --- | --- | --- |
| Production | 99.5% | p95 < 300 ms | rolling 30 days |
| Staging | 99.5% (same rules, lower traffic) | p95 < 300 ms | rolling 30 days |

The 0.5% budget equals **216 minutes** of complete unavailability over 30 days, or any equivalent partial-failure mix.

## Error budget math

A burn rate of `Nx` spends the 30-day budget `N` times faster than the SLO permits. The alert thresholds are derived from the objective rather than hard-coded percentages:

```text
budget         = 1 - 0.995        = 0.005
fast threshold = budget * 14.4    = 0.0720  (7.2% error ratio)
slow threshold = budget * 6       = 0.0300  (3.0% error ratio)
ticket         = budget * 1       = 0.0050  (0.5% error ratio)
```

At 14.4x burn, 1 hour consumes 2% of the monthly budget; at 6x, 6 hours consumes 5%; at 1x, 1 day consumes about 3.3%.

## Recording rules

Per namespace the chart records:

| Rule | Window |
| --- | --- |
| `checkout_api:slo_error_ratio:5m` | 5 minutes |
| `checkout_api:slo_error_ratio:1h` | 1 hour |
| `checkout_api:slo_error_ratio:30m` | 30 minutes |
| `checkout_api:slo_error_ratio:6h` | 6 hours |
| `checkout_api:slo_error_ratio:2h` | 2 hours |
| `checkout_api:slo_error_ratio:1d` | 24 hours |
| `checkout_api:slo_latency_p95:5m` | 5 minutes |

The availability ratio is:

```promql
sum(rate(http_requests_total{namespace="checkout-prod", service="checkout-api", status=~"5.."}[5m]))
/
sum(rate(http_requests_total{namespace="checkout-prod", service="checkout-api"}[5m]))
```

## Alerts

| Alert | Condition | For | Severity | Response |
| --- | --- | --- | --- | --- |
| `CheckoutApiAvailabilityFastBurn` | 5m and 1h error ratios > 7.2% | 2m | critical, page | Stop the rollout, follow [fast-burn runbook](runbook.md#availability-fast-burn) |
| `CheckoutApiAvailabilitySlowBurn` | 30m and 6h error ratios > 3.0% | 15m | warning, ticket | Investigate trend, consider rollback |
| `CheckoutApiAvailabilityTicket` | 2h and 1d error ratios > 0.5% | 30m | info, ticket | Review budget, schedule follow-up |
| `CheckoutApiLatencySloBreach` | 5m p95 > 300 ms | 15m | warning | Check saturation and recent rollout |

Each alert requires both a short and a long window, which suppresses transient spikes while still detecting sustained burn.

Alert routing is intentionally a no-op receiver in the demo. Add PagerDuty/Slack receivers in `deploy/platform/kube-prometheus-stack/values.yaml`, then route `severity=critical` to the paging service and warnings to the team channel.

## Canary gate

Argo Rollouts sends 20% of traffic to the canary, waits 2 minutes, then runs the analysis template:

- success rate over 2m must stay at or above **99.5%** in production (**99%** staging),
- p95 latency over 2m must stay below **300 ms**.

The template runs every minute, five times, and tolerates at most two failed measurements. A failed analysis aborts the rollout automatically; the stable ReplicaSet keeps serving traffic.

## Error budget policy

1. **Budget > 50% remaining** — normal delivery.
2. **Budget 20–50% remaining** — production changes still allowed; the release reviewer must acknowledge the burn in the promotion PR.
3. **Budget < 20% remaining** — freeze non-urgent production changes until the budget recovers or a root-cause fix is merged.
4. **Budget exhausted** — only reliability fixes and rollbacks may be promoted. An exception requires an incident commander and a written follow-up.

The availability target, latency target and alert thresholds are values, not code: change `prometheusRule.slo` in an environment overlay to evaluate a different objective without touching templates.
