# Harchester United Demonstration Environment — Scope and Execution Plan

**Status: proposed scope (revision of the original brief), ready for engineering.**

This document takes the original "Fable Scope — Harchester United Demonstration
Environment" brief, corrects the parts that conflict with the product as it exists
in this tree, fills the gaps that would have surfaced during build, and sequences
the work as milestones with exit criteria. Where this document changes the brief,
the change is called out in **§2** so the original author can accept or reject it.

The demo is the M6 "Demonstration" milestone of
[03-execution-plan.md](03-execution-plan.md), made concrete: a fictional club
website, a fictional ticketing platform, an operations console and a swarm
launcher, all running against the **real** gateway.

---

## 1. Objective (unchanged in substance)

Build a complete fictional football-club ticketing ecosystem that demonstrates
Bruiser Gateway exactly as a Premier League club would deploy it: the club owns
the website and identity, a third-party platform owns allocation and checkout,
and Bruiser is placed at the platform's admission point using the **Edge**
deployment method.

It is a reference deployment, not part of the product. Uses:

- enterprise sales demonstrations and club CTO / platform presentations
- security demonstrations (Authority Check PASS on a realistic topology)
- pilot environments
- load and swarm testing (the 1 × 10,000 EAF demo from the execution plan)

Governing principle:

> The supporter never knows Bruiser exists.

Bruiser is visible in exactly one place: the admin console.

**Language.** Internal documents may say Bruiser is "invisible". They must not
describe it as middleware, a proxy or an API gateway (ADR-020 is binding and the
console is seen by CTOs). The console, README and demo script use: *the
authoritative control layer that ensures one customer remains one customer,
regardless of how many agents they deploy.* The Edge is *where the club places
that layer*.

---

## 2. Changes to the original brief

Each row is a correction the build would otherwise have discovered late.

| # | Brief said | Problem | This scope says |
|---|-----------|---------|-----------------|
| C1 | Tree with `gateway/`, `sdk/`, `helm/`, `docker-compose.demo.yml` at root | Those directories do not exist. The product is `cmd/`, `internal/`, `configs/`, `deploy/`, `protocol/`. | `demos/` at the repo root inside the existing Go module; compose at `deploy/compose/docker-compose.demo.yml` next to the existing compose file; `README-demo.md` at root (§3). |
| C2 | "Bruiser is invisible middleware" | Violates ADR-020. | See §1 language rule. |
| C3 | Metrics "Queue depth"; swarm outcome "remaining queued"; control "Clear queues" | The product has **no server-side queue** (ADR-006). `/v1/authorize` answers `409 BUSY` with `Retry-After`; agents retry. Showing a queue would misrepresent the product. | Metric is **Held back (BUSY)**, sourced from the gateway; **Agents waiting** is the load-lab's count of agents in their retry loop. "Clear queues" becomes **Stop swarm**. |
| C4 | Enforcement 0 % … 100 % with "deterministic customer assignment" | Percentage rollout does not exist anywhere in the tree; the Edge analogue knows `enforce`, `dry-run`, `off`. Rollout is an Edge (deployment-method) setting, not lease-core policy. | Implemented in the demo SimTix Edge: `bucket = fnv1a(customer_id) mod 100`; `bucket < percent` → enforce, else dry-run (§5.2). |
| C5 | Scenario 4 repeats the **same membership** swarm at 10 / 50 / 100 % | With deterministic per-customer assignment one customer is either fully enforced or fully observed. The percentage cannot show a gradient with one customer. | Scenario 4 uses a **multi-supporter preset** (1,000 supporters × 10 agents = 10,000 agents). Scenario 2 stays single-customer (§7). This is the 1,000 × 10 demo the execution plan already calls for. |
| C6 | "Reset executions" implemented by the demo | The gateway's busy cache is in memory (`internal/lease/cache.go`); deleting rows in Postgres leaves stale BUSY answers for up to the lease TTL (60 s), so the demo looks broken right after reset. There is no admin API yet (M7). | One small **product** change: an operator endpoint that revokes all active executions for the merchant (audited `ADMIN_RESET`) and clears EAF accumulators, gated by a separate secret (§6). Audit events are never deleted. |
| C7 | Club → SimTix carries `(customer_id)` | Not designed. Passing a raw id in a query string would be noticed by the audience and does not produce the per-login principal the flagship demo relies on. | Signed SSO handoff: club issues a 60 s HS256 JWT, SimTix verifies it and mints its own `boxoffice_session` cookie (unique `jti`), which is what Bruiser's `cookie-jwt` extractor already reads. 10,000 logins → 10,000 principals → 1 grant (§5.3). |
| C8 | "Run Authority Check and receive a PASS certificate" from the console | `bruiser authority-check` is CLI-only, text output. | Product change: `--json` output. The console ships the `bruiser` binary and renders the report as a certificate (§6, §5.4). |
| C9 | Both "Dry Run" and "0 %" as controls | Identical behaviour. | One control with states **Off · Dry Run · 10 · 25 · 50 · 75 · 100 %**. Off means the Edge does not call Bruiser at all (the "before" picture, no observation). |
| C10 | Reset must clear "logs" | Bruiser's audit log is append-only and is part of the pitch. | Reset clears **demo** logs (SimTix request log, load-lab run log). Bruiser `audit_events` are retained; `make demo-nuke` is the only thing that drops them. |
| C11 | Passwords plaintext, no guard | Acceptable for the reasons given, but needs a mechanical guard, not just a note. | Seed lives in a separate database (`harchester`), never in the Bruiser database; table comment, README banner and a CI check that `demos/` imports nothing from `internal/` (§3.2). |
| C12 | Domains `*.demo.bruisergateway.com` | Existing docs use `sandbox.bruiser-gateway.com` (ADR-016). | Decision needed: one domain family. This document uses the brief's names as placeholders; the compose file works with `*.localhost` regardless. |
| C13 | Club "inspired by Dream Team's Harchester United", stadium "Dragon's Lair", opponent "Arsenal" | Harchester United / Dragon's Lair are Sky's fictional IP; Arsenal is a real club's mark. Low risk in private demos, higher once hosted publicly. | Keep the names (the brief is explicit) but ship **original** crest, colours and copy, no Dream Team characters or assets, and make the opponent a config value (`DEMO_OPPONENT`, default `Arsenal`) so it can be swapped before public hosting. Flagged for the founder, not decided here. |

