# Bruiser Gateway — Technical Design (V1)

Status: proposed. Decisions and their rationale are logged in
[04-decisions.md](04-decisions.md). Anything marked **(V1.5)** or **(V2)** is
designed for but not built in V1.

---

## 1. Design rules

1. **Grant is the only operation that needs coordination.** BUSY, DENIED and
   status reads may be answered from local or cached knowledge. A stale cache can
   only cause a spurious BUSY (harmless, retried), never a duplicate grant.
2. **Time comes from the store.** Lease expiry is evaluated with the store's clock
   (`now()` inside the transaction). Node clocks only drive timers and hints.
3. **Fail closed on allocation, open on discovery.** If the store is unreachable,
   acquire/renew/handoff return `503` with `Retry-After`; discovery pass-through
   continues; existing leases remain valid until their `exp`.
4. **Every grant is fenced.** Downstream systems can reject stale holders even if
   their token has not expired.
5. **One binary, one database.** No Redis, no message broker, no sidecars in V1.
   Coordination technology is an implementation detail behind the `Store`
   interface and is never part of the product's definition.
6. **Tenant in every row, every query, every token.**
7. **Authority is the requirement; the deployment method is an implementation
   detail.** Every path to the protected allocation operation passes an admission
   check (§9). Bruiser is sold as the control layer, never as middleware, a proxy
   or an API gateway. The Authority Check proves it; integration is not complete
   until it reports PASS.
8. **Enforcement never depends on the client knowing Bruiser exists.** Edge and
   Proxy methods acquire executions transparently from the merchant's own
   session; Bruiser-aware clients get richer behaviour, not different rules.
9. **Protocol-first.** OpenAPI, JSON Schema, token spec and conformance suite are
   the definition; the Go binary is one implementation of them.

## 2. Stack

| Concern | Choice | Notes |
|---------|--------|-------|
| Language | Go 1.23+ | static binary, excellent concurrency tooling (`-race`), small containers, enterprise-credible |
| HTTP | `net/http` + `chi` | no framework lock-in |
| Store | PostgreSQL 15+ via `pgx` | source of truth for leases, policy, audit; migrations via `goose` |
| Tokens | PASETO v4.public (Ed25519); JWT/EdDSA available for ecosystems that need it | JWKS at `/.well-known/bruiser/jwks.json` |
| Config/policy | YAML, validated with a JSON Schema; hot-reload | stored in Postgres, editable via admin API |
| Observability | `slog` JSON logs, Prometheus metrics, OpenTelemetry traces | `/metrics`, `/healthz`, `/readyz` |
| Tests | `go test -race`, `pgregory.net/rapid` (property), `testcontainers-go` (real Postgres), custom torture harness, `k6` load | |
| Admin UI | server-rendered templates + htmx + SSE | no JS build pipeline in V1 |
| Dev deploy | Docker Compose (3× gateway, Caddy LB, Postgres, SimTix, swarm, dashboard) | |
| Prod deploy | Helm chart; external Postgres | |
| SDKs | Go, Node, Python verification middleware; MCP tool definition **(V1.5)** | |

## 3. Domain model

```
Merchant        merchant_id, name, idp_jwks_url | idp_shared_secret (dev), signing_key_ids[]
Customer        (not stored as an entity; asserted per session)  customer_id, anchors{}
Principal       type ∈ {agent, browser}, id, display, vendor?, capabilities[]
Session         session_id, merchant_id, customer_id, anchors{}, principal, exp
Policy          merchant_id, version, yaml, compiled, active_from
Domain          merchant_id, domain_key  (PK), fence bigint, updated_at
Execution       execution_id, merchant_id, domain_key, customer_id, principal,
                resource, action, rule_name, fence, state, granted_at,
                expires_at, max_lifetime_at, renew_count, ended_at, end_reason
Waiter          merchant_id, domain_key, waiter_id (= future execution_id), principal, expires_at
AuditEvent      seq, merchant_id, at (store clock), type, customer_id, principal,
                domain_key, execution_id, fence, rule_name, reason, request_id, attrs{}
```

### 3.1 Domain key derivation

```
domain_key = merchant_id
           + "/" + rule.name
           + "/" + for dim in rule.scope: dim + "=" + value(dim, request, session)
```

