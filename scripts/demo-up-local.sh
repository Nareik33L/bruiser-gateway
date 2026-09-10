#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PIDDIR="$ROOT/.demo-pids"
mkdir -p "$PIDDIR" "$ROOT/bin"
export BRUISER_DATABASE_URL="${BRUISER_DATABASE_URL:-postgres://bruiser:bruiser@127.0.0.1:5432/bruiser?sslmode=disable}"
export BRUISER_DEV_HMAC_SECRET="${BRUISER_DEV_HMAC_SECRET:-dev-secret-change-me}"
export BRUISER_MERCHANT_ID=harchester
export BRUISER_MERCHANT_NAME="Harchester United FC"
export BRUISER_PROFILE="$ROOT/configs/harchester.yaml"
export BRUISER_EDGE_SECRET=edge-secret-dev
export BRUISER_ORIGIN_SECRET=origin-lock-dev
export BRUISER_OPERATOR_SECRET=operator-secret-dev
export HARCHESTER_DATABASE_URL="${HARCHESTER_DATABASE_URL:-postgres://bruiser:bruiser@127.0.0.1:5432/harchester?sslmode=disable}"
export HARCHESTER_SSO_SECRET=harchester-sso-dev
export SIMTIX_PUBLIC_URL=http://127.0.0.1:8091
export SIMTIX_ORIGIN_URL=http://127.0.0.1:8090
export SIMTIX_BRUISER_URL=http://127.0.0.1:8080
export SIMTIX_ORIGIN_SECRET=origin-lock-dev
export DEMO_ADMIN_SECRET=demo-admin-dev
export DEMO_ADMIN_PASSWORD=harchester
export HARCHESTER_URL=http://127.0.0.1:8100
export SIMTIX_EDGE_URL=http://127.0.0.1:8091
export LOADLAB_URL=http://127.0.0.1:8120
export BRUISER_URL=http://127.0.0.1:8080
export BRUISER_BIN="$ROOT/bin/bruiser"

start() {
  local name="$1"; shift
  echo "starting $name"
  nohup "$@" >"$PIDDIR/$name.log" 2>&1 &
  echo $! >"$PIDDIR/$name.pid"
}

start gateway "$ROOT/bin/bruiser" serve
start simtix "$ROOT/bin/simtix-demo"
start harchester "$ROOT/bin/harchester"
start loadlab "$ROOT/bin/loadlab"
start admin "$ROOT/bin/admin"

echo "demo processes started. logs in $PIDDIR"
echo "  club    http://127.0.0.1:8100"
echo "  tickets http://127.0.0.1:8091"
echo "  admin   http://127.0.0.1:8110  (password: harchester)"
