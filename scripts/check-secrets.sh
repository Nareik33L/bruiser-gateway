#!/usr/bin/env bash
# Fails if well-known lab secrets appear outside the lab-only allowlist.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

pattern='admin-secret-dev|edge-secret-dev|origin-lock-dev|operator-secret-dev|dev-secret-change-me'
allow='(^|/)(Makefile|SECURITY\.md|\.gitleaks\.toml|scripts/check-secrets\.sh)$|_test\.go$|(^|/)internal/(testlab/|config/config\.go|check/check\.go)|(^|/)cmd/simtix/main\.go$'

hits="$(git grep -nI -E "$pattern" -- . ':!.git' || true)"
if [[ -z "$hits" ]]; then
  echo "secret-scan: no well-known lab secrets"
  exit 0
fi

bad=0
while IFS= read -r line; do
  file="${line%%:*}"
  if [[ "$file" =~ $allow ]]; then
    continue
  fi
  echo "secret-scan: forbidden lab secret in $line"
  bad=1
done <<< "$hits"

if [[ "$bad" -ne 0 ]]; then
  echo "secret-scan: commit unique secrets via the environment, not the tree" >&2
  exit 1
fi
echo "secret-scan: well-known lab secrets confined to allowlisted lab sources"
