#!/usr/bin/env bash
# Replace the placeholder GitHub owner/repository in every tracked manifest.
# Run this once after creating your fork, before pushing and bootstrapping.
set -euo pipefail

OWNER="${1:-}"
REPO="${2:-meridian}"

if [[ -z "${OWNER}" ]]; then
  cat >&2 <<'EOF'
usage: scripts/set-github-org.sh <github-owner> [repository]

example: scripts/set-github-org.sh acme meridian
EOF
  exit 2
fi

if [[ ! "${OWNER}" =~ ^[A-Za-z0-9_.-]+$ || ! "${REPO}" =~ ^[A-Za-z0-9_.-]+$ ]]; then
  echo "owner and repository must be valid GitHub names" >&2
  exit 2
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

mapfile_compat() {
  # macOS ships bash 3.2, which has no mapfile.
  find . -type f \
    \( -name '*.yaml' -o -name '*.yml' -o -name '*.json' -o -name '*.md' -o -name '*.tf' -o -name '*.sh' \) \
    -not -path './.git/*' \
    -not -path './infra/terraform/.terraform/*' \
    -not -path './.render/*' \
    -print0
}

changed=0
while IFS= read -r -d '' file; do
  tmp="$(mktemp)"
  cp "${file}" "${tmp}"
  perl -pi -e "s|github\\.com/[A-Za-z0-9_.-]+/${REPO}|github.com/${OWNER}/${REPO}|g; s|ghcr\\.io/[A-Za-z0-9_.-]+/${REPO}|ghcr.io/${OWNER}/${REPO}|g" "${file}"
  if ! cmp -s "${tmp}" "${file}"; then
    changed=$((changed + 1))
  fi
  rm -f "${tmp}"
done < <(mapfile_compat)

echo "updated ${changed} files for github.com/${OWNER}/${REPO}"
echo "review with: git diff --stat"