Dimensions: `customer`, `resource`, `resource_pool`, `principal_type`,
`anchor:<name>`. Missing anchor → `DENIED reason=missing_anchor` (fail closed).
Dimension set is extensible without schema change.

### 3.2 Execution state machine

```
            ┌──────────── renew (holder, ACTIVE, before max_lifetime) ───────────┐
            ▼                                                                    │
 acquire → ACTIVE ──release(holder)──────────────▶ RELEASED                      │
            │  └──expires_at < now() ─────────────▶ EXPIRED                      │
            │  └──revoke(merchant | customer w/ precedence)─▶ REVOKED            │
            └────handoff(cooperative | preempt)──▶ HANDED_OFF ─┐                 │
                                                               ▼                 │
                                                new ACTIVE execution, fence+1 ───┘
```

Terminal states are immutable. `EXPIRED` is a *derived* state: any ACTIVE row with
`expires_at < now()` is treated as expired by every reader; a sweeper later
materialises the row state and emits `EXECUTION_EXPIRED`. Correctness never
depends on the sweeper running.

## 4. Storage and concurrency

### 4.1 Schema (abridged)

```sql
create table domains (
  merchant_id text not null,
  domain_key  text not null,
  fence       bigint not null default 0,
  primary key (merchant_id, domain_key)
);

create table executions (
  execution_id     text primary key,
  merchant_id      text not null,
  domain_key       text not null,
  customer_id      text not null,
  principal_type   text not null,
  principal_id     text not null,
  session_id       text not null,
  resource         text not null,
  action           text not null,
  rule_name        text not null,
  fence            bigint not null,
  state            text not null,            -- ACTIVE | RELEASED | EXPIRED | REVOKED | HANDED_OFF
  granted_at       timestamptz not null,
  expires_at       timestamptz not null,
  max_lifetime_at  timestamptz not null,
  renew_count      int not null default 0,
  ended_at         timestamptz,
  end_reason       text,
  successor_id     text
);
create index on executions (merchant_id, domain_key) where state = 'ACTIVE';
create index on executions (merchant_id, customer_id, granted_at desc);

create table audit_events (
  seq          bigserial primary key,
  merchant_id  text not null,
  at           timestamptz not null default now(),
  type         text not null,
  customer_id  text, principal_type text, principal_id text,
  domain_key   text, execution_id text, fence bigint,
  rule_name    text, reason text, request_id text,
  attrs        jsonb not null default '{}'
);
```

### 4.2 Acquire (single transaction)

```
BEGIN;
  INSERT INTO domains (merchant_id, domain_key) VALUES ($m, $d) ON CONFLICT DO NOTHING;
  SELECT fence FROM domains WHERE merchant_id=$m AND domain_key=$d FOR UPDATE;      -- serialises the domain
  SELECT * FROM executions
    WHERE merchant_id=$m AND domain_key=$d AND state='ACTIVE' AND expires_at > now();
  -- if one of those is held by this principal → heartbeat/resume:
  --     expires_at = least(now()+ttl, max_lifetime_at), rebind session_id,
  --     renew_count + 1, audit EXECUTION_RENEWED reason=resume, return ALREADY_HELD
  -- if count >= rule.max_active → BUSY (409) with holder info; write audit; COMMIT
  UPDATE domains SET fence = fence + 1 ... RETURNING fence;
  INSERT INTO executions (... fence, state='ACTIVE',
      granted_at=now(), expires_at=now()+ttl, max_lifetime_at=now()+max_lifetime);
  INSERT INTO audit_events (...);
COMMIT;
```

Only one transaction per domain proceeds at a time; all others wait on the row lock
(milliseconds) and then observe the new ACTIVE row. This holds across any number of
gateway processes because the store is the arbiter. `max_active > 1` is the same
path with a count check.

### 4.3 Renew / release / revoke / handoff

All take the domain row lock first, re-check `state='ACTIVE' AND expires_at >
now()`, then mutate.

**Renew is the heartbeat.** Clients (browser or Bruiser-aware agent) send
`POST /v1/executions/{id}/renew` (alias `POST /v1/executions/{id}/heartbeat`)
every 20–30 seconds (default 25 s). Each call sets
`expires_at = least(now()+ttl, max_lifetime_at)` (`ttl` default 60 s) and returns
a fresh token with the same fence. The holder is the original session *or* a new
session bound to the same principal, so a refresh or reconnect before expiry
resumes rather than locking the customer out. If heartbeats stop, the lease
expires after the TTL and is released automatically. A reconnect *after* expiry
is a new acquire under merchant policy (typically a new GRANT).

