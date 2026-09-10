#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PIDDIR="$ROOT/.demo-pids"
if [[ ! -d "$PIDDIR" ]]; then
  echo "no pid dir"
  exit 0
fi
for f in "$PIDDIR"/*.pid; do
  [[ -f "$f" ]] || continue
  pid=$(cat "$f")
  name=$(basename "$f" .pid)
  if kill -0 "$pid" 2>/dev/null; then
    echo "stopping $name ($pid)"
    kill "$pid" 2>/dev/null || true
  fi
  rm -f "$f"
done
echo "demo processes stopped"
