#!/usr/bin/env bash
# Forward the local control-plane UIs. Ctrl-C stops all forwards.
set -euo pipefail

KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/meridian}"
export KUBECONFIG

trap 'kill 0' EXIT INT TERM

kubectl -n argocd port-forward svc/argocd-server 8081:80 &
kubectl -n monitoring port-forward svc/kube-prometheus-stack-grafana 3000:80 &
kubectl -n monitoring port-forward svc/kube-prometheus-stack-prometheus 9090:9090 &

cat <<'EOF'
ArgoCD    http://localhost:8081
Grafana   http://localhost:3000
Prometheus http://localhost:9090

Ctrl-C to stop.
EOF

wait
