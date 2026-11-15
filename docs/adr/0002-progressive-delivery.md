# ADR 0002: Progressive delivery with Argo Rollouts and ingress-nginx

- Status: Accepted
- Date: 2026-11-15

## Context

Kubernetes Deployments roll pods but do not evaluate whether a new version is healthy under real traffic. The platform needs a deployment gate that can stop a bad release before it reaches all users, using the same SLO signals that operators trust.

## Decision

Use Argo Rollouts with the nginx traffic-routing integration for staging and production.

- Local uses a plain Deployment to keep the laptop demo small.
- Staging and production use a Rollout with a stable Service and a canary Service.
- The canary Ingress carries `nginx.ingress.kubernetes.io/canary: "true"` and a weight managed by the rollout controller.
- Steps: 20% for 2 minutes, analysis, 50% for 2 minutes, then 100%.
- Analysis queries Prometheus (`kube-prometheus-stack`) every minute for 2m windows of nginx ingress metrics:
  - success rate ≥ 99% staging, ≥ 99.5% production,
  - p95 latency < 300 ms.
- Five measurements with `failureLimit: 2`; a failed analysis aborts the rollout and the stable service keeps traffic.
- The Deployment and Rollout share one pod template helper so probes, security context and environment cannot drift.

## Consequences

- A bad release is limited to the canary weight and is aborted automatically.
- The gate depends on ingress metrics. A canary with no traffic (or a broken metric label) produces no data and blocks promotion; this is a false negative that the runbook addresses explicitly.
- The canary Ingress must route the same host/path as the stable Ingress. The chart fails rendering if rollout traffic routing is enabled without ingress.
- Argo Rollouts is another control-plane component; it is installed from Git at sync wave -10.
- Rollout objects are not Deployments. Teams must use `kubectl argo rollouts` (or the CRD) for promotion, abort and undo.
- The rollout controller owns `.spec.selector` on both Services: it injects `rollouts-pod-template-hash`. Helm and ArgoCD must therefore not fight it. The ApplicationSet and prod Application ignore `.spec.selector` on the two Services, and an ad-hoc `helm upgrade` of a Rollout release needs `--force-conflicts` because the field is owned by `rollouts-controller` under server-side apply.

## Alternatives considered

- **Native Deployment rolling update** — no traffic-level gate and no automatic analysis. Rejected for staging/production.
- **Service mesh (Istio/Linkerd) traffic split** — stronger traffic management but a much heavier local footprint and a second control plane. Rejected for this demo; the chart's `trafficRouting` block is the seam.
- **Feature flags only** — excellent complement, not a substitute for deployment-level gating. Out of scope.