Not changed: four applications, one compose file, real gateway, real SimTix, no
mocked execution path, no payment provider, supporter accounts survive reset,
10,000 seeded supporters, 500 returned seats, Saturday 15:00.

---

## 3. Repository layout and isolation

### 3.1 Layout

```
bruiser-gateway/
├── cmd/bruiser/                  product CLI + gateway (unchanged)
├── internal/                     product (one small addition, §6)
├── configs/
│   ├── arsenal.yaml              existing lab profile (unchanged)
│   └── harchester.yaml           merchant profile the demo gateway loads
├── deploy/
│   ├── compose/
│   │   ├── docker-compose.yml    existing product compose (unchanged)
│   │   └── docker-compose.demo.yml
│   ├── edge/
│   │   ├── nginx-auth-request.conf          existing
│   │   └── cloudflare-worker/               stretch, §8 Phase 6
│   └── caddy/Caddyfile.demo      hostname routing + TLS for the demo
├── demos/
│   ├── harchester-web/           club website               (Go, server-rendered)
│   ├── simtix/                   ticketing platform: origin + edge (Go)
│   ├── admin-console/            operations console         (Go + SSE)
│   ├── load-lab/                 swarm launcher, internal network only (Go)
│   ├── seed/                     supporter + fixture generator, migrations
│   └── shared/                   Go helpers only: HS256 issue/verify, SSE writer, small JS util
├── Makefile                      + demo-up / demo-down / demo-reset / demo-swarm / demo-check / demo-nuke
└── README-demo.md
```

Each application is one Go `main` package with its own `internal/` subtree
(Go's `internal` rule then isolates the demo apps from each other as well as from
the product). Templates and static assets are embedded (`embed.FS`) so each app
is a single distroless image, built from the existing multi-stage `Dockerfile`
with new targets.

**Why one Go module and not four repos or a JS stack.** The team is one or two Go
engineers; the existing site and Attack Lab are already server-rendered Go with
vanilla JS; `go build ./...`, `go vet ./...` and the existing CI keep covering the
demo for free; and the swarm launcher must be Go to reach 10,000 concurrent
agents from one process. "Cloudflare-ready" is satisfied at the deployment layer
(§9), not by choosing a frontend framework.

### 3.2 Isolation rules (enforced)

1. **No `demos/**` package imports `internal/**`.** Enforced by a `make check-demo-boundary`
   target (`go list -deps` over `./demos/...` greps for `/internal/`) that runs in
   `make ci`. The only shared code path is `demos/shared`, which depends on
   `golang-jwt` and the standard library.
2. Demo applications talk to Bruiser **only over HTTP** using the documented
   contract: `POST /v1/authorize` request/response headers as implemented in
   `internal/api/public/authorize.go`, `GET /metrics`, and the operator endpoint
   from §6. This is what a real platform integration looks like.
3. The club and SimTix issue HS256 JWTs themselves. Bruiser trusts them because
   `configs/harchester.yaml` names the cookie and the shared secret
   (`identity.extractor: cookie-jwt`), exactly as a club IdP would be configured.
