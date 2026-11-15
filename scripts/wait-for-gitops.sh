#!/usr/bin/env bash
# Wait until every ArgoCD Application is Synced and Healthy.
set -euo pipefail

TIMEOUT=600
while [[ $# -gt 0 ]]; do
  case "$1" in
    --timeout)
      TIMEOUT="${2:?--timeout requires seconds}"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/meridian}"
export KUBECONFIG
deadline=$((SECONDS + TIMEOUT))

while ((SECONDS < deadline)); do
  if ! command -v kubectl >/dev/null 2>&1; then
    echo "kubectl is required" >&2
    exit 1
  fi

  data="$(kubectl -n argocd get applications -o json 2>/dev/null || echo '{"items":[]}')"
  total="$(jq '.items | length' <<<"${data}")"
  ready="$(jq '[.items[] | select((.status.sync.status // "") == "Synced" and (.status.health.status // "") == "Healthy")] | length' <<<"${data}")"
  pending="$(jq -r '.items[] | select((.status.sync.status // "") != "Synced" or (.status.health.status // "") != "Healthy") | "\(.metadata.name): sync=\(.status.sync.status // "Unknown") health=\(.status.health.status // "Unknown")"' <<<"${data}")"

  if ((total > 0 && ready == total)); then
    echo "all ${total} ArgoCD Applications are Synced and Healthy"
    exit 0
  fi

  echo "waiting for ArgoCD: ${ready}/${total} ready"
  [[ -n "${pending}" ]] && echo "${pending}" | sed 's/^/  /'
  sleep 10
done

echo "timed out after ${TIMEOUT}s waiting for ArgoCD Applications" >&2
kubectl -n argocd get applications -o wide >&2 || true
exit 1
