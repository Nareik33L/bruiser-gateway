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

`BRUISER_MODE=dry-run` or admin **0%** places Bruiser in the real request
path without blocking. Then raise `enforce_percent` (10 / 25 / 50 / 75 /
100) on `PUT /v1/admin/controls` — no redeploy. `BRUISER_ENFORCE_PERCENT`
sets the boot default. Scope (`events`, `routes`, `pools`, `cohorts`,
`environments`, `policies`) limits who is eligible; everyone else is still
observed. Emergency **0%** is dry-run again.

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
4. Expect PASS. `--origin` and `--control` are required. A FAIL or a
   missing origin is not production-ready. The last report is also on
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

## Prometheus / EAF

`bruiser_observed_eaf` and `bruiser_downstream_eaf` are **process-local
gauges**. Each replica only sees the traffic that hit that process.
**Do not sum them across replicas.** `sum(bruiser_observed_eaf)` is not
a cluster amplification factor.

Admin `GET /v1/admin/status` `eaf` is the same: per process.

The counters **are** additive:

| Metric | Sum across replicas? |
|--------|----------------------|
| `bruiser_allocation_attempts_total` | Yes |
| `bruiser_executions_forwarded_total` | Yes |
| `bruiser_acquire_total` | Yes |
| `bruiser_observed_eaf` | **No** |
| `bruiser_downstream_eaf` | **No** |
| `bruiser_queue_depth` | No (gauge; use `max` or admin `usage`) |

Cluster observed EAF (the correct aggregate):

```promql
sum(increase(bruiser_allocation_attempts_total[5m]))
/
clamp_min(sum(increase(bruiser_executions_forwarded_total[5m])), 1)
```

A ready recording rule is in `deploy/prometheus/eaf.rules.yaml`
(`bruiser:observed_eaf:ratio`). Load that file; do not change how the
process gauges work.

## Identity trust boundary

Bruiser **consumes** the merchant’s authenticated customer identifier.
It does not mint one and it does not decide who a human is.

- Signed extractors (`cookie-jwt`, `bearer-jwt`, `oidc`, `edge-signed`)
  are the V1 demonstration. The Arsenal-like lab uses a cookie JWT.
- `identity.extractor: header` (and the `auto` fallback to
  `X-Customer-Id`) trusts an **unsigned** header. Accept that only from
  a trusted, authenticated edge (mTLS, network policy, or the edge
  secret already wrapping `/v1/authorize`). If clients can set
  `X-Customer-Id` themselves, they pick their customer.
- Stronger signed-identity options can come later. Unsigned-header
  hardening is not a V1 blocker when the club uses JWT/OIDC the way the
  lab already does.

## Emergency controls

`GET/PUT /v1/admin/controls` (admin secret). Audited as `CONTROL_CHANGED`.

| Control | Effect |
|---------|--------|
| `mode=dry-run` / `enforce_percent=0` | 100% observe, 0% enforce; records `WOULD_*` |
| `enforce_percent` | Active enforcement share (customer-stable hash) |
| `scope` | Limit ramp to event / route / pool / cohort / env / policy |
| `enforcement=false` | Kill switch: effective 0% (still observes) |
| `queue_enabled=false` | Intra-customer queue off (BUSY / WOULD_REJECT) |
| `max_waiters` | Override policy waiter cap |
| `lease_ttl_seconds` | Override lease TTL |
| `fail_closed=false` | Store errors become ALLOW (off by default) |
| `disabled_actions` | Route-level skip (e.g. `["purchase"]`) |
| `POST /v1/admin/controls/drain` | Emergency queue drain (`EMERGENCY_DRAIN`) |
| `POST /v1/admin/controls/revoke-all` | Emergency revoke (`EMERGENCY_REVOKE`) |

Scarce-inventory routes default fail-closed on an unhealthy instance.

## Progressive enforcement assignment

`enforce_percent` controls **active enforcement only**. Observation,
evaluation, and recording stay at 100% of traffic Bruiser sees.

Assignment is at the **customer** grain, not per request or per agent:

