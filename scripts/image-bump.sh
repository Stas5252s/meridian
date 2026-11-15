#!/usr/bin/env bash
# Bump the checkout-api image tag in an environment overlay and commit it.
# The commit is the deployment: ArgoCD reconciles staging automatically and a
# production change is reviewed as a pull request.
set -euo pipefail

usage() {
  cat <<'EOF'
usage: scripts/image-bump.sh <tag> [staging|prod]

Commits the new tag to the selected environment overlay. The caller pushes the
commit (staging) or opens a pull request (prod).
EOF
}

TAG="${1:-}"
ENV="${2:-staging}"

if [[ -z "${TAG}" || "${TAG}" == "-h" || "${TAG}" == "--help" ]]; then
  usage
  exit 2
fi

if [[ ! "${TAG}" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]]; then
  echo "invalid image tag: ${TAG}" >&2
  exit 2
fi

case "${ENV}" in
  staging | prod) ;;
  *)
    echo "environment must be staging or prod, got: ${ENV}" >&2
    exit 2
    ;;
esac

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VALUES_FILE="${ROOT_DIR}/deploy/charts/checkout-api/environments/${ENV}.yaml"

[[ -f "${VALUES_FILE}" ]] || { echo "missing ${VALUES_FILE}" >&2; exit 1; }

perl -0pi -e "s/^  tag: .*\$/  tag: ${TAG}/m" "${VALUES_FILE}"

if git -C "${ROOT_DIR}" diff --quiet -- "${VALUES_FILE}"; then
  echo "checkout-api already pinned to ${TAG} in ${ENV}"
  exit 0
fi

git -C "${ROOT_DIR}" add "${VALUES_FILE}"
GIT_AUTHOR_NAME="${GIT_AUTHOR_NAME:-meridian-bot}" \
GIT_AUTHOR_EMAIL="${GIT_AUTHOR_EMAIL:-meridian-bot@users.noreply.github.com}" \
GIT_COMMITTER_NAME="${GIT_COMMITTER_NAME:-meridian-bot}" \
GIT_COMMITTER_EMAIL="${GIT_COMMITTER_EMAIL:-meridian-bot@users.noreply.github.com}" \
  git -C "${ROOT_DIR}" commit -m "chore(gitops): deploy checkout-api ${TAG} to ${ENV} [skip ci]"

echo "committed ${TAG} to ${ENV}; push the branch to let ArgoCD reconcile"