4. Three Postgres **databases** on one instance: `bruiser` (product, untouched by
   demo code), `harchester` (supporters, club sessions), `simtix` (events, seats,
   holds, orders, request log). No demo code connects to `bruiser`.
5. Product changes needed by the demo (§6) land in their own PRs with tests and an
   ADR line, never inside a demo PR.

### 3.3 What happens to the existing lab code

- `internal/simtix` and `cmd/simtix` (the M3 lab origin) stay as the **CI fixture**
  for `internal/check` tests. `demos/simtix` is the full platform. When the demo
  compose runs in CI (Phase 6), the `check` tests can be pointed at it and the
  fixture can be removed; that is a follow-up, not part of this scope.
- The public **Attack Lab** on the product site (`internal/attacklab`, in-process,
  capped at 100) is unchanged. `demos/load-lab` is the heavy, internal-only tool.
  They share nothing.

---

## 4. Applications

### 4.1 Harchester United FC (`demos/harchester-web`)

Public hostname: `demo.bruisergateway.com` (placeholder, C12). Locally
`harchester.localhost`.

A believable club site. Zero Bruiser branding, no Bruiser HTTP calls, no Bruiser
cookies. It does not know Bruiser exists.

Pages: Home · News (3–5 seeded articles) · Fixtures (seeded, Arsenal highlighted
as "Members' sale now open") · Membership · Login · My Account / My Tickets ·
Match page for Harchester United v Arsenal with **Buy Tickets**.

Visual: warm ivory/beige ground, deep charcoal type, purple primary, orange
accent, editorial football aesthetic, traditional English club feel. Original
crest and photography placeholders (C13).

Identity and session:

- Login form: **Membership Number** (7 digits) + **Password**. Lookup in the
  `harchester.supporters` table; plaintext comparison (see §5.5 for why and the
  guard rails).
- On success: server-side club session (`hufc_session` cookie, HttpOnly,
  SameSite=Lax, opaque id stored in `harchester.sessions`). This cookie is
  meaningless to Bruiser and to SimTix; it is the club's own.
- **Buy Tickets** while logged out → `/login?next=/tickets/hfc-ars/buy`, then
  returned to the buy action after login.
- **Buy Tickets** while logged in → the club issues a **handoff token** (§5.3) and
  302s the supporter to SimTix.
- The login endpoint has **no rate limit** in the demo. This is deliberate and
  documented: the demo isolates what Bruiser contributes; bot mitigation is a
  different product category (ADR-020) and the flagship scenario models agents
  that hold a member's genuine credentials.