On Edge and Proxy, a Bruiser-unaware client does not call renew: the same cookie
presenting again is ALREADY_HELD and takes the resume path above.

Handoff ends the old execution and inserts the successor with `fence+1` in the
same transaction; `successor_id` links them.

### 4.4 Local busy cache

After any store round-trip that reveals an ACTIVE execution in a domain, the node
caches `(domain_key → holder, expires_at)`. Subsequent acquires from a *different*
principal for that domain are answered BUSY locally until `expires_at`, with a
hedge: every Nth cached BUSY (default 1 in 50) goes to the store to refresh. The
holder's own requests always go to the store. Watch subscribers are notified via
`LISTEN/NOTIFY` on `bruiser_domain_<hash>` so releases propagate across nodes
within milliseconds.

### 4.5 Throughput expectations

Per-domain serialisation ≈ 1–3k grant-path ops/s on a modest Postgres; the busy
cache means 10,000 agents against one domain produce ~200 store hits in the first
burst, not 10,000. Cross-domain traffic parallelises. Discovery never touches the
store. Reads of execution status can use a replica **(V2)**.

### 4.6 Store failure behaviour

| Store state | acquire / handoff | renew | release | GET status | discovery |
|-------------|-------------------|-------|---------|------------|-----------|
| healthy | normal | normal | normal | normal | pass-through |
| unreachable | 503 fail closed | 503; token stays valid until `exp` | 503 (lease expires naturally) | from cache with `stale: true` | pass-through |
| slow | bounded by per-request deadline (default 2 s) then 503 | same | same | cache | pass-through |

`/readyz` fails when the store is unreachable so load balancers stop routing
grant traffic to that node; `/healthz` stays green so the process is not killed.

## 5. Identity, sessions and tokens

### 5.1 Customer assertion → session

```
POST /v1/sessions
Authorization: Bearer <customer assertion JWT signed by merchant IdP>
{
  "principal": { "type": "agent", "id": "shopping-agent-123", "vendor": "acme" }
}
→ 201 { "session_id": "...", "session_token": "<paseto>", "expires_at": "...",
        "customer_id": "cust_alice", "anchors": ["membership_no", "household_id"] }
```

Bruiser validates the assertion against the merchant's configured JWKS
(`iss`, `aud`, `exp`, `sub` → `customer_id`, `bruiser_anchors` claim). The
merchant is asserting both *who the customer is* and *that this principal is
authorised to act for them* (the merchant ran the consent/OAuth flow). Dev mode
accepts an HMAC shared secret. Session tokens are short-lived (default 1 h).

`customer_id` is the club's existing supporter identity. Preferred: membership /
supporter number. Acceptable initially: the club's account identifier. Bruiser
never mints its own customer identifiers and never stores credentials.

### 5.1a Merchant session → implicit session (transparent enforcement)

For clients that do not speak the protocol (§9, Edge and Proxy methods), Bruiser derives
the customer from the merchant's **existing session credential** on the
allocation request itself. A merchant configures one *customer extractor*:

```yaml
identity:
  extractor: jwt                         # jwt | cookie-jwt | introspect | header-signed
  jwt:  { jwks_url: https://id.club.com/.well-known/jwks.json, aud: club-web, subject_claim: supporter_id,
          anchors_claim: bruiser_anchors, cookie: club_session }
  # introspect: { url: https://id.club.com/introspect, auth: <secret ref> }   # for opaque sessions
  # header-signed: { header: X-Club-Customer, hmac_secret: <secret ref> }      # edge already authenticated the user
```

The extractor yields `customer_id` + anchors; the principal is
`session:<hash of merchant session id>` (type `agent` by default, `browser` if the
extractor is told the merchant marks human-present sessions — e.g. a claim set by
the web login flow). Bruiser then creates or reuses an *implicit* Bruiser session
with the same TTL as the merchant session. Everything downstream is identical to
the explicit path.

Principal typing for unaware clients is deliberately coarse: Bruiser does not
attempt to detect agents. Two unaware agents sharing one merchant login are one
principal and coalesce onto one execution; per-execution in-flight limits (§9.4)
bound what that execution can do concurrently.

### 5.2 Execution token

PASETO v4.public, signed with the merchant's current Ed25519 key (`kid` in footer):

