#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export POSTGRES_ADMIN_URL="${POSTGRES_ADMIN_URL:-postgres://bruiser:bruiser@127.0.0.1:5432/postgres?sslmode=disable}"
cd "$ROOT"
go run ./demos/tools/preparedb
