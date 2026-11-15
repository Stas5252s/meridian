# Operations runbook

This runbook assumes:

```bash
export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/meridian}"
```

The Argo Rollouts kubectl plugin is optional; plain `kubectl` equivalents are shown.

## First five minutes

```bash
kubectl get applications -n argocd
kubectl get rollout,pods -n checkout-prod -o wide
kubectl get analysisrun -n checkout-prod
kubectl get events -n checkout-prod --sort-by=.lastTimestamp | tail -30
curl -sSk https://checkout-prod.localtest.me:8443/readyz
```

Check the Grafana dashboard **Meridian / Checkout API** (`uid: meridian-checkout-api`) for availability, p95, burn rate, pod restarts, in-flight requests and ingress 5xx. Prometheus is reachable with `make port-forward` at http://localhost:9090.

Useful queries:

```promql
checkout_api:slo_error_ratio:5m{namespace="checkout-prod"}
checkout_api:slo_latency_p95:5m{namespace="checkout-prod"}
sum(rate(http_requests_total{namespace="checkout-prod", service="checkout-api"}[5m])) by (route, status)
histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{namespace="checkout-prod", service="checkout-api"}[5m])) by (le, route))
sum(kube_pod_container_status_restarts_total{namespace="checkout-prod"}) by (pod)
```

## Availability fast burn

Alert: `CheckoutApiAvailabilityFastBurn` (critical, 5m and 1h error ratio above 7.2%).

1. Confirm this is real: query 5xx by route. A single client hammering an invalid endpoint typically produces 4xx, not 5xx.
2. If a rollout is in progress, pause or abort it:
   ```bash
   kubectl argo rollouts pause rollout/checkout-api-prod -n checkout-prod
   kubectl argo rollouts abort rollout/checkout-api-prod -n checkout-prod
   ```
   Without the plugin, set `spec.paused: true` or revert the image tag in `environments/prod.yaml` and sync.
3. Inspect logs for the error signature:
   ```bash
   kubectl -n checkout-prod logs deploy/checkout-api-prod --since=15m --tail=200
   ```
   Logs are JSON with `request_id`, `route`, `status`, `duration_ms`.
4. Check resource pressure and restarts:
   ```bash
   kubectl -n checkout-prod top pods
   kubectl -n checkout-prod get pods
   kubectl -n checkout-prod describe pod <pod>
   ```
5. If the previous revision was healthy and the error started with the current revision, roll back:
   ```bash
   kubectl argo rollouts undo rollout/checkout-api-prod -n checkout-prod
   ```
   Then commit a Git revert so ArgoCD does not re-apply the bad version.
6. If the cause is upstream (ingress, DNS, Prometheus) and not the application, treat the burn as an infrastructure incident and keep the rollback decision explicit in the incident channel.

## Availability slow burn

Alert: `CheckoutApiAvailabilitySlowBurn` (warning, 30m and 6h above 3%).

This is a trend, not a page. Compare per-route error ratios and look for a persistent 5xx class. Check whether an HPA event, node pressure or canary weight change precedes the burn. If the trend continues for another 30 minutes, treat it as fast burn and consider rollback. Open a ticket if the cause is a low-frequency dependency failure that cannot be fixed immediately.

## Availability ticket

Alert: `CheckoutApiAvailabilityTicket` (info, 2h and 1d above 0.5%).