- `eligible_for_arsenal` is shown on the match page ("You are eligible for this
  sale") but **enforced by SimTix**, not the club site, so the audience sees
  eligibility is the platform's job and concurrency control is Bruiser's.

### 4.2 SimTix (`demos/simtix`)

Public hostname: `tickets.demo.bruisergateway.com`. Locally `tickets.localhost`.

A fictional ticketing platform that visibly is a different company: its own
name, logo, navigation, colour system (cool neutrals, one strong brand colour that
is not purple or orange), footer legal boilerplate, cookie banner.

Two listeners in one binary, exactly like today's `cmd/simtix`:

- **Origin** (`:8090`, compose-internal only). Postgres-backed events, seat
  blocks, holds with TTL (120 s), orders, per-account ticket limit (4 per member
  per event, checked **non-atomically** on purpose so the race is real), members-only
  eligibility, request log, `/_admin/*` stats and reset (admin secret). Origin
  lockdown: allocation routes require `X-Bruiser-Origin-Secret` (already the
  behaviour in `internal/simtix`), which is what makes Authority Check's bypass
  probe pass.
- **Edge** (`:8091`, the public listener). The platform's admission layer, the
  code a club's WAF/CDN team would own. Before forwarding allocation routes it
  calls `POST /v1/authorize` with the incoming cookie and `X-Original-*` headers,
  applies the decision, injects the origin secret, and forwards. It implements
  `off | dry-run | enforce` plus the percentage rollout (§5.2), the per-execution
  in-flight limit, and exposes `GET/PUT /_edge/config` and `GET /_edge/stats` on
  the compose network for the console.

Pages (served by origin through the edge): SSO landing (`/sso`) · Event page ·
Seat allocation (stand/block picker; "Best available" button) · Basket · Checkout
(name/email prefilled from the handoff; card form that accepts any Luhn-valid
number, no PSP) · Confirmation with order reference and a printable ticket.

Allocation API (matches `configs/harchester.yaml`, same shape as today's lab
routes so `internal/check` works unchanged):

| Route | Action | Bruiser control |
|-------|--------|-----------------|
| `GET /api/events`, `GET /api/events/{event}` | search | none |
| `POST /api/events/{event}/holds` | hold | **yes** (`resource: event:{event}`) |
| `POST /api/orders` | purchase | **yes** (`resource_from: event_id`) |
| `POST /api/holds/{hold}/release` | release | none |

The browser seat-allocation page calls `POST /api/events/hfc-ars/holds` via the
edge; that is the moment Bruiser authorises, invisibly. On `409 BUSY` the page
shows a neutral platform message ("You already have a reservation in progress on
another device") and polls `Retry-After`. It never says "Bruiser".

### 4.3 Admin Console (`demos/admin-console`)

Public hostname: `admin.demo.bruisergateway.com`. Locally `admin.localhost`.
Password protected (single shared operator password in the demo; Cloudflare
Access in front when hosted, §9).

The only place Bruiser is named. Audience: CTOs, platform engineers, security
teams, sales.

**Dashboard** (SSE, 1 s cadence). Every number names its source so an engineer in
the room can verify it:

| Tile | Source |
|------|--------|
| Seats remaining / held / sold | SimTix origin `/_admin/stats` |
| Customers online | `harchester` sessions active in last 10 min |
| Allocation attempts | `bruiser_allocation_attempts_total{resource="event:hfc-ars"}` from gateway `/metrics` |
| Executions forwarded | `bruiser_executions_forwarded_total` |
| Held back (BUSY) | attempts with `outcome="busy"` |
| Requests absorbed | attempts − forwarded, and `bruiser_busy_cache_hits_total` (§6) |
| Observed EAF (**headline**) | `bruiser_observed_eaf` |
| Downstream EAF | `bruiser_downstream_eaf` |
| Active executions | gateway operator endpoint (§6) |
| Agents launched / authenticated / waiting / finished | load-lab stream |
| Enforcement mode and % | SimTix edge `/_edge/config` |
| Would-have-blocked (Dry Run) | edge `/_edge/stats` (`X-Bruiser-Would-Block`) |

**Controls.** Reset event (seats + holds + orders) · Reset executions (§6) ·
Stop swarm · Clear demo logs · **Reset demo** (all of the above, one button; does
not touch supporter accounts, does not recreate any database) · Enforcement
selector Off / Dry Run / 10 / 25 / 50 / 75 / 100 % (applies on the next request,
no restart).

**Agent Lab.** A form over the load-lab (§4.4): Membership number · Password ·
Number of agents (presets 1 · 50 · 500 · 5,000 · 10,000) · Spawn rate (agents/s)
· Retry window (s) · Preset **Single supporter** (Scenario 2) or **1,000
supporters × 10** (Scenario 4) · Launch · Stop. Live feed of agent events and the
end-of-run summary card ("10,000 agents · 10,000 authenticated · 1 execution ·
9,999 held back · 1 seat allocated").

**Authority Check.** Button runs `bruiser authority-check --json` against the
SimTix edge and origin, renders the probe list and Overall Result, and on PASS
produces a printable **certificate** (merchant, routes covered, probe results,
timestamp, gateway version, report SHA-256). On FAIL it names the open path.
Note for the script: under Dry Run or Off the check reports **FAIL** by design,
because the Edge forwards everything; that is the point of the check.

**Audit.** Read-only view of the last 200 `audit_events` for the merchant,
fetched through the operator endpoint (never a direct DB connection), with a
"why was this BUSY?" expansion showing holder, fence and rule.

### 4.4 Load-Lab (`demos/load-lab`)

Not on any public hostname. Reachable only from the admin console over the
compose network; the console proxies and authenticates.

One Go process. Worker pool with configurable concurrency (default 500) and spawn
rate. Each agent performs the **real supporter journey over HTTP**:

1. `POST harchester/login` with membership + password → `hufc_session` cookie
   (real password check against the seed).
2. `GET harchester/tickets/hfc-ars/buy` → 302 to SimTix `/sso?token=…`.
3. `GET simtix/sso` → SimTix verifies the handoff, sets `boxoffice_session`.
4. `POST simtix/api/events/hfc-ars/holds {"seats":1}` through the Edge.
5. Classify from the edge's decision headers: `ALLOW` → hold created;
   `BUSY` → enter retry loop honouring `Retry-After` until the retry window
   ends (**Agents waiting**); `DENIED`/`THROTTLED` → record.
6. The agent that holds a seat completes `POST /api/orders` so the flagship
   result is a **sold** seat, not a hold that expires.

Nothing is bypassed and nothing is forged: the launcher only holds credentials
and drives the same endpoints a browser would. It has no way to mint
`boxoffice_session` cookies (that secret is not configured in the load-lab
container).

Limits: `LOADLAB_MAX_AGENTS` (default 10,000), one run at a time, hard stop on
**Stop**. Emits an SSE stream of agent events and a final JSON summary that the
demo assertion test (§8 Phase 6) checks.

Multi-supporter preset: agent *i* logs in as supporter `1000001 + (i mod N)` with
the seed's password; N defaults to 1,000.

Budget for one 10,000-agent run on a laptop: ~40,000 HTTP requests through Caddy;
10,000 club logins (10,000 indexed selects); 10,000 `/v1/authorize` calls, each
inserting a `sessions` row and hitting the busy cache with a 1-in-50 store
refresh (~200 store acquires). Expect 10–25 s at 500 concurrency. Measure in
Phase 4 before deciding whether the per-attempt session insert needs attention
(§10 R4).

---

## 5. Design details the brief left open

### 5.1 Merchant profile (`configs/harchester.yaml`)

```yaml
merchant_id: harchester
name: Harchester United FC
profile: demo-premier-league-club
identity:
  extractor: cookie-jwt
  cookie: boxoffice_session
  subject_claim: sub
  hmac_secret_env: BRUISER_DEV_HMAC_SECRET
routes:   # as in §4.2
policy:
  rule_name: purchase-per-event
  max_active: 1
  lease_ttl: 60s
  max_lifetime: 15m
```

The gateway container in the demo compose runs `serve` with
`BRUISER_MERCHANT_ID=harchester` and `BRUISER_PROFILE=/configs/harchester.yaml`.
It has **no public hostname**; only the SimTix edge and the console reach it.

### 5.2 Enforcement modes and rollout (SimTix Edge)

| Setting | Calls `/v1/authorize` | On BUSY/DENIED | Origin secret | Headers to caller |
|---------|----------------------|----------------|---------------|-------------------|
| Off | no | n/a | injected (so the origin still works) | none |
| Dry Run | yes | **forward anyway**, count would-block | injected | `X-Bruiser-Would-Block: 1` |
| Enforce N % | yes | customers with `fnv1a32(customer_id) mod 100 < N` are **blocked** (status and body from Bruiser passed through); others behave as Dry Run | from Bruiser response, else injected | decision headers |

100 % is the only setting under which Authority Check can PASS. Deterministic
bucketing means a given supporter has a consistent experience during a rollout,
which is the property a CTO cares about, and is why Scenario 4 needs many
supporters (C5).

### 5.3 Identity handoff (Club → SimTix → Bruiser)

```
Harchester (club IdP)                SimTix (platform)                 Bruiser
─────────────────────                ─────────────────                 ───────
POST /login ─► hufc_session
GET /tickets/hfc-ars/buy
  ─► 302 tickets.../sso?token=T
     T = HS256{ iss:harchester, aud:simtix,
                sub:<membership_no>, name, email,
                eligible:true, exp:+60s, jti }
                                     GET /sso verifies T (HARCHESTER_SSO_SECRET)
                                     mints boxoffice_session =
                                       HS256{ iss:simtix, aud:bruiser,
                                              sub:<membership_no>, exp:+1h, jti }
                                     302 /events/hfc-ars
                                     POST /api/events/hfc-ars/holds (via Edge)
                                       Edge ─► POST /v1/authorize (Cookie) ───► extractor reads
                                                                                boxoffice_session,
                                                                                customer = sub,
                                                                                principal = unaware:<jti>
                                       ◄── ALLOW + X-Bruiser-Execution / BUSY 409
```

- Bruiser's customer identity is the **7-digit membership number** (matches
  `docs/07`, the Authority Check default `1001234`, and the brief's examples).
  `customer_id` in the seed is an internal opaque key the club uses; it never
  reaches Bruiser.
- Each login produces a fresh `boxoffice_session` with a unique `jti`, so each
  authenticated agent is a distinct principal for the same customer: that is
  what yields 1 GRANTED and 9,999 BUSY in `authorize.go`. The same cookie
  re-used is `ALREADY_HELD`, also correct.
- Two secrets, two audiences: `HARCHESTER_SSO_SECRET` (club ↔ SimTix) and
  `BRUISER_DEV_HMAC_SECRET` (SimTix ↔ Bruiser). The load-lab container has
  neither.

### 5.4 Authority Check from the console

The console image copies `/bruiser` from the gateway build stage and runs
`bruiser authority-check --edge http://simtix:8091 --origin http://simtix:8090
--membership 1001234 --event hfc-ars --json`. `internal/check` already has
`Membership` and `EventID` in its config; the CLI flags and JSON output are the
product change (§6). Certificate rendering is demo code.

### 5.5 Seed data (`demos/seed`)

Deterministic generator (fixed PRNG seed, checked-in Go code, **no** 10,000-row
file in git) applied idempotently on start-up of `harchester-web` and `simtix`
(`insert … on conflict do nothing`), so `make demo-up` on a fresh volume seeds
and a restart does not duplicate.

Supporters (10,000): `customer_id` (`cus_…`), `membership_number` (`1000001`–`1010000`),
`first_name`, `last_name`, `email` (`@example.com` only), `password` (default
`password` for all; the brief's table stands), `membership_tier` (Junior /
Bronze / Silver / Gold / Season Ticket, weighted), `loyalty_points`,
`season_ticket`, `eligible_for_arsenal` (~85 %). Plus named accounts for the
script: `1001234` Alice Okafor (Gold, eligible), `1000002` (Junior, **not**
eligible, for the "SimTix says no" beat), and the presenter's own account.

Plaintext passwords: retained for the reasons in the brief (fictional, closed,
deterministic credentials for the launcher). Guard rails: column comment
`'DEMO ONLY - plaintext by design - never copy this schema'`, a banner at the
top of `demos/seed/README.md` and `README-demo.md`, the boundary check (§3.2)
so nothing under `internal/` can ever depend on this schema, and the
`harchester` database is separate from `bruiser`.

Fixtures: **Harchester United v Arsenal**, Premier League, Saturday 15:00 at the
Dragon's Lair, `event_id = hfc-ars`, 500 returned seats across 4 blocks with 2
price bands, members-only sale open now. Three more fixtures (one away, one cup,
one sold-out) for realism; only `hfc-ars` is on sale.

### 5.6 Reset semantics

| Button | Effect | Never touches |
|--------|--------|---------------|
| Reset event | SimTix: delete holds and orders for `hfc-ars`, seats back to 500, request log cleared | supporters |
| Reset executions | gateway operator endpoint: revoke all ACTIVE executions for `harchester` (audited `ADMIN_RESET`), forget busy cache, zero EAF accumulators | `audit_events` |
| Stop swarm | load-lab: cancel run, drain workers | anything else |
| Clear demo logs | SimTix request log, load-lab run history, console event feed | `audit_events` |
| **Reset demo** | all of the above, in that order | any database schema; supporter rows |

`make demo-nuke` drops the three databases. It is not in the console.

---

## 6. Product changes required (separate PRs, product tree)

Kept to the minimum that makes the demo honest. Each is a legitimate early slice
of M7 (Admin & observability) rather than demo scaffolding.

| # | Change | Where | Size |
|---|--------|-------|------|
| P1 | Operator endpoint group, enabled only when `BRUISER_OPERATOR_SECRET` is set: `POST /v1/operator/reset-executions` (revoke all ACTIVE for the merchant with reason `ADMIN_RESET`; the busy cache already `forget`s on `Revoke`), `GET /v1/operator/executions?state=ACTIVE`, `GET /v1/operator/audit?limit=200`, `POST /v1/operator/eaf/reset`. Header `X-Bruiser-Operator-Secret`. Store gets `ListActive(merchant)` and `ListAudit(merchant, limit)`. | `internal/api/public`, `internal/store/postgres` | S–M |
| P2 | `bruiser authority-check --json`, `--membership`, `--event` flags. | `cmd/bruiser/check.go` | S |
| P3 | Export busy-cache hits/misses as `bruiser_busy_cache_hits_total` / `_misses_total` (the counters already exist on `BusyCache`). | `internal/lease`, `internal/api/public` | S |
| P4 | `Dockerfile` targets for the four demo images; `harchester.yaml` copied into the gateway image alongside `arsenal.yaml`. | `Dockerfile`, `configs/` | S |
| P5 | ADR-023: demo environment is a reference Edge deployment; operator endpoints are the first slice of M7; seed plaintext rule. | `docs/04-decisions.md` | S |

Not a product change and explicitly out of scope here: percentage rollout in the
gateway (it is an Edge concern), any queue, any change to lease semantics.

---

## 7. Demonstration scenarios (script-ready)

Every scenario starts from **Reset demo** and enforcement **100 %** unless stated.

**Scenario 1 — Normal purchase (2 min).** Login as `1001234`/`password` on the
club site → Fixtures → Arsenal → Buy Tickets → land on SimTix already signed in →
Best available → Basket → Checkout → Confirmation. Console shows attempts 1,
forwarded 1, EAF 1.0×. Nothing on either site mentions Bruiser. Then log in as
`1000002` and show SimTix refusing eligibility: the platform still owns its rules.

**Scenario 2 — 10,000-agent swarm, one supporter (flagship, 3 min).** Agent Lab:
membership `1001234`, 10,000 agents, spawn 1,000/s, single-supporter preset,
Launch. Expected on the summary card: 10,000 authenticated · 10,000 allocation
attempts · **1 execution forwarded** · 9,999 held back · observed EAF ≈ 10,000×
· downstream EAF 1× · **1 seat sold**, 499 remaining. Open the audit view: one
`EXECUTION_GRANTED`, the rest `EXECUTION_BUSY` with the same holder.

**Scenario 3 — Dry Run (the "before", 2 min).** Reset demo → enforcement **Dry
Run** → same swarm. Expected: 10,000 attempts forwarded; SimTix shows hundreds of
seats held by one member until the per-account limit (non-atomic) is visibly
breached and blocks go to zero; console shows "would have blocked: 9,999". No
supporter was blocked. Run Authority Check → **FAIL**, open path named. Reset.

**Scenario 4 — Progressive rollout (3 min).** Preset **1,000 supporters × 10**.
Run at **10 %**: ~100 supporters see enforcement (1 execution each, 9 BUSY), 900
behave as Dry Run; console shows the two populations. Reset executions and seats,
run at **50 %**, then **100 %**: every supporter holds at most one seat, forwarded
executions ≈ number of supporters until seats run out at 500. Same supporter,
same bucket, every run.

**Closing — Authority Check (1 min).** At 100 %, press Run Authority Check →
PASS on all probes including "Direct allocation bypass blocked" → print the
certificate.

---

## 8. Execution plan

Milestones with exit criteria, sized S/M/L as in
[03-execution-plan.md](03-execution-plan.md). Dependencies are stated; there are
no dates.

```
D0 Skeleton & boundary
 ├─ D1 Seed + Harchester web ──┐
 ├─ D2 SimTix origin + edge ───┼─ D4 Load-lab ─┐
 └─ D3 Product slices P1–P5 ───┘               ├─ D5 Admin console
                                               └─ D6 Assertions, runbook, hosting
```

D1, D2 and D3 are independent after D0 and can run in parallel. D4 needs D1+D2.
D5 needs D3+D4. D6 needs everything.

### D0 — Skeleton and boundary (S)

- `demos/` tree with four `main` packages that serve a health page; `demos/shared`
  with HS256 issue/verify and SSE writer, unit-tested.
- `deploy/compose/docker-compose.demo.yml`: `postgres` (three databases via init
  script), `gateway` (profile `harchester`, no published port), `harchester-web`,
  `simtix`, `admin-console`, `load-lab` (no published port), `caddy` routing
  `harchester.localhost`, `tickets.localhost`, `admin.localhost` and the
  production hostnames from one Caddyfile.
- `configs/harchester.yaml`; `Dockerfile` targets (P4).
- `make demo-up | demo-down | demo-reset | demo-swarm | demo-check | demo-nuke |
  check-demo-boundary`; `check-demo-boundary` added to `make ci`.
- `README-demo.md` skeleton with the plaintext-password banner.

Exit: `make demo-up` on a clean checkout brings all services healthy; `make ci`
fails if a demo package imports `internal/`.

### D1 — Seed and Harchester United (M)

- Seed generator and idempotent apply; `harchester` schema and sessions table.
- All pages in §4.1, login/logout, `next` redirect, session cookie, My Tickets
  (reads orders from SimTix via a server-to-server call with the SSO secret).
- Buy Tickets → handoff token → 302.
- Visual system per brief; original crest; responsive.

Exit: login as `1000001`/`password` works; unauthenticated Buy Tickets round-trips
through login; Buy Tickets produces a valid handoff JWT (unit test verifies claims
and 60 s expiry); no string "bruiser" appears in any served asset (test greps the
rendered pages).

### D2 — SimTix (M)

- `simtix` schema, seed of fixtures and 500 seats; origin routes in §4.2 with
  lockdown, holds TTL, non-atomic per-account limit, eligibility.
- Edge with `off | dry-run | enforce N%`, config/stats endpoints, decision
  headers; in-flight limit.
- SSO landing minting `boxoffice_session`; all pages; neutral BUSY handling.

Exit: Scenario 1 completes in a browser through Caddy against the real gateway;
`bruiser authority-check` from the host (`make demo-check`) reports PASS at
100 %, FAIL under Dry Run and with lockdown disabled; two browser logins of the
same supporter → second hold gets the platform's "reservation in progress"
message and the edge logged `BUSY`.

### D3 — Product slices (S–M, product PRs)

P1–P5 from §6, each with tests: operator endpoints refuse without the secret and
are absent when unset; reset revokes and the busy cache forgets (test asserts a
GRANT immediately after reset); `--json` output round-trips into
`check.Report`.

Exit: `make ci` green; ADR-023 merged.

### D4 — Load-lab (M)

- Journey runner per §4.4, worker pool, spawn rate, retry loop, classification,
  order completion, SSE feed, summary JSON, single-run lock, Stop.
- Presets single-supporter and 1,000 × 10.
- Perf pass on a laptop: record run time, Postgres load, busy-cache hit ratio
  (P3) at 1,000 / 5,000 / 10,000.

Exit: 10,000 single-supporter agents produce exactly 1 forwarded execution, 9,999
BUSY and 1 order, in under 30 s at the default concurrency; 1,000 × 10 at 100 %
produces ≤ 1 hold per supporter and 500 sold. If run time is unacceptable, open
the R4 discussion before D5.

### D5 — Admin console (M)

- Auth, dashboard tiles with sources, SSE aggregator (gateway `/metrics` parsing,
  edge stats, SimTix stats, load-lab stream), controls, enforcement selector,
  Agent Lab form and live feed, Authority Check runner and certificate, audit view.
- "Reset demo" ordering per §5.6.

Exit: success criteria 4–7 achieved from the browser only; after Reset demo the
very next swarm produces a GRANT within one lease TTL of nothing (proves C6 is
solved).

### D6 — Assertions, runbook, hosting (M)

- `demos/e2e`: compose-driven test that runs a 200-agent single-supporter swarm
  and a 50 × 4 multi-supporter swarm and asserts the summary numbers; runs in CI
  on PRs touching `demos/`, `configs/harchester.yaml` or `internal/api/public`.
  Nightly job runs the full 10,000.
- `docs/demo-runbook.md`: the §7 script with timings, reset points, what to say
  when something is slow, and the ADR-020 vocabulary.
- Hosting per §9; presenter checklist; recorded 90-second video (execution plan
  M6).
- Stretch: Cloudflare Worker reference edge in `deploy/edge/cloudflare-worker/`
  implementing §5.2, used in the hosted deployment instead of the Go edge. This
  closes an M3 gap and makes "Cloudflare-ready" literal.

Exit: someone outside the team runs the demo from `README-demo.md` without help;
hosted URLs live behind Cloudflare with the console behind Access; CI asserts the
headline numbers.

---

## 9. Hosting and "Cloudflare-ready"

- One VM (4 vCPU / 8 GB is ample) running the demo compose; Cloudflare DNS
  proxied records for the three public hostnames; Cloudflare origin certificate
  or Caddy ACME on the origin; **Cloudflare Access** policy on the admin hostname
  in addition to the console password; the gateway and load-lab have no DNS
  records and no published ports.
- All three applications are server-rendered with static assets under
  `/assets/*` and long cache headers, so Cloudflare caches them at the edge and
  the Worker edge (D6 stretch) is a drop-in: the same `/v1/authorize` contract
  from a Worker in front of `tickets.*` instead of the Go edge container.
- Nightly `Reset demo` via cron on the VM so the hosted environment is always in
  Scenario 1 state.

---

## 10. Risks specific to the demo

| ID | Risk | Effect | Mitigation |
|----|------|--------|------------|
| R1 | Reset leaves stale BUSY | demo looks broken for 60 s | P1 revokes through the store so the busy cache forgets; D5 exit criterion tests it |
| R2 | Single-customer rollout demo shows nothing | Scenario 4 falls flat | Multi-supporter preset (C5); the script says why deterministic bucketing is the right property |
| R3 | Audience hears "middleware / proxy" | positioning damage (ADR-020) | Vocabulary in runbook; console copy reviewed against ADR-020 before D6 exit |
| R4 | `/v1/authorize` inserts a `sessions` row per attempt; 10,000 inserts on a laptop Postgres may stretch the run | flagship feels slow | Measure at D4. If needed, a product PR moves the insert after a cache-hit BUSY decision (a M2-spirit optimisation); do not change demo code to hide it |
| R5 | Fictional IP and a real club's mark in public collateral | legal/brand exposure once hosted | C13: original assets; `DEMO_OPPONENT` configurable; founder decision before public hosting |
| R6 | Plaintext seed copied into a real integration | security incident | Separate database, schema comment, README banners, boundary check; onboarding checklist says "never reuse demo schema" |
| R7 | Laptop network/Caddy becomes the bottleneck rather than the gateway | wrong story told | Console shows gateway-side counters, not launcher-side; run time is not the headline, EAF is |
| R8 | Console reads Prometheus text | fragile parsing | Use `prometheus/common/expfmt` (already a transitive dependency) in the console |

---

## 11. Success criteria (restated with verification)

A club CTO can, from a browser:

1. Visit Harchester United and find no trace of Bruiser (automated grep in D1).
2. Log into a seeded member account (`1001234`).
3. Buy Arsenal tickets on SimTix; the hold is authorised by the real gateway
   (audit shows `EXECUTION_GRANTED` for `event:hfc-ars`).
4. Open the admin console and read live gateway metrics with their sources.
5. Launch a 10,000-agent swarm using the same credentials; every agent performs a
   real login and a real SimTix hold attempt.
6. Watch observed EAF climb toward 10,000× while downstream EAF stays at 1× and
   one seat is sold.
7. Run Authority Check at 100 % and print a PASS certificate; see it FAIL under
   Dry Run.

And, for the team: `make demo-up` works from a clean checkout; CI asserts the
headline numbers; no demo package imports the product; the product tree changed
only by P1–P5.