```json
{
  "iss": "bruiser/arsenal",
  "sub": "cust_alice",
  "exe": "exe_839281",
  "dom": "arsenal/purchase-per-event/customer=cust_alice/resource=event:ars-che",
  "res": "event:ars-che-2026-10-04",
  "act": "purchase",
  "prn": "agent:shopping-agent-123",
  "fnc": 17,
  "iat": "...", "exp": "<lease expires_at>", "jti": "..."
}
```

`exp` always equals the lease's current `expires_at`, so a token can never outlive
its lease. Renewal issues a new token (new `exp`, new `jti`, same `fnc`).

### 5.3 Merchant-side verification (Embedded)

```
verify(token):
  signature valid for kid in JWKS         else reject
  exp > now - skew_tolerance (5 s)        else reject
  res matches the inventory being acted on else reject
  fnc >= last_fence_seen[sub, res]        else reject   (store last_fence_seen per customer+resource)
  last_fence_seen[sub, res] = fnc
  (purchase step, recommended) POST /v1/introspect {token} → { active: true|false }
```

Offline verification is enough for `hold`; introspection on `purchase` closes the
revoke-propagation window to zero. SDK middleware implements exactly this.

### 5.4 Admin authentication

API keys (argon2id-hashed, prefix-identified) or mTLS; roles `admin`, `operator`,
`viewer`, `auditor`. SSO for the admin UI is a Core feature.

## 6. Public API (Protocol v0)

All endpoints require a session token unless stated. JSON bodies; `Idempotency-Key`
header honoured on mutating requests; `request_id` echoed in responses and audit.

