# Bruiser Gateway — operator runbook

Self-hosted. The merchant owns Postgres, secrets, the IdP, the origin, and
networking. This runbook describes **shipped** behaviour only.

Companion: [production-config.md](production-config.md),
[authority-certificate.md](authority-certificate.md),
[staging-idp.md](staging-idp.md), [ops.md](../ops.md).

---

## Installation

### Topology

Three supported placements. Authority is the requirement; the method is a choice.

| Placement | Path |
|-----------|------|
| **Edge** | Customer → merchant edge (NGINX / Cloudflare / Go Edge) → `POST /v1/authorize` with `X-Bruiser-Edge-Secret` → origin. Edge injects `BRUISER_ORIGIN_SECRET` from **local** config. |
| **Proxy** | Bruiser proxy (`BRUISER_PROXY_ADDR`) admits then stamps the origin secret. Inbound client `X-Bruiser-*` headers are stripped except edge/admin/operator/event/nonce. |
| **Embedded** | Origin SDK verifies execution JWT + fence. Optional `/v1/introspect` for revoke. Origin lockdown still recommended so direct holds fail. |

Typical production: 1–N Bruiser replicas, one Postgres, distinct public (`:8080`) and admin (`:8082`) listeners, origin on a private network. Helm chart: `deploy/helm/bruiser-gateway`. Lab Compose: `deploy/compose/docker-compose.yml` (not a production topology).

### Prerequisites

- Go 1.25 runtime image / released `bruiser` binary (or the Helm image).
- PostgreSQL 16 (merchant-managed).
- Merchant OIDC/JWKS IdP reachable from Bruiser (HTTPS).
- Distinct admin, operator, edge, and origin secrets (see production-config).
- Merchant profile YAML with hold/purchase routes, `unmatched: deny`, closed `resources:` list.
- Origin configured to require Bruiser execution JWT + fence + origin secret.

### Database

```bash
export BRUISER_DATABASE_URL='postgres://USER:PASS@HOST:5432/bruiser?sslmode=require'
bruiser migrate
```

Helm runs a migrate Job when `migrate.enabled` (default true). Migrations are
additive SQL under `internal/store/postgres/migrations/`. Do not reverse-migrate
unless a release note says the down migration is safe.

Lab DSN `postgres://bruiser:bruiser@…` **fails** production boot.

### Secrets

Supply from the environment, SealedSecret, or vault. Never commit. Generate
with `openssl rand -base64 32`. Pairwise distinct: admin ≠ operator ≠ edge ≠ origin.

Edge and origin must be updated in the **same change window** when those two
rotate.

### Identity / JWKS

Set `BRUISER_JWKS_URL`, `BRUISER_ISSUER`, `BRUISER_AUDIENCE` and the matching
profile `identity:` block (`extractor: oidc`). Bruiser caches JWKS for five
minutes; a fetch failure is unauthorized (fail closed), not a skip.

### Origin lockdown

Origin verifies: valid Bruiser execution JWT + fence + merchant + canonical
resource + expiry. Origin secret is path trust only. Garbage token + valid
origin secret → **403**. `/v1/authorize` never returns the origin secret.

Lab `AllowOriginSecretOnly` is refused in production.

### Edge integration

Configure the edge with `BRUISER_EDGE_SECRET` (to call authorize) and
`ORIGIN_SECRET` / `BRUISER_ORIGIN_SECRET` (to reach origin). See
[08-make-authoritative.md](../08-make-authoritative.md).

### Configuration validation

```bash
bruiser config validate /configs/your-profile.yaml
```

No `FAIL`. WARNs (empty catalogue, `sslmode=disable`, lab profile path) must
be owned before go-live.

---

## Pre-production checks

| Check | Command / signal | Pass |
|-------|------------------|------|
| Config validation | `bruiser config validate` | No FAIL |
| Readiness | `GET /readyz` | `store=ok`, `signing_key=ok`; 503 until then (allocation fail-closed) |
| Liveness | `GET /healthz` | 200 |
| Doctor | `bruiser doctor --profile … --http … --front … --origin …` | Not FAIL |
| Authority Check | see [authority-certificate.md](authority-certificate.md) | Certificate **issued** |
| Origin authority | forged execution + valid origin secret | 403 |
| Resource catalogue | unknown ID | rejected when `resources:` is set |
| Logging / audit | `audit_events` growing; `AUTHORITY_CHECK` row | Yes |
| Fail-closed | stop Postgres, `POST /v1/authorize` | not 2xx (503 `store unavailable` unless `fail_closed=false`) |

