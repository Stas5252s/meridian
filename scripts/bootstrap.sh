#!/usr/bin/env bash
# Bring up the local Meridian platform: kind cluster, ArgoCD, root Application.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TF_DIR="${ROOT_DIR}/infra/terraform"
KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/meridian}"
export KUBECONFIG

for tool in docker terraform kubectl helm jq; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "missing required tool: ${tool}" >&2
    exit 1
  fi
done

if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is not reachable. Start Docker Desktop or OrbStack and retry." >&2
  exit 1
fi

cd "${TF_DIR}"
terraform init -input=false

# Providers are configured from the dedicated kubeconfig, so create the cluster
# in its own apply before Terraform configures the Kubernetes/Helm providers.
echo "==> creating kind cluster"
terraform apply -auto-approve -input=false -target=kind_cluster.meridian

echo "==> building and loading the local checkout-api image"
docker build -t checkout-api:dev "${ROOT_DIR}/services/checkout-api"
CLUSTER_NAME="$(terraform output -raw cluster_name)"
docker save checkout-api:dev | docker exec -i "${CLUSTER_NAME}-control-plane" ctr -n k8s.io images import -

echo "==> installing ArgoCD and registering the root Application"
terraform apply -auto-approve -input=false

echo "==> waiting for the ArgoCD control plane"
kubectl -n argocd rollout status deployment/argocd-server --timeout=300s
kubectl -n argocd rollout status deployment/argocd-applicationset-controller --timeout=300s

echo "==> waiting for the GitOps tree to converge (up to 15m)"
"${ROOT_DIR}/scripts/wait-for-gitops.sh" --timeout 900

cat <<EOF

Meridian is up.

  ArgoCD UI     kubectl -n argocd port-forward svc/argocd-server 8081:80
                http://localhost:8081  (admin password: terraform -chdir=${TF_DIR} output -raw argocd_admin_password_command)
  Grafana       https://grafana.localtest.me:8443
                password: terraform -chdir=${TF_DIR} output -raw grafana_admin_password
  Storefront    https://checkout.localtest.me:8443
  Staging       https://checkout-staging.localtest.me:8443

  Smoke test    make smoke
  Load test     make load
  Tear down     make down

EOF