| Method & path | Purpose | Success | Notable failures |
|---------------|---------|---------|------------------|
| `POST /v1/sessions` | exchange customer assertion + principal for a session | 201 | 401 bad assertion, 403 principal type not allowed |
| `POST /v1/executions/acquire` `{resource, action}` | request a lease in the matching domain | 201 GRANTED, 200 already held by this principal | 409 BUSY, 403 DENIED, 503 UNAVAILABLE |
| `POST /v1/executions/{id}/renew` | heartbeat: extend lease | 200 new token, `heartbeat_after_ms` | 410 with `reason` ∈ EXPIRED/REVOKED/HANDED_OFF/RELEASED, 403 not holder |
| `POST /v1/executions/{id}/heartbeat` | alias of renew | 200 | same as renew |
| `POST /v1/executions/{id}/release` | give up lease | 200 | 410 |
| `POST /v1/executions/{id}/handoff` `{to: {type, id?}, mode}` | transfer lease | 201 new execution (token to new holder) | 403 precedence, 409 target has own lease |
| `POST /v1/executions/{id}/revoke` `{reason}` | end lease (customer via precedence, or admin) | 200 | 403 |
| `GET /v1/executions/{id}` | status (customer's own; no token material) | 200 | 404 |
| `GET /v1/executions/{id}/watch` | SSE / long-poll until state changes | stream | |
| `GET /v1/domains/current?resource&action` | what is active in my domain right now | 200 | |
| `POST /v1/introspect` `{token}` | merchant-side liveness check (merchant credential) | 200 `{active, execution}` | |
| `POST /v1/authorize` | Edge admission check (merchant credential): given the original request's method, path, resource, action and credentials, decide allow/deny and acquire transparently if needed | 200 allow + headers to inject (`X-Bruiser-Execution`, `X-Bruiser-Fence`) | 401, 403 DENIED, 409 BUSY (edge maps to 429/503 with `Retry-After`), 503 UNAVAILABLE |
| `GET /.well-known/bruiser/jwks.json` | public keys | 200 | |
| `GET /.well-known/bruiser/protocol` | protocol version & capabilities | 200 | |

BUSY body:

```json
{ "status": "BUSY", "domain": "...", "active_execution_id": "exe_839281",
  "holder": {"type":"agent","id":"shopping-agent-123"}, "expires_at": "...",
  "retry_after_ms": 4000, "watch": "/v1/executions/exe_839281/watch",
  "can_preempt": false }
```

**Recovery.** Reconnect before expiry resumes the same execution (ALREADY_HELD
extends TTL and rebinds the session). After expiry, a new execution may be
acquired according to merchant policy.

`can_preempt: true` is how a browser learns it may "take control".

**Proxy endpoints (Proxy method):** `POST /v1/proxy/{adapter}/{op}` for `op ∈ search |
hold | release | purchase | cancel`, plus a transparent HTTP reverse-proxy listener
that applies *route rules* to the merchant's own URL space. `search` is
uncontrolled; the rest require a matching ACTIVE execution for the caller —
acquired explicitly beforehand, or transparently on first use — and are forwarded
with fence attached.

### 6.1 Admin API

`/admin/v1/executions` (list/filter), `/admin/v1/executions/{id}/revoke`,
`/admin/v1/customers/{id}` (current state + history), `/admin/v1/policy`
(get/put/validate/history), `/admin/v1/audit` (query by customer, execution,
domain, time, type), `/admin/v1/keys` (rotate), `/admin/v1/stats` (live counters,
SSE).

## 7. Policy engine

```yaml
version: 1
merchant: arsenal
defaults:
  lease_ttl: 60s
  max_lifetime: 15m
  session_ttl: 1h
  principal_types: [agent, browser]
domains:
  - name: purchase-per-event
    match: { action: [purchase, hold] }          # first matching rule wins, top-down
    scope: [customer, resource]
    max_active: 1
    lease_ttl: 60s
    precedence: [browser, agent]                 # earlier outranks later; may preempt
    waiting: { mode: none }                      # none | bounded (V1.5): {max_waiters: 1, claim_window: 10s}
  - name: household-cap
    match: { action: [purchase], resource_prefix: "event:" }
    scope: [anchor:household_id, resource]
    max_active: 1
    on_missing_anchor: deny                      # deny | fallthrough
  - name: discovery
    match: { action: [search] }
    control: none
fallback: deny                                    # deny | allow-uncontrolled
```

Compiled once per version; evaluation is pure and allocation-free on the hot path.
Policy changes are versioned, validated (`bruiser policy validate`), audited
(`POLICY_UPDATED`), and applied to *new* acquires only — existing executions keep
their granted terms. Multiple rules may apply to one request **(V2)**; V1 is
first-match to keep reasoning simple.

## 8. Merchant adapter interface

```go
type Adapter interface {
    Search(ctx, SearchRequest) (SearchResult, error)               // uncontrolled
    Hold(ctx, Execution, HoldRequest) (Hold, error)                // fenced
    Release(ctx, Execution, HoldID) error
    Purchase(ctx, Execution, PurchaseRequest) (Receipt, error)     // fenced
    Cancel(ctx, Execution, OrderID) error
    Capabilities() Capabilities
}
```

`Execution` carries `customer_id`, `principal`, `fence`, `expires_at`. Adapters
must be stateless with respect to Bruiser and idempotent on retry. V1 ships
`adapter/simtix` (HTTP) and `adapter/memory` (tests).

## 9. Deployment methods and the Authority Check

Authority is the requirement. Embedded, Edge and Proxy are how a merchant places
the control layer in front of existing infrastructure — they are not different
products. The lease core, policy engine, tokens, audit and Authority Check are
identical in every method; what differs is where the admission check happens and
how bypass is closed.

### 9.1 Embedded — in-application middleware

The merchant's checkout verifies the execution token (§5.3) via an SDK. Bruiser is
out of the data path. Bypass closure: every allocation route in the application is
behind the middleware; there is no unauthenticated allocation API. Fits clubs that
control their checkout code.

### 9.2 Edge — API gateway / WAF / Worker

The merchant's existing edge calls `POST /v1/authorize` before forwarding
allocation requests. Bruiser ships reference configurations for:

- NGINX / OpenResty `auth_request`
- Envoy `ext_authz` (HTTP)
- Kong and Tyk plugins (thin, calling `/v1/authorize`)
- Cloudflare Worker / Fastly Compute snippet
- AWS API Gateway Lambda authorizer

`/v1/authorize` receives method, path, headers (credentials), and the merchant's
*route rules* map them to `(resource, action)`:

```yaml
routes:
  - match: { method: POST, path: "/api/events/{event}/holds" }
    resource: "event:{event}"
    action: hold
  - match: { method: POST, path: "/api/orders" }
    resource_from: { body_json: "$.event_id", prefix: "event:" }
    action: purchase
  - match: { path: "/api/events/**", method: GET }
    action: search                       # uncontrolled
```

On allow, Bruiser returns headers for the edge to inject (`X-Bruiser-Execution`,
`X-Bruiser-Fence`, `X-Bruiser-Customer`). The origin must reject allocation
requests lacking a valid edge signature or arriving from anywhere other than the
edge (mTLS, private network, or a per-deployment shared secret the edge adds). If
the client already holds an explicit execution token it is verified; otherwise the
customer is extracted (§5.1a) and an execution acquired transparently.

### 9.3 Proxy — reverse proxy

Bruiser terminates allocation traffic for configured routes and forwards to the
origin (or via an adapter), attaching fence and identity. The same route rules
apply. Bypass closure: origin allocation endpoints are reachable only from the
Bruiser proxy (network policy / allowlist / mTLS). Fits merchants who can change
neither the application nor an edge, and the simulator/demo. This method places
Bruiser in the data path, so its HA and fail-closed behaviour matter most here;
the Helm chart's defaults assume it.

### 9.4 Per-execution limits

An execution is a serialisation point; downstream should treat one execution as one
shopper (one basket, one hold set). To prevent a customer collapsing many clients
onto one execution and hammering the origin, Edge and Proxy enforce, per execution,
a **max in-flight requests** (default 2) and a **rate limit** (default 5 rps),
node-locally in V1. Embedded SDKs expose the same limits as an optional local
guard. Exceeding returns 429 with `Retry-After` and an `EXECUTION_THROTTLED` audit
event when sustained.

### 9.5 Bruiser Authority Check (product feature)

`bruiser authority-check --target <merchant config>` is a CLI and an admin-UI
action. Against a staging or production environment it:

1. enumerates the allocation routes from route rules / SDK registration;
2. attempts each route **without** any Bruiser execution, with an expired token,
   with a tampered token, with a stale fence, and from outside the enforcement path
   where network access permits;
3. probes for common bypasses: alternative hostnames, HTTP vs HTTPS origin, legacy
   API versions, mobile API paths, GraphQL mutations, direct origin IP;
4. reports PASS/FAIL per probe with evidence, and writes an `AUTHORITY_CHECK`
   audit event.

Example output:

```
Bruiser Authority Check

  PASS — Browser allocation route protected
  PASS — Mobile API protected
  PASS — Agent API protected
  PASS — Expired token rejected
  PASS — Tampered token rejected
  PASS — Direct allocation bypass blocked

  Overall Result: PASS
```

A deployment that cannot achieve PASS is not production-ready. The check is part
of the integration guide, part of the demo (lockdown off → FAIL with the named
open path; lockdown on → PASS), and the go-live gate in the design-partner
onboarding checklist. It cannot prove the absence of paths it does not know about,
so the integration questionnaire ([06-integration-discovery.md](06-integration-discovery.md))
asks the merchant to enumerate them, and the report lists what was covered.

### 9.6 Choosing a deployment method

| Merchant situation | Method |
|--------------------|--------|
| Owns checkout code, can add a dependency | **Embedded** (preferred when available: Bruiser out of the data path) |
| Checkout is a platform, but club controls the edge/CDN/API gateway/WAF in front of it | **Edge** |
| Platform exposes a supported pre-allocation hook or webhook | **Edge** via hook, or adapter-specific variant |
| No control over app or edge, but can route DNS/network for allocation endpoints | **Proxy** |
| No viable way to make Bruiser authoritative | Not a V1 customer; record requirements and defer |

## 10. SimTix — simulated ticketing system

Separate binary. Events with N seats, price bands, time-limited holds (default
120 s), purchase, cancel; per-account limit (e.g. 4 seats) enforced *non-atomically*
by default to mirror common real-world behaviour (toggle to atomic for honest
comparison); a club-style login issuing a session cookie/JWT (so transparent
enforcement can be demonstrated); optional Embedded token verification via the Go SDK;
an `--enforcement` flag selecting `embedded`, `edge` (SimTix behind a bundled NGINX
`auth_request` config) or `proxy` (behind the Bruiser proxy), and `--origin-lockdown`
to reject traffic not carrying the edge secret; request log with timing;
Prometheus metrics; a reset endpoint. Deliberately simple and deliberately
representative of the failure modes Bruiser addresses (hold churn, duplicate
allocation under concurrency, downstream load, bypass).

## 11. Swarm — agent load generator

Configurable customers × agents × events; agent behaviour profiles (`greedy`,
`polite`, `retry-storm`, `handoff-aware`, `unaware` — speaks only the merchant's
API with a merchant session, never the Bruiser protocol); targets Bruiser or SimTix
directly (`--bypass`); emits per-request outcomes to the dashboard and a results
JSON with the headline numbers (downstream requests, grants, busy, duplicate
allocations, bypass successes, seats per customer histogram, p50/p99).

## 12. Torture harness and invariant checker

`cmd/torture` runs N in-process or containerised gateways against one Postgres and
drives randomised operations from M customers × K principals while injecting
faults: SIGKILL a gateway, `pg_sleep`/pause the store, add latency, skew a node's
clock via the injected `Clock`, drop LISTEN/NOTIFY, restart mid-transaction.

Every response and every audit event is written to a history. The checker then
verifies:

- **I1 exclusivity** — for every domain and every store timestamp, ACTIVE
  executions ≤ `max_active`.
- **I2 fence monotonicity** — fences strictly increase per domain; a successor
  always has a higher fence than its predecessor.
- **I3 token ≤ lease** — no issued token has `exp` after its execution's
  `expires_at` at issue time.
- **I4 no zombie success** — no `renew` succeeded after the execution reached a
  terminal state.
- **I5 audit completeness** — every state transition has exactly one audit event;
  every GRANT response has a matching `EXECUTION_GRANTED`.
- **I6 fail closed** — no GRANT was issued during a window where the store was
  known unavailable.
- **I7 idempotency** — same principal + same domain never holds two ACTIVE
  executions.

A violation fails CI and dumps the minimal history for reproduction.

## 13. Observability

Metrics (Prometheus): `bruiser_acquire_total{outcome,rule}`,
`bruiser_active_executions{merchant}`, `bruiser_busy_cache_hit_ratio`,
`bruiser_lease_expired_total`, `bruiser_handoff_total{mode}`,
`bruiser_store_op_seconds{op}`, `bruiser_store_unavailable`,
`bruiser_downstream_requests_total{adapter,op}`,
`bruiser_allocation_attempts_total{resource,outcome}`,
`bruiser_executions_forwarded_total{resource}`,
`bruiser_eaf{resource,window}` (gauge).

**Execution Amplification Factor (primary KPI).**

```
EAF = incoming allocation attempts ÷ authorised executions forwarded
```

Incoming = every scarce-inventory request Bruiser saw (GRANTED, BUSY, DENIED,
transparent acquire, throttled). Authorised forwarded = requests Bruiser allowed
through to the merchant (one per ACTIVE execution's first forwarded allocation,
plus any subsequent in-flight requests that execution is permitted). Reported as
two gauges the admin UI leads with:

- `observed_eaf` — incoming ÷ authorised. Alice's 10,000 agents produce ~10,000×.
- `downstream_eaf` — authorised forwarded ÷ distinct customers who attempted.
  Under `max_active: 1` this sits at 1×.

Windows: live (last 60 s), current on-sale, last 24 h, last 30 d. Per merchant,
per resource, per customer. Informational only; never used for billing or
throttling.

Traces: one span per request with `merchant`, `domain`, `execution`, `fence`
attributes. Logs: JSON, `request_id` correlated to audit events.

**Usage figures (fair-use transparency, not metering):** the admin UI and
`/admin/v1/stats` expose peak concurrent executions (rolling 30 days), executions
per month, distinct resources protected, peak observed EAF and peak downstream
EAF. These are informational; the gateway never throttles or licenses on them.

**Audit retention:** `audit.retention` per merchant, default `13 months`. A daily
job deletes (or, if `audit.archive` is configured, exports then deletes) events past
retention and records an `AUDIT_PURGED` event with counts. Retention changes are
themselves audited.

## 14. Security controls

- Ed25519 keys generated by `bruiser keys rotate`; at least two keys live during
  rotation; JWKS cached by merchants with `max-age`.
- Session and execution tokens are bearer tokens: TLS required; tokens never
  logged; `jti` recorded for replay detection within `exp`.
- Rate limits (token bucket, per node): per principal 20 rps, per customer 100 rps,
  per merchant configurable; exceeding → 429 with `Retry-After` and an audit event
  when sustained.
- Request body limits, strict JSON, deadline per request, connection limits.
- Admin API on a separate listener/port; RBAC checked per route.
- Postgres: least-privilege role for the gateway; migrations via a separate role;
  optional RLS by `merchant_id`.
- Secrets from environment or mounted files only; no secrets in policy YAML.
- Container: distroless, non-root, read-only filesystem; SBOM and image signing in
  the release pipeline.
- Threat model (STRIDE) maintained in `docs/security/threat-model.md`; external
  review before first production deployment.

## 15. Deployment

**Compose (dev/demo):** `caddy` LB → `gateway ×3` → `postgres`; plus `simtix`
(with its NGINX edge for Edge), `swarm`, `dashboard`. One command, seeded merchant,
seeded policy, seeded route rules, dev IdP secret.

**Helm (prod):** `Deployment` with HPA, `PodDisruptionBudget`, readiness on store,
external Postgres via secret, `ServiceMonitor`, `NetworkPolicy`, optional
`Ingress`; separate admin `Service`. Migrations as a `Job` hook. Zero-downtime
upgrade path documented and tested. Self-hosting by the merchant is the primary
data-residency control; the chart never phones home.

**Hosted sandbox (`sandbox.bruiser-gateway.com`):** the same Helm chart on a small
managed Kubernetes cluster, with a `sandbox` build flag enabling self-service
throwaway merchants (signup with email, auto-expiring after 14 days, hard rate and
size limits, nightly reset of demo merchants), the live demo dashboard, and hosted
API docs. This is the one Bruiser deployment that is genuinely multi-tenant, so it
doubles as the tenant-isolation proving ground. It is a sales/discovery asset, not
a production offering, and says so.

## 16. Repository and licence layout

Licensing (founder decision, pending legal review): protocol, schemas,
specifications and SDKs under **Apache-2.0**; gateway core under **BSL 1.1**.
The repository is laid out so the two can be published separately at M8 — the
Apache-2.0 assets to a public `bruiser-protocol` repository, the gateway to
`bruiser-gateway` — without moving code:

```
LICENSE                 BSL 1.1 (gateway core; parameters set with counsel)
protocol/               Apache-2.0 (own LICENSE): OpenAPI, JSON Schemas, token spec, audit vocabulary,
                        conformance suite, protocol docs
sdk/go/ sdk/node/ sdk/python/   Apache-2.0 (own LICENSE): verification middleware + clients
deploy/edge/            Apache-2.0: NGINX / Envoy / Kong / Cloudflare / API Gateway reference configs
cmd/bruiser/            gateway: serve | migrate | policy validate | keys rotate | admin-key create | authority-check
cmd/simtix/             simulated ticketing system
cmd/swarm/              agent swarm generator
cmd/torture/            distributed correctness harness + checker
internal/lease/         state machine, Store interface, busy cache, clock
internal/store/postgres/
internal/policy/        schema, compiler, evaluator
internal/auth/          assertions, sessions, tokens, JWKS, admin keys, RBAC
internal/api/public/    v1 handlers        internal/api/admin/
internal/audit/         emitter, exporters
internal/adapter/       interface, simtix, memory
internal/enforce/       route rules, /v1/authorize, transparent acquire, per-execution limits
internal/proxy/         Proxy method reverse proxy
internal/authority/     authority-check probes and report
web/admin/              templates, htmx, SSE
deploy/compose/ deploy/helm/
docs/  docs/security/
```

BSL 1.1 parameters proposed for legal review: *Additional Use Grant* — production
use permitted for any purpose except offering the software, or a service whose
value derives primarily from it, to third parties as a hosted or managed Bruiser
service; *Change Date* — four years from each release; *Change Licence* —
Apache-2.0. Commercial licences (Core/Enterprise) sit alongside BSL for customers
who need different terms.

## 17. Open technical items (tracked, not blocking)

- Postgres `LISTEN/NOTIFY` vs. periodic polling for watch fan-out under heavy
  connection counts — start with NOTIFY, measure.
- Whether the busy-cache hedge ratio should adapt to observed store latency.
- Introspection endpoint caching semantics (must never return `active: true` for
  a revoked execution — so no caching of positive answers beyond a few hundred ms).
- Anchor hashing/normalisation rules (belongs in the protocol spec).
- Whether per-execution in-flight limits (§9.4) need to be store-coordinated for
  Proxy deployments with many proxy replicas, or whether node-local is sufficient in
  practice.
- Customer extractor for opaque merchant sessions: introspection latency on the
  allocation path; cache TTL vs. logout propagation.