Startup banner must show `env=production`, `fail_open=false`, `identity=jwks`
(or the Dry Run acknowledgement banner if you have not finished observe-only).

---

## Dry Run

Dry Run is **0% enforcement, 100% observation**. Every eligible request is
still evaluated and recorded. Nothing is blocked by Bruiser.

Production refuses Dry Run unless acknowledged:

```bash
export BRUISER_ALLOW_UNSAFE_MODES=1
export BRUISER_MODE=dry-run
# or: BRUISER_ENFORCE_PERCENT=0
# or after boot: PUT /v1/admin/controls  {"mode":"dry-run"} or {"enforce_percent":0}
```

`enforcement=false` is the kill switch (also effective 0%). Startup logs
`BRUISER_ALLOW_UNSAFE_MODES=1 acknowledged: …`.

Admin report: `GET /v1/admin/dry-run` — **What Bruiser would have stopped**.

| Record | Meaning |
|--------|---------|
| `WOULD_ALLOW` | Policy would have admitted this execution |
| `WOULD_QUEUE` | Intra-customer queue would have applied (202) |
| `WOULD_REJECT` | Would have been BUSY / DENIED / budget exhausted |
| `WOULD_EXPIRE` | Hypothetical occupancy that would have expired |

Estimated absorbed / origin load: compare `bruiser_allocation_attempts_total`
with `bruiser_executions_forwarded_total` on **each** replica (do **not** sum
`bruiser_observed_eaf`). Cluster ratio:

```promql
sum(increase(bruiser_allocation_attempts_total[5m]))
/
clamp_min(sum(increase(bruiser_executions_forwarded_total[5m])), 1)
```

Tune policy (`max_active`, `budget.max_ops`, `waiting`, rate limits, catalogue)
from `WOULD_*` and audit — not by weakening origin lockdown.

Leave Dry Run until staging IdP evidence and the Authority certificate are
filed.

---

## Progressive enforcement

`enforce_percent` controls **active enforcement only**. Observation,
evaluation, and recording stay at 100% of traffic Bruiser sees.

Presets: **10% → 25% → 50% → 75% → 100%**. Any 0–100 value is valid.
`PUT /v1/admin/controls` on `BRUISER_ADMIN_ADDR` (operator or admin
credential). Audited as `CONTROL_CHANGED`. No redeploy.

Customer cohort assignment is **deterministic**:

```
bucket = FNV-64a( ramp_salt || 0x00 || customer_id ) % 100
enforce  = (bucket < enforce_percent)
```

- Grain is **customer**, not request, agent, or device.
- Raising 10 → 25 adds buckets 10–24. Lowering 25 → 10 drops the same customers.
- Optional `ramp_salt` reshuffles; changing it is auditable.
- `scope` (events, routes, pools, cohorts, environments, policies) limits who
  is eligible for active enforcement. Out-of-scope traffic is still observed
  (`WOULD_*`).

Suggested dwell: stay at each step until dry-run residuals, origin errors, and
audit look boring. There is no shipped auto-ramp.

Reproduce: `go test ./internal/ops ./internal/api/public -run 'TestBucket|TestProgressive|TestV1AcceptanceRamp' -count=1`

---

## Emergency rollback

Return enforcement to **0%** without turning Bruiser off:

```bash
# Admin listener, operator or admin secret:
curl -sS -X PUT "$ADMIN/v1/admin/controls" \
  -H "X-Bruiser-Operator-Secret: $BRUISER_OPERATOR_SECRET" \
  -H "Content-Type: application/json" \
  -d '{"enforce_percent":0}'
```

Equivalent: `{"mode":"dry-run"}` or `{"enforcement":false}`.

