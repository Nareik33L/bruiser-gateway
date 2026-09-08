# Architecture Decision Log

Short records of decisions that shape V1. Each can be revisited; the "revisit
when" line says what evidence would justify it.

---

## ADR-001 — PostgreSQL is the coordination store; no Redis in V1

**Decision.** Leases, fences, policy and audit live in PostgreSQL. Grants
serialise on a per-domain row lock inside one transaction. No second datastore.

**Why.** The invariant needs a linearizable arbiter. A single Postgres row lock is
one, with durability and an audit trail in the same transaction. Redis-based
locking (Redlock) has well-documented safety caveats around clock assumptions and
process pauses, and adds a second system to run, secure and back up. Merchants
already operate Postgres. Denials do not touch the store, so throughput is not the
concern it first appears to be.

**Consequences.** Grant throughput per domain is bounded by row-lock latency
(fine: one domain = one customer's agents). The `Store` interface allows another
backend later. Coordination technology is an implementation detail: it never
appears in positioning, and the product is not "a lock" (founder principle).

**Revisit when.** Torture/load testing shows grant-path latency or connection
pressure that the busy cache and pooling cannot absorb, or an enterprise requires
active-active multi-region grants.

## ADR-002 — Authority is the requirement; Embedded / Edge / Proxy are deployment methods

**Decision.** Integration is defined as making Bruiser authoritative over the
protected allocation operation and closing every bypass path. Three **deployment
methods** are supported as equals, chosen from the merchant's existing
infrastructure: **Embedded** (in-application middleware verifying the execution
token); **Edge** (API gateway / WAF / Worker calling `/v1/authorize`); **Proxy**
(Bruiser reverse proxy). They are how a merchant places the control layer, not
different products. Bruiser is never sold as middleware, a proxy or an API
gateway. Every deployment runs the Bruiser Authority Check before go-live; a club
that cannot achieve PASS is not production-ready. Where no method can make Bruiser
authoritative, the merchant is not a V1 customer.

**Why.** Founder decisions Q2 and Q13, restated as product philosophy: authority
is the requirement; the deployment method is an implementation detail. Target
clubs have mixed setups; Bruiser must be platform-agnostic and must not depend on
a ticketing-platform partnership. Embedded is still recommended where available
because it keeps Bruiser out of the purchase data path and degrades most
gracefully, but it is a preference within the requirement, not the requirement.

**Consequences.** Route rules, `/v1/authorize`, transparent acquire and reference
edge configurations become V1 scope (M3). SDKs in the merchant's language remain
necessary for Embedded. The Authority Check is a named product feature, a go-live
gate and a sales/demo artefact.

**Revisit when.** A supported platform integration point emerges that warrants a
dedicated adapter-specific method.

## ADR-002a — Transparent enforcement: two client classes, one set of rules

**Decision.** Bruiser never relies on agents voluntarily integrating. Two client
classes exist:

- **Bruiser-aware** — explicit acquire, renew, handoff, watch; better UX.
- **Bruiser-unaware** — merchant's existing authenticated session; Bruiser
  extracts customer identity and acquires the execution automatically.

In Edge and Proxy the unaware path is the default: extract from the merchant
session, acquire on first allocation request. The same concurrency policy applies
to both classes. SDKs and MCP tooling are adoption aids only.

**Why.** Founder decision Q8/Q13 and the Transparent Enforcement principle: the
merchant remains protected regardless of the client. A control layer that only
controls cooperative agents controls nothing.

**Consequences.** A configurable customer extractor (JWT / cookie-JWT /
introspection / edge-signed header); coarse principal typing for unaware clients;
per-execution in-flight and rate limits to bound clients that collapse onto one
execution.

## ADR-003 — Fencing tokens on every grant

**Decision.** Each domain carries a monotonically increasing fence; every grant
(including handoff successors) increments it and embeds it in the token. Merchants
reject tokens whose fence is lower than the highest they have seen for that
customer + resource.

**Why.** Token expiry alone cannot protect against a preempted or paused holder
acting with a still-unexpired token. Fences make revoke and handoff *safe*
downstream, not just recorded.

**Revisit when.** Never for the core; the format of how fences are conveyed may
evolve with the protocol.

## ADR-004 — Lease is permission to attempt allocation, not an inventory hold

**Decision.** Bruiser never holds, allocates or knows seat-level inventory.

**Why.** Principle 5: do not build a ticketing system. Keeps the data model tiny and
the merchant in control of inventory semantics.

## ADR-005 — Fail closed on allocation, open on discovery

**Decision.** Store unavailable → acquire/renew/handoff return 503; discovery
proxying continues; existing tokens stay valid until `exp`; readiness fails.

**Why.** The one unacceptable outcome is a duplicate grant. Blocking new grants for
the duration of an outage is the safe trade. Discovery is abundant and harmless.

## ADR-006 — No server-side queue in V1; BUSY + watch instead

**Decision.** Excess acquires get 409 BUSY with `retry_after` and a watch channel.
A bounded waiting set is V1.5. Inter-customer fair queueing is out of scope.

**Why.** Unbounded per-customer queues are a DoS vector and a distributed problem in
themselves. A watch channel gives agents an efficient wait without the gateway
owning ordering. Inter-customer fairness is a waiting-room product, not a
concurrency gateway.

**Revisit when.** Design partners need "next agent in line" semantics (then
V1.5 bounded waiting), or ask Bruiser to replace their waiting room (then a
product decision, not a technical one).

## ADR-007 — Store clock is the authority for lease time

**Decision.** `expires_at` is computed and compared using Postgres `now()` inside
the transaction. Node clocks drive timers and hints only. Merchant verification
tolerates 5 s skew on `exp`.

**Why.** Multi-node correctness must not depend on NTP on every gateway host.

## ADR-008 — Go, single static binary

**Decision.** Go 1.23+, `net/http` + `chi`, `pgx`.

**Why.** Concurrency tooling (`-race`, goroutine-friendly torture harness), small
distroless images, one artefact to deploy, wide enterprise acceptance, strong
ecosystem for Postgres, OTel and Prometheus.

**Alternatives considered.** Rust (excellent, heavier iteration cost for V1);
TypeScript (fast to write, less convincing for a correctness-critical
infrastructure product); JVM (fine, heavier operational footprint).

**Revisit when.** Team composition dictates otherwise before M1 starts.

## ADR-009 — PASETO v4.public for tokens, JWT/EdDSA available

**Decision.** Default execution and session tokens are PASETO v4.public
(Ed25519). A JWT/EdDSA representation is offered for ecosystems whose libraries
expect JWT.

**Why.** PASETO removes algorithm-confusion and `alg: none` classes of bugs. JWT
compatibility matters for merchant adoption.

## ADR-010 — Policy is first-match, YAML, versioned, hot-reloaded

**Decision.** Rules are evaluated top-down, first match wins; `control: none`
rules exist for discovery; policy changes apply to new acquires only.

**Why.** Easy to reason about and to explain in an audit ("rule X matched"). Rule
composition can be added later without breaking existing policies.

## ADR-011 — Identity anchors are consumed, never computed

**Decision.** Bruiser accepts anchors (membership number, household, payment
fingerprint) as claims in the merchant's customer assertion and can scope
domains on them. It never derives them.

**Why.** Keeps Bruiser honest about the identity boundary, avoids handling raw
PII, and still gives merchants a lever against multi-accounting.

## ADR-012 — Torture harness with an offline invariant checker is a first-class deliverable

**Decision.** `cmd/torture` and the I1–I7 checker are built at M2, run on every PR
(short) and nightly (long), and block release.

**Why.** The invariant is the product. A test that only exercises the happy path
under load would not detect the failure modes that matter (node death mid-grant,
store pause, clock skew, handoff races).

## ADR-013 — Admin UI is server-rendered with htmx; no JS build pipeline in V1

**Decision.** Templates + htmx + SSE served from the gateway binary.

**Why.** Functionality and observability matter more than polish; one binary stays
one binary; no front-end toolchain to maintain in an OSS project at this stage.

**Revisit when.** Core/Enterprise UI requirements outgrow it.

## ADR-014 — Licensing: Apache-2.0 for protocol and SDKs; BSL 1.1 for the gateway core

**Decision.** Protocol, schemas, specifications, SDKs and reference edge configs
are Apache-2.0. The gateway core is BSL 1.1 with an Additional Use Grant permitting
production use and forbidding competing hosted Bruiser services, Change Date four
years per release, Change Licence Apache-2.0. Subject to legal review. Repository
private until the OSS/Core boundary is formalised. The gateway tier is described as
*source-available*, not open source.

**Why.** Founder decision Q3: maximise adoption of the protocol while protecting
the commercial product from being hosted as a competing service.

**Consequences.** Repository laid out for a clean split into `bruiser-protocol`
(public, Apache-2.0) and `bruiser-gateway` (BSL) at M8. No licence files published
before legal sign-off.

## ADR-015 — Identity anchors to the club's existing supporter identity

**Decision.** `customer_id` is the club's membership/supporter number where
available, otherwise the club's account identifier. Bruiser never creates
identifiers, stores credentials or runs a login.

**Why.** Founder decision Q1. Keeps Bruiser out of the identity business and ties
the guarantee to the identity the club already trusts and can support.

## ADR-016 — A hosted public sandbox is in scope; production is self-hosted

**Decision.** `sandbox.bruiser-gateway.com` runs the same binary and Helm chart with
self-service throwaway merchants, the live demo and hosted docs. It is a sales and
developer-discovery asset. Production deployments are self-hosted by the merchant,
which is also the primary data-residency control.

**Why.** Founder decision Q5 and Q9. Developers need to try the API before a
club will deploy it; the sandbox is also the only multi-tenant Bruiser and so
proves tenant isolation.

## ADR-017 — Simple pricing: fair-use envelope, no metering; usage figures reported, never enforced

**Decision.** Core is £20k/yr within a documented fair-use envelope; Enterprise
from £100k/yr. The gateway reports peak concurrent executions, executions per month
and resources protected in the admin UI. It never throttles or licenses on them.

**Why.** Founder decision Q12. Simplicity sells; metering infrastructure is not a
V1 problem.

## ADR-018 — Audit retention default 13 months, configurable per merchant

**Decision.** `audit.retention` defaults to 13 months; a daily purge/archive job
runs; retention changes and purges are audited.

**Why.** Founder decision Q9. Covers a full season plus dispute lag; merchants
adjust to their own legal requirements.

## ADR-021 — V1 execution tokens are JWT/EdDSA; PASETO remains the protocol default

**Decision.** M1 ships JWT signed with Ed25519 (`alg: EdDSA`, JWKS OKP/Ed25519).
PASETO v4.public remains the protocol's preferred format and can be added as a
second representation without changing claims or fences.

**Why.** JWT/EdDSA maps directly onto merchant JWKS verification (the Embedded
deployment method) and has library support in Go, Node and Python on day one.
The claims (`exe`, `dom`, `res`, `act`, `prn`, `fnc`, `exp` = lease expiry) are
identical to the design.

**Revisit when.** SDKs land; add PASETO as a parallel token type if agent
frameworks prefer it.

## ADR-019 — Execution Amplification Factor is the primary operational KPI

**Decision.** EAF = incoming allocation attempts ÷ authorised executions
forwarded. The admin dashboard, demo and sales conversation lead with it.
Reported as observed EAF (how much autonomous demand was absorbed) and
downstream EAF (should sit at 1× under `max_active: 1`), per merchant / resource
/ customer, over selectable windows. Never used for billing or throttling.

**Why.** Turns the 10,000-agent story into a number a ticketing office can watch.
Makes the product claim measurable rather than rhetorical.

## ADR-020 — Commercial messaging rule is binding

**Decision.** Customer-facing copy never describes Bruiser as a Redis lock, bot
detection, DDoS protection, a ticketing platform, middleware, a proxy or an API
gateway. It always describes Bruiser as: the authoritative control layer for
autonomous commerce that ensures one customer remains one customer, regardless
of how many agents they deploy. Coordination technology and deployment methods
are implementation details.

**Why.** Founder product principle. Selling a lock or a proxy invites a build-vs-buy
comparison Bruiser loses; selling authority invites a fairness conversation Bruiser
wins.

## ADR-022 — Arsenal-like profile is the standing lab analogue until discovery

**Decision.** Until Stage A discovery confirms a design partner, engineering
builds against an **unverified Arsenal-like** Premier League club profile
(`docs/07-club-profile-arsenal.md`, `configs/arsenal.yaml`). It is a working
model of a club whose box office is a platform (Ticketmaster eTicketing
shape), whose customer id is a membership number, and whose viable V1 path
is **Edge** (Embedded blocked until a platform hook exists). Nothing in the
profile is a claim that Arsenal FC is a customer.

**Why.** Founder instruction to proceed on best-guess assumptions for a club
like Arsenal until discovery can confirm. Waiting on a call would stall M3;
a wrong-but-explicit analogue is cheaper to replace than an invented generic
shop.

**Consequences.** Lab merchant id `arsenal`; cookie `boxoffice_session`;
invented hold/purchase paths; SimTix is the origin analogue; Authority Check
targets those paths. Every row is tagged unverified and must be overwritten
the moment a discovery call lands. If discovery finds the club cannot front
the box office and has no hook, this profile is not a V1 customer — the lab
remains useful for clubs that look like a simpler version of the same model.

**Revisit when.** First Stage A questionnaire comes back; replace or fork the
profile rather than silently drifting.

## ADR-023 — Heartbeat renewal and reconnect recovery

**Decision.** An ACTIVE execution is kept alive by a lightweight heartbeat every
20–30 seconds (default 25 s). Each heartbeat is `POST /v1/executions/{id}/renew`
(alias `POST /v1/executions/{id}/heartbeat`) and sets
`expires_at = least(now()+ttl, max_lifetime_at)`. Default lease TTL is 60 seconds.
If heartbeats stop, the lease expires and is released automatically.

The same principal re-acquiring before expiry is a resume, not a new grant:
ALREADY_HELD extends the TTL, increments `renew_count`, rebinds `session_id`, and
audits `EXECUTION_RENEWED` with reason `resume`. A reconnect after expiry may
acquire a new execution under merchant policy.

On Edge and Proxy, a Bruiser-unaware client does not speak the protocol: the same
cookie presenting again is the heartbeat.

**Why.** Acquire / renew / release / expire / revoke / handoff named the
lifecycle but did not say *how* renewal happens. Without an explicit heartbeat,
a closed tab either holds the domain until max lifetime or a refresh looks like a
second competing principal. Recovery is an acceptance criterion: brief disconnects
must not lock the customer out, and abandoned tabs must not hold the domain.

**Consequences.** Holder checks accept the original session *or* a new session
bound to the same principal. Clients receive `heartbeat_after_ms` on ACTIVE
execution responses. `BRUISER_HEARTBEAT_INTERVAL` (default 25 s) is independent of
`BRUISER_LEASE_TTL` (default 60 s).

**Revisit when.** A design partner needs a different default TTL (for example a
longer basket hold) or wants heartbeats tied to inventory-hold keepalives rather
than the execution lease.

