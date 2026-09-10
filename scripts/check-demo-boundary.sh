#!/usr/bin/env bash
set -euo pipefail
# Fail if any demos/ package imports the product tree (internal/).
deps=$(go list -deps ./demos/...)
if echo "$deps" | grep -E 'github.com/.*/bruiser-gateway/internal(/|$)' >/dev/null; then
  echo "demo boundary violated: demos/ must not import internal/"
  echo "$deps" | grep -E 'bruiser-gateway/internal(/|$)' || true
  exit 1
fi
# Club site copy must not name Bruiser. Ignore the Go module import path.
if grep -n -i 'bruiser' demos/harchester-web/pages.go | grep -vi 'bruiser-gateway' >/dev/null; then
  echo "Harchester United pages must not mention Bruiser"
  grep -n -i 'bruiser' demos/harchester-web/pages.go | grep -vi 'bruiser-gateway' || true
  exit 1
fi
echo "demo boundary ok"