Review the error budget policy in [slo.md](slo.md#error-budget-policy). If production budget is below 20%, freeze non-urgent promotions until the next budget recovery. Document the follow-up in the promotion PR.

## Latency breach

Alert: `CheckoutApiLatencySloBreach` (p95 above 300 ms for 15 minutes).

1. Break latency down by route and pod:
   ```promql
   histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{namespace="checkout-prod", service="checkout-api"}[5m])) by (le, route))
   histogram_quantile(0.95, sum(rate(nginx_ingress_controller_request_duration_seconds_bucket{namespace="checkout-prod"}[5m])) by (le, ingress))
   ```
2. Check saturation: CPU throttling, memory pressure, in-flight requests, node load.
3. Check for a recent rollout or a change in the ingress/analysis path.
4. The application has an intentional fault-injection knob, `config.faultLatencyMs`, used for canary demonstrations. Confirm it is `0` in the environment overlay.
5. If latency is caused by the current revision, abort the rollout or roll back as above.

## Canary is stuck or failed

```bash
kubectl -n checkout-prod get rollout checkout-api-prod -o wide
kubectl -n checkout-prod get analysisrun
kubectl -n checkout-prod describe analysisrun <name>
kubectl argo rollouts get rollout checkout-api-prod -n checkout-prod --watch
```

Common causes:

- **Analysis query returns no data** — the canary ingress has no traffic, or the `ingress` label does not match `<release>-canary`. Verify the canary Ingress exists and nginx exports metrics.
- **Prometheus unreachable** — check the AnalysisTemplate's `prometheusAddress` points at `http://prometheus-operated.monitoring.svc.cluster.local:9090` and that the monitoring app is healthy.
- **Failed status below threshold** — abort the rollout, inspect logs, fix forward in Git or revert, then promote again.
- **Paused indefinitely** — a `pause: {}` step (no duration) requires a human; use `kubectl argo rollouts promote`.

To recover:

```bash
kubectl argo rollouts abort rollout/checkout-api-prod -n checkout-prod
# fix Git, let ArgoCD sync, then:
kubectl argo rollouts promote rollout/checkout-api-prod -n checkout-prod
```

## ArgoCD out of sync or degraded

```bash
kubectl -n argocd get applications
argocd app get checkout-api-prod
argocd app diff checkout-api-prod
argocd app sync checkout-api-prod
```

- `ComparisonError` usually means the Git remote is unreachable, the path is wrong, or a Helm value fails schema validation. Check the application conditions and repo-server logs:
  ```bash
  kubectl -n argocd logs deploy/argocd-repo-server --tail=200
  ```
- The root app and platform apps auto-sync with self-heal. Manual `kubectl edit` changes are reverted; change Git instead.
- The production app has no automated sync by design. A merge alone does not deploy.
- If a sync is stuck on CRDs, confirm the monitoring/rollouts apps are healthy; checkout apps are in a later sync wave.

## Kyverno denied admission

```bash
kubectl -n checkout-prod describe pod <pod>
kubectl get validatingpolicy,imagevalidatingpolicy,mutatingpolicy
kubectl get policyreport -A
kubectl get clusterpolicyreport
```

Typical messages:

- **signed images** — the image is not signed by the release workflow, the SBOM attestation is missing, or the keyless subject/issuer does not match. Verify externally:
  ```bash
  cosign verify ghcr.io/<owner>/<repo>/checkout-api:<tag> \
    --certificate-identity-regexp 'https://github.com/<owner>/<repo>/.github/workflows/release.yml@refs/tags/' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com
  cosign verify-attestation --type spdxjson ghcr.io/<owner>/<repo>/checkout-api:<tag> \
    --certificate-identity-regexp 'https://github.com/<owner>/<repo>/.github/workflows/release.yml@refs/tags/' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com
  ```
- **registry** — production only allows `ghcr.io/<owner>/<repo>/`. Fix the image reference; do not weaken the policy.
- **probes / resources / security context** — the chart renders these by default; a hand-written pod in a checkout namespace will be reported or denied.
- **audit policies** — do not block admission, but produce policy reports. Read the report before assuming the workload is compliant.

An emergency exception must be a reviewed Kyverno `PolicyException` with an owner and expiry, not an `Enforce → Audit` flip.

## TLS and certificate problems

```bash
kubectl -n checkout-prod get certificate,certificaterequest,order,challenge
kubectl -n cert-manager logs deploy/cert-manager --tail=200
kubectl get clusterissuer
```

The demo uses a self-signed `ClusterIssuer`, so browsers warn by design. A real cluster should use an ACME issuer or an internal CA and should not ship the self-signed issuer.

## Capacity and HPA

```bash
kubectl -n checkout-prod get hpa
kubectl -n checkout-prod describe hpa checkout-api-prod
kubectl -n checkout-prod top pods
kubectl get nodes
```

HPA scales on CPU (65% target in production, 3–12 replicas). If the HPA is at maximum and latency is high, check node capacity and the PodDisruptionBudget (`minAvailable: 2`). Never scale below the PDB during an incident without acknowledging the reduced availability.

## Rollback

Preferred order:

1. **Abort canary** — `kubectl argo rollouts abort rollout/checkout-api-prod -n checkout-prod`.
2. **Git revert** the image bump so ArgoCD converges to the previous tag. For production, open a revert PR and follow the normal promotion path.
3. **Rollout undo** — `kubectl argo rollouts undo rollout/checkout-api-prod -n checkout-prod --to-revision=<n>` for an immediate local rollback; record the revision and reconcile Git afterwards.

After recovery, add the timeline, customer impact, detection source, and follow-up actions to the incident record. If an alert did not fire when it should have, file a reliability bug against the SLO rules.
