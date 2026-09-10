#!/bin/bash
# Packed Fly origin: same processes as docker-compose.demo.yml.
# Caddy binds :8080 (Fly http_service). Gateway is :8081 on loopback only.
set -euo pipefail

export POSTGRES_USER="${POSTGRES_USER:-bruiser}"
export POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-bruiser}"
export POSTGRES_DB="${POSTGRES_DB:-bruiser}"
export PGDATA="${PGDATA:-/var/lib/postgresql/data}"

export DEMO_CLUB_HOST="${DEMO_CLUB_HOST:-club.bruiser-gateway.com}"
export DEMO_TICKETS_HOST="${DEMO_TICKETS_HOST:-tickets.bruiser-gateway.com}"
export DEMO_ADMIN_HOST="${DEMO_ADMIN_HOST:-admin.bruiser-gateway.com}"
export SIMTIX_PUBLIC_URL="${SIMTIX_PUBLIC_URL:-https://${DEMO_TICKETS_HOST}}"
export DEMO_OPPONENT="${DEMO_OPPONENT:-Arsenal}"

export BRUISER_DEV_HMAC_SECRET="${BRUISER_DEV_HMAC_SECRET:-dev-secret-change-me}"
export BRUISER_EDGE_SECRET="${BRUISER_EDGE_SECRET:-edge-secret-dev}"
export BRUISER_ORIGIN_SECRET="${BRUISER_ORIGIN_SECRET:-origin-lock-dev}"
export BRUISER_OPERATOR_SECRET="${BRUISER_OPERATOR_SECRET:-operator-secret-dev}"
export HARCHESTER_SSO_SECRET="${HARCHESTER_SSO_SECRET:-harchester-sso-dev}"
export DEMO_ADMIN_SECRET="${DEMO_ADMIN_SECRET:-demo-admin-dev}"
export DEMO_ADMIN_PASSWORD="${DEMO_ADMIN_PASSWORD:-harchester}"

export BRUISER_HTTP_ADDR=":8081"
export BRUISER_DATABASE_URL="${BRUISER_DATABASE_URL:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:5432/${POSTGRES_DB}?sslmode=disable}"
export BRUISER_MERCHANT_ID=harchester
export BRUISER_MERCHANT_NAME="Harchester United FC"
export BRUISER_PROFILE="${BRUISER_PROFILE:-/configs/harchester.yaml}"
export HARCHESTER_DATABASE_URL="${HARCHESTER_DATABASE_URL:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:5432/harchester?sslmode=disable}"
export HARCHESTER_HTTP_ADDR=":8100"
export SIMTIX_HTTP_ADDR=":8090"
export SIMTIX_EDGE_ADDR=":8091"
export SIMTIX_BRUISER_URL="http://127.0.0.1:8081"
export SIMTIX_ORIGIN_URL="http://127.0.0.1:8090"
export SIMTIX_EDGE_URL="http://127.0.0.1:8091"
export SIMTIX_ORIGIN_SECRET="${BRUISER_ORIGIN_SECRET}"
export LOADLAB_HTTP_ADDR=":8120"
export LOADLAB_MAX_AGENTS="${LOADLAB_MAX_AGENTS:-10000}"
export HARCHESTER_URL="http://127.0.0.1:8100"
export ADMIN_HTTP_ADDR=":8110"
export BRUISER_URL="http://127.0.0.1:8081"
export BRUISER_BIN="/usr/local/bin/bruiser"
export LOADLAB_URL="http://127.0.0.1:8120"

mkdir -p /var/log/harchester
ensure_db() {
  local name="$1"
  if ! psql -U "$POSTGRES_USER" -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${name}'" | grep -q 1; then
    psql -U "$POSTGRES_USER" -d postgres -c "CREATE DATABASE ${name}"
  fi
}

echo "starting postgres"
docker-entrypoint.sh postgres >>/var/log/harchester/postgres.log 2>&1 &
for i in $(seq 1 90); do
  if pg_isready -U "$POSTGRES_USER" -h 127.0.0.1 >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
pg_isready -U "$POSTGRES_USER" -h 127.0.0.1
ensure_db harchester
ensure_db simtix

echo "starting gateway"
bruiser serve >>/var/log/harchester/gateway.log 2>&1 &
echo "starting simtix"
simtix-demo >>/var/log/harchester/simtix.log 2>&1 &
echo "starting harchester"
harchester >>/var/log/harchester/harchester.log 2>&1 &
echo "starting loadlab"
loadlab >>/var/log/harchester/loadlab.log 2>&1 &
echo "starting admin"
admin >>/var/log/harchester/admin.log 2>&1 &

for url in \
  http://127.0.0.1:8081/healthz \
  http://127.0.0.1:8090/healthz \
  http://127.0.0.1:8100/healthz \
  http://127.0.0.1:8120/healthz \
  http://127.0.0.1:8110/healthz
do
  ok=0
  for i in $(seq 1 90); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      ok=1
      break
    fi
    sleep 1
  done
  if [[ "$ok" != 1 ]]; then
    echo "timeout waiting for $url" >&2
    tail -n 40 /var/log/harchester/*.log >&2 || true
    exit 1
  fi
done

echo "origin processes ready; caddy on :8080"
exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
