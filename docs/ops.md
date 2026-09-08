# Bruiser Gateway — Operations

Self-hosted. The merchant owns Postgres, keys, audit, and networking.

## Deploy

**Compose (lab):**

```bash
docker compose -f deploy/compose/docker-compose.yml up --build
```

**Helm:**

```bash
helm upgrade --install bruiser deploy/helm/bruiser-gateway \
  --set secrets.BRUISER_DATABASE_URL='postgres://…' \
  --set secrets.BRUISER_DEV_HMAC_SECRET='…' \
  --set secrets.BRUISER_EDGE_SECRET='…' \
  --set secrets.BRUISER_ORIGIN_SECRET='…' \
  --set secrets.BRUISER_ADMIN_SECRET='…'
```

The chart can run a migrate Job (`migrate.enabled`, default true), optional
HPA, PDB, NetworkPolicy, and ServiceMonitor. Image signing keys are **not**
shipped — supply your own admission setup.

Migrations: `bruiser migrate` (also the Helm Job). The binary applies
`internal/store/postgres/migrations/*.sql` in filename order.

## Backup

Back up the merchant Postgres instance with the usual PITR/basebackup
practice. Critical tables: `executions`, `domains`, `waiters`, `policies`,
`audit_events`, `signing_keys`, `sessions`. Restore is fail-closed on
allocation until the store is ready (`/readyz`).

Audit default retention is 13 months (`BRUISER_AUDIT_RETENTION`). Purge
writes `AUDIT_PURGED`.

## Authority Check runbook

1. Place Bruiser in front of every scarce-inventory route (Embedded, Edge, or Proxy).
2. Lock the origin so a parallel path cannot allocate without Bruiser.
3. `bruiser authority-check --front <edge> --origin <origin>`
4. Expect PASS. A FAIL is not production-ready. The last report is also on
   `GET /v1/authority-check` (admin secret) and in audit as `AUTHORITY_CHECK`.

## Key rotation

| Secret | How |
|--------|-----|
| `BRUISER_DEV_HMAC_SECRET` / cookie JWT | Rotate in the merchant IdP and Bruiser together. Old cookies fail extract. |
| Edge-signed HMAC | Same: both Edge and Bruiser must share the new secret. |
| OIDC JWKS | Point `identity.jwks_url` at the IdP. Bruiser caches JWKS for 5 minutes. |
| Bruiser signing key (Ed25519) | `signing_keys` holds the current key. New processes call `EnsureSigningKey`. Downstream Embedded SDKs fetch `/.well-known/bruiser/jwks.json`. |
| `BRUISER_EDGE_SECRET` / `BRUISER_ORIGIN_SECRET` / `BRUISER_ADMIN_SECRET` | Restart with new env. Edge and origin must match in the same change window. |

## Heartbeat and sweep

Lease TTL default 60s, heartbeat 20s. The sweeper (`BRUISER_SWEEP_INTERVAL`,
default 2s) expires due leases and promotes the next intra-customer waiter
when policy `waiting.mode` is `bounded`.

## Health

- `GET /healthz` — process up
- `GET /readyz` — store reachable
- `GET /metrics` — Prometheus (EAF, acquire outcomes, store unavailable)
