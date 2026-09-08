# Bruiser — Post-core capabilities

Binding roadmap. These strengthen deployment and enterprise readiness
**after** the core customer-level execution control model is proven. They
must not delay outbound or change the proposition.

Bruiser remains a **customer-level execution control layer**. It does not
replace waiting rooms, authentication, ticketing, payments, or the customer
database.

```
CDN / WAF
    ↓
Bruiser          ← dry-run or enforce; same placement
    ↓
Waiting room (optional, merchant-owned)
    ↓
Existing ticketing / commerce
    ↓
Inventory
```

Keep the club’s existing stack. Add Bruiser.

---

## Near-term (this tree)

| Capability | Command / surface |
|------------|-------------------|
| One-command install | Compose, Helm, Edge, Proxy — already the placement methods |
| Diagnostics | `bruiser doctor` — PASS / WARN / FAIL |
| Config validation | `bruiser config validate` (also `profile validate`) |
| Transparent dry-run | `BRUISER_MODE=dry-run` or admin controls; same path, never blocks |
| Authority Check | `bruiser authority-check` — go-live gate (already shipped) |
| Health / readiness | `/healthz`, `/readyz` (store, signing key, mode) |
| Emergency controls | `GET/PUT /v1/admin/controls`, drain, auditable |
| Active-execution protection | Per-execution in-flight, rate, burst, request deadline (already shipped) |
| EAF | Admin + Prometheus (already shipped); dry-run report adds *would-have* EAF |

## Production-readiness (docs first)

Upgrade/rollback and backup/restore: [ops.md](ops.md). No Bruiser-managed
cloud storage. Merchant Postgres is the system of record.

## Commercial / proof

EAF, absorbed attempts, origin-load reduction — admin, Grafana, dry-run
24h report. Historical analytics can deepen without new infrastructure.

---

## Dry-run (transparent)

Bruiser sits in the **real** production path and evaluates the **same**
policies. It does **not** enforce:

- no customer blocked
- no request queued
- no execution revoked
- existing flows continue

Every affected request records a hypothetical decision: `WOULD_ALLOW`,
`WOULD_QUEUE`, `WOULD_REJECT`, `WOULD_EXPIRE`. Admin: **What Bruiser would
have stopped** (`GET /v1/admin/dry-run`).

Install → Dry Run → Observe → Tune → Enforce. Same architecture.

The queue in dry-run is still **intra-customer**: Alice’s extra agents
*would* wait for Alice. Bob is never lined up behind Alice.

## Emergency controls

Operators can flip enforcement OFF (fail-open pass-through), disable the
intra-customer queue, override max waiters / lease TTL, drain waiters, and
revoke. Allocation routes still **fail closed on store unavailability**
unless the operator sets `fail_closed: false`. Enforcement OFF is
pass-through on a healthy node; an unhealthy node must not accidentally
create uncontrolled concurrent allocation. Every control change is audited.

## Scope boundary

Supporting capabilities only. Core purpose unchanged: one customer remains
one customer, regardless of how many agents they deploy.
