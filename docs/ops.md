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

```bash
bruiser config validate configs/example.yaml
bruiser doctor --profile configs/example.yaml --http http://127.0.0.1:8080
# Optional go-live gate:
bruiser doctor --front http://edge --origin http://origin
```

`BRUISER_MODE=dry-run` (or profile `mode: dry-run`, or admin **Dry run**)
places Bruiser in the real request path without blocking. Flip to enforce
without changing Edge / Proxy / Embedded placement.

## Upgrade and rollback

Bruiser upgrades independently of the merchant’s ticketing or commerce
platform. No origin schema change is required.

1. `bruiser config validate` against the running profile.
2. Apply the new image / binary (Helm `image.tag`, Compose pull, or replace
   the binary). Migrations are additive and run on start / migrate Job.
3. `bruiser doctor --http <control-plane>` and `/readyz`.
4. If dry-run is already on, read **What Bruiser would have stopped** before
   enabling enforcement.

**Rollback.** Redeploy the previous Bruiser image tag. Additive tables
(`runtime_controls`, `dry_run_*`, `waiters`) can remain; an older binary
ignores them. Do not reverse-migrate a store that a newer binary already
wrote unless the release notes say the down migration is safe. Protocol
tokens remain backward compatible for the supported v0 draft.

Configuration compatibility: unknown YAML fields are ignored; invalid mode,
lease, queue, identity, or route settings fail `config validate` before
serve.

## Backup and restore

Use the merchant’s existing Postgres backup (PITR / basebackup / snapshots).
Bruiser does not require Bruiser-managed cloud storage.

**Must back up**

| Table | Why |
|-------|-----|
| `executions`, `domains` | Active leases and fences |
| `waiters` | Intra-customer queue |
| `policies` | Versioned policy documents |
| `audit_events` | Why? trail (default 13 months) |
| `signing_keys` | Ed25519 execution/session keys |
| `sessions` | Live Bruiser sessions |
| `runtime_controls` | Dry-run / kill-switch state |
| `dry_run_holds`, `dry_run_events` | Hypothetical occupancy and 24h report |

**Can be reconstructed**

- In-process busy cache, JWKS cache, Prometheus gauges
- Expired `dry_run_holds` (TTL)
- Fair-use usage figures (derived)

**Recovery.** Restore Postgres, start Bruiser, wait until `GET /readyz`
returns `store=ok` and `signing_key=ok`. Allocation is fail-closed until
the store is ready — an unhealthy restore must not open concurrent
allocation. If restore fails, keep the previous replica or fail closed;
do not fail open on scarce-inventory routes unless an operator set
`fail_closed: false`.

**Retention.** Audit: `BRUISER_AUDIT_RETENTION` (default 13 months), purge
writes `AUDIT_PURGED`. Dry-run events live in the same database; drop or
truncate `dry_run_events` if the club does not need history.

## Backup

Back up the merchant Postgres instance with the usual PITR/basebackup
practice (see above). Restore is fail-closed on allocation until the store
is ready (`/readyz`).

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

- `GET /healthz` — process up (liveness)
- `GET /readyz` — JSON: `store`, `signing_key`, `mode`, `enforcement`, `upstream` (informational). 503 if store or signing key is not ready.
- `GET /metrics` — Prometheus (EAF, acquire outcomes, store unavailable)

## Emergency controls

`GET/PUT /v1/admin/controls` (admin secret). Audited as `CONTROL_CHANGED`.

| Control | Effect |
|---------|--------|
| `mode=dry-run` | Same path, never blocks; records `WOULD_*` |
| `enforcement=false` | Pass-through (`X-Bruiser-Control: bypass`) |
| `queue_enabled=false` | Intra-customer queue off (BUSY / WOULD_REJECT) |
| `max_waiters` | Override policy waiter cap |
| `lease_ttl_seconds` | Override lease TTL |
| `fail_closed=false` | Store errors become ALLOW (off by default) |
| `disabled_actions` | Route-level skip (e.g. `["purchase"]`) |
| `POST /v1/admin/controls/drain` | Emergency queue drain (`EMERGENCY_DRAIN`) |
| `POST /v1/admin/controls/revoke-all` | Emergency revoke (`EMERGENCY_REVOKE`) |

Scarce-inventory routes default fail-closed on an unhealthy instance.