```
bucket = FNV-64a( ramp_salt || 0x00 || customer_id ) % 100
enforce  = (bucket < enforce_percent)
```

- `customer_id` is the merchant-authenticated identifier Bruiser
  consumed (cookie JWT `sub`, bearer subject, edge-signed header, or
  introspected subject). Bruiser does not invent one. Unsigned identity
  headers are only safe behind a trusted edge — see **Identity trust
  boundary**.
- `ramp_salt` is optional (`PUT /v1/admin/controls`). Changing it
  reshuffles the cohort; the new mapping is deterministic and auditable
  via `CONTROL_CHANGED`.
- Every agent, browser, device, and reconnect for that customer inherits
  the same bucket while identity is unchanged.
- Raising 10 → 25 adds customers with buckets 10–24. Lowering 25 → 10
  drops those same customers. Nobody is randomly re-rolled per request.
- Out-of-scope traffic (`scope.events` / `routes` / …) is still
  observed and recorded with `WOULD_*`; it is not actively enforced.

Reproduce: `go test ./internal/ops ./internal/api/public -run 'TestBucket|TestProgressive|TestV1AcceptanceRamp' -count=1`

## Failure modes

| Failure | Intended behaviour | Fail |
|---------|--------------------|------|
| Bruiser process restart | Postgres holds leases, fences, waiters. New process resumes the same execution (`ALREADY_HELD`) and can renew. | Closed on allocation until `/readyz` |
| Pod replacement / extra replica | Shared store. Any replica can resume, renew, or reject. Busy-cache is in-process and rebuilds. | Closed if that replica cannot reach the store |
| Rolling deployment | Overlapping replicas share the store. An in-flight execution survives. Heartbeat may rebind session. | Closed on a replica whose store ping fails |
| Backing-store outage | Acquire / authorize / renew return 503 `store unavailable` | **Closed** (unless `fail_closed=false`) |
| Upstream / origin outage | Bruiser still admits. Origin errors are origin errors. Lease remains until TTL/release. | n/a (not a Bruiser fail-open) |
| Network partition (edge ↔ Bruiser) | Edge/Proxy cannot authorize. Do not forward scarce allocation. | Closed |
| Failed renewal | Lease expires; sweeper materialises `EXPIRED` and may promote the next same-customer waiter | Closed for the expired holder |
| Client disappearance | TTL + sweep. No implicit fail-open. | Closed |
| Stale queue entry | Waiter TTL / `ExpireWaiters`; leave/release removes it | Closed |
| Duplicate acquire | Same principal+session → `ALREADY_HELD` (same fence). Other principal → `BUSY`/`QUEUED` | Closed |
| Concurrent acquire race | Store unique-active constraint: exactly one `GRANTED` | Closed |
| Deployment during active execution | Successor process reads the same row; fence unchanged until handoff/revoke | Closed |
| Kill switch `enforcement=false` | Effective 0%: observe only. Store errors ALLOW (passthrough) | Open *by operator choice* |
| `unmatched: allow` | Unlisted routes fail-open for discovery. Every hold/purchase path must be listed. Origin lockdown (`BRUISER_ORIGIN_SECRET`) is required; `config validate` FAILs the combination without it. | Open for **unlisted** routes only if origin lockdown is off — that combo is invalid |

No supported deployment may leave a scarce-inventory route reachable
without Bruiser **and** origin lockdown. `bruiser authority-check`
must be PASS.

## Authority Check classification

Each probe is `PASS`, `FAIL`, or `WARN`.

- **FAIL** — a request allocated around Bruiser. Overall `FAIL`.
- **WARN** (blocking) — a required surface was not provided (no origin
  URL), so authority cannot be proven. Overall `WARN`.
- **WARN** (non-blocking) — an optional probe was skipped (no
  `--control` URL) or an alternate path was absent (404). Overall stays
  `PASS` if every required probe passed.
- **PASS** overall — Bruiser is authoritative for the covered
  allocation operation under that deployment.

```bash
bruiser authority-check --front <edge-or-proxy> --origin <origin> --control <bruiser>
```