| Goal | How |
|------|-----|
| 0% enforce | `enforce_percent=0` / dry-run / `enforcement=false` |
| Preserve observation | Do **not** remove Bruiser from the path; do not set unmatched fail-open |
| Verify traffic | Origin still serves; `/readyz`; `WOULD_*` and audit still written; edge/proxy returns |
| Investigate | Audit `CONTROL_CHANGED`, acquire outcomes, origin 403s, JWKS/store errors |
| Restore enforcement | Raise percent along the same deterministic buckets (same `ramp_salt`) |

Redeploying a previous **image tag** is the binary rollback. Additive tables
can remain; an older binary ignores unknown columns. Image rollback does not
replace a 0% control-plane rollback when the fault is policy/origin, not the
build.

There is no shipped “panic button” other than these admin controls and taking
the process down (which fail-closes allocation while the store is unreachable).

---

## Operational failure

| Failure | Shipped behaviour |
|---------|-------------------|
| PostgreSQL outage | Acquire / authorize / renew → **503** `store unavailable`. Fail-closed unless the operator set `fail_closed=false` (requires unsafe acknowledgement at boot if production). |
| Gateway process / pod failure | Leases, fences, waiters live in Postgres. Successor resumes (`ALREADY_HELD`). Unhealthy replica fail-closes its own allocation. |
| Replica loss | Remaining replicas share the store. In-process busy cache rebuilds. Do not sum `bruiser_observed_eaf` across replicas. |
| Replica recovery | Start process, wait `/readyz` `store=ok` `signing_key=ok`, then it admits. |
| Origin failure | Bruiser may still admit. Origin errors are origin errors. Lease remains until TTL/release. Not a Bruiser fail-open. |
| JWKS failure | JWT extract → unauthorized. Cache is 5 minutes; a bad fetch does not skip verification. Sessions already issued remain until expiry/revoke. |
| Expired / revoked sessions | Authenticated routes check the session store. `POST /v1/sessions/logout` or `/revoke`. Acquire with a revoked session → **401**. |
| Expired executions | Sweeper materialises `EXPIRED`, may promote the next same-customer waiter. Stale fence rejected at origin. |
| Bruiser signing-key (`signing_keys` Ed25519) | `EnsureSigningKey` on boot. Downstream fetches `/.well-known/bruiser/jwks.json`. New processes reuse the current row. |

---

## Security operations

| Task | How |
|------|-----|
| Origin / edge / admin / operator secret rotation | Generate new distinct values. Update edge **and** origin together for origin/edge. Restart Bruiser. `config validate` must still pass. |
| JWKS rotation | IdP publishes a new `kid`. Bruiser refetches within 5 minutes (or invalidate by restart). Old `kid` works until the IdP drops it. |
| Session revocation | `POST /v1/sessions/logout` (bearer session) or admin tools. |
| Execution revocation | Admin `POST /v1/admin/executions/{id}/revoke`. `POST /v1/admin/controls/revoke-all` is emergency (`EMERGENCY_REVOKE`, admin credential). Origin + introspect reject revoked material. |
| Audit review | Table `audit_events` (default 13 months). Events include `AUTHORITY_CHECK`, `CONTROL_CHANGED`, `POLICY_CHANGED` (exactly one per policy write), `EMERGENCY_DRAIN`, `EMERGENCY_REVOKE`, `AUDIT_PURGED`. |
| Authority Check revalidation | After origin, secret, identity, or catalogue change: run the certificate command again. A previous certificate does not cover a new topology. |
| Incident response | 1) 0% enforcement if Bruiser is harming checkout. 2) Keep observation. 3) Revoke sessions/executions if identity or execution material leaked. 4) Rotate the implicated secret. 5) File audit + certificate artifact. 6) Restore ramp with the same salt when the cause is closed. Do not disable origin lockdown to “keep sales up”. |

`POST /v1/admin/controls/drain` emergency-drains the intra-customer queue
(`EMERGENCY_DRAIN`).

---

## Key rotation (Bruiser signing)

Execution/session JWTs are Ed25519 keys in Postgres `signing_keys`, not files
in git. Rotation of **release-signing** (cosign) keys is a human/legal item —
see [release-and-licensing.md](release-and-licensing.md).

---

## What this runbook does not invent

- No hosted Bruiser IdP or hosted control plane
- No automatic ramp or automatic rollback
- No SIEM connector
- No public Open Source portal
- Discovery `control: none` is not fail-open
