# Bruiser Gateway — Product & Technical Brief (v2)

Working name: **Bruiser Gateway** · Domain: bruiser-gateway.com

This is a revision of the original brief. It keeps the product direction intact and
sharpens the parts that would otherwise cause trouble in engineering, sales or
enterprise due-diligence. The concrete design lives in
[02-technical-design.md](02-technical-design.md); the build sequence lives in
[03-execution-plan.md](03-execution-plan.md).

---

## 0. What changed from v1, and why

| # | Change | Why it matters |
|---|--------|----------------|
| 1 | **The guarantee is stated precisely.** Bruiser bounds *concurrent authorised execution per scarcity domain*. It does not bound *how many customer identities one human controls*. | The original thesis silently depends on the merchant having strong customer identity. If Alice can register 10,000 accounts, a per-customer lease is worthless. Being explicit about this boundary is what makes the pitch survive a CTO conversation, and it points at the real product extension (identity anchors, §4). |
| 2 | **Two enforcement modes, with token verification as the primary.** Bruiser issues a signed *execution token*; the merchant's hold/purchase path verifies it. Proxy mode is secondary. | A proxy in the purchase path is a single point of failure and a hard sell to a club's ticketing vendor. A token the merchant verifies keeps Bruiser out of the data path, makes outages degrade safely, and makes integration a middleware, not a re-architecture. |
| 3 | **Execution lease ≠ inventory hold.** A lease is permission to *attempt* allocation. Inventory holds stay in the merchant's system. | Prevents Bruiser drifting into being a ticketing system (Principle 5). |
| 4 | **Fencing tokens are part of the primitive.** Every grant carries a monotonically increasing fence per domain. | This is what makes revoke and handoff *safe*, not merely *recorded*: a preempted agent's still-unexpired token is rejected downstream because its fence is stale. Without this, "take control" is cosmetic. |
| 5 | **Denial never needs coordination; only grant does.** | Turns the distributed-correctness requirement into a design rule. Any gateway node can answer BUSY from local knowledge; only GRANT goes to the coordination store. Load from 10,000 agents is absorbed cheaply and the invariant is never at risk. |
| 6 | **Queueing is scoped down for V1.** V1 returns BUSY with a wait hint and a watch channel. A *bounded* waiting set is V1.5. Inter-customer fairness (virtual waiting rooms) is explicitly out of scope. | 10,000 queue entries per customer is a DoS vector and a distributed-systems problem in its own right. Bruiser is *intra-customer* concurrency control; Queue-it-style *inter-customer* fairness remains the merchant's waiting room. Mixing them muddies both the product and the sale. |
| 7 | **Fail-safe semantics are defined.** Allocation fails closed; discovery fails open; existing leases keep working until natural expiry; lease time is the store's clock, not node clocks. | "Fail safely" is a requirement, not a design. This is the design. |
| 8 | **Sybil resistance is reframed as "identity anchors".** Policy scopes can key on membership number, household, payment fingerprint etc. supplied by the merchant. | Gives merchants a lever against multi-accounting without Bruiser pretending to solve identity. |
| 9 | **Agent-side adoption path added.** SDKs and an MCP/tool definition so agent frameworks speak Bruiser natively. | The protocol only becomes a standard if agents implement it, not just merchants. |
| 10 | **Calendar estimates removed from the plan; replaced by milestones with exit criteria.** | Milestones are verifiable; dates are not. |

### v2.1 — incorporating founder decisions

Founder answers to the open questions (recorded in
[05-decisions-from-founder-review.md](05-decisions-from-founder-review.md)) changed
the following:

| # | Change | Source |
|---|--------|--------|
| 11 | **Authority is a requirement, not a mode.** Bruiser must be authoritative over the protected allocation operation. Integration is defined as closing every path to `hold`/`purchase` that does not pass Bruiser's admission check. Three enforcement patterns are offered (in-app middleware, edge/API-gateway integration, Bruiser reverse proxy); the merchant picks whichever makes Bruiser authoritative in their environment. | Q2, Q13 |
| 12 | **Enforcement must work for agents that have never heard of Bruiser.** In edge and proxy patterns Bruiser derives the customer from the merchant's existing authenticated session and acquires the execution *transparently*. Bruiser-aware agents get a better experience (watch, handoff, explicit renew); unaware agents are still controlled. Agent-facing tooling (SDKs, MCP) is convenience, never the enforcement mechanism. | Q8, Q13 |
| 13 | **Identity anchors to the club's existing supporter identity.** Membership/supporter IDs preferred; existing account login acceptable initially. Bruiser does not create an identity system. | Q1 |
| 14 | **Licensing decided in principle.** Protocol, schemas, specifications and SDKs: Apache-2.0. Gateway core: BSL 1.1, subject to legal review. Repository private until the OSS/Core boundary is formalised. The gateway tier is therefore described as *source-available*, and the genuinely open-source assets are the protocol and SDKs. | Q3 |
| 15 | **Hosted public sandbox/demo is in scope** as a sales and developer-discovery asset; production remains self-hosted. | Q5 |
| 16 | **Design-partner acquisition is an immediate commercial priority** and the integration-discovery questionnaire ([06-integration-discovery.md](06-integration-discovery.md)) is how those conversations validate requirements. | Q7 |
| 17 | **Audit retention** configurable, default 13 months. **Pricing** stays simple: Core within a documented fair-use envelope, no per-request metering; the gateway reports plain usage figures for transparency. **Trademark clearance** before public launch or significant commercial investment. **External security review** is an M8 exit criterion. | Q9–Q12 |
| 18 | **Positioning principle made explicit: Bruiser is not a distributed-lock product.** Coordination technology is an implementation detail. The product is the identity, execution-control, lease, policy, queueing, handoff, audit and merchant-integration layer around scarce-inventory transactions. | Founder principle |

---

## 1. Vision

Bruiser Gateway is a control layer between autonomous software agents and systems
holding scarce or finite inventory.

The problem: one customer should not be able to multiply their purchasing power by
deploying many autonomous agents. Most commerce and ticketing systems treat each
session, request or automation as an independent actor. In an agent-driven world a
single customer might field 10, 100 or 10,000 agents simultaneously searching,
reserving, retrying and buying the same scarce inventory.

Bruiser sits between those agents and the merchant's system and enforces the
merchant's definition of legitimate concurrent access.

```
Alice                                          Alice
 ├── Agent 1 ─────┐                             ├── Agent 1 ─────┐
 ├── Agent 2 ─────┤                             ├── Agent 2 ─────┤
 ├── ...          ├──> Ticketing system         ├── ...          ├──> Bruiser ──> Ticketing system
 └── Agent 10,000 ┘                             └── Agent 10,000 ┘      │
                                                                        └── one authorised execution
```

First vertical: football ticketing. Long-term: any finite-capacity system —
concerts, product drops, restaurants, hotels, flights, appointments, auctions.

The first release is deliberately narrow and must be technically excellent.

## 2. The proposition, stated precisely

> **One customer may have many agents, but only the merchant-permitted number of
> authorised executions for that customer may act on a given scarcity domain at a
> time.**

What Bruiser guarantees (the invariant):

> For any scarcity domain, the number of ACTIVE executions never exceeds the
> merchant's configured `max_active`, across all gateway instances, under
> concurrent requests, node failures and store failures.

What Bruiser explicitly does **not** guarantee:

- that two customer identities are not the same human (that is the merchant's
  identity problem — Bruiser gives the merchant *levers*, see §4);
- fairness *between* customers (that is a waiting room / lottery problem);
- that a request came from a human rather than an agent (Bruiser does not care);
- payment, fraud or inventory correctness (those remain with the merchant).

The merchant's system does not need to decide whether a request is human or AI. It
receives requests carrying a Bruiser execution token that already establishes:

- who the customer is
- which principal (agent or browser) is acting for them
- what resource they are accessing
- which execution is currently authorised, and its fence
- that the merchant's concurrency policy has been satisfied.

## 3. Conceptual model

| Concept | Question it answers | Example |
|---------|---------------------|---------|
| **Merchant** | Who owns the inventory? (tenant) | `arsenal` |
| **Customer** | Who owns the purchasing authority? | `cust_alice` |
| **Principal** | What is acting for the customer right now? Either an `agent` or a human-present `browser`. | `agent:shopping-agent-123`, `browser:sess-77` |
| **Resource** | What scarce thing is being acted on? | `event:ars-che-2026-10-04` |
| **Scarcity domain** | The key concurrency is counted against; derived from policy scope + request. | `arsenal/cust_alice/event:ars-che` |
| **Execution (lease)** | Which authorised operation currently holds permission to attempt allocation in a domain? | `exe_839281`, fence `17` |
| **Execution token** | Signed proof of an ACTIVE execution the merchant can verify. | PASETO/JWT, `exp` = lease expiry |

These are separate. The system must never collapse "customer" and "agent", and
must never treat "execution" as belonging to an agent — **the execution belongs to
the authenticated customer**; principals hold it on the customer's behalf.

## 4. Identity: what Bruiser trusts and what it adds

Bruiser does **not** authenticate humans and does not create an identity system.
It anchors to the club's existing authenticated supporter/customer identity. The
preferred anchor is a membership or supporter ID; an existing account login is
acceptable initially. Bruiser obtains that identity in one of two ways:

- a signed *customer assertion* from the merchant's identity provider (OIDC ID
  token or merchant-signed JWT), exchanged for a Bruiser session — used by
  Bruiser-aware agents and browsers; or
- the merchant's **existing session credential** (cookie or bearer token),
  validated by Bruiser with the merchant's keys at the edge or proxy — used to
  control clients that do not speak the Bruiser protocol (§9).

Either way, a principal is bound to the customer.

To give merchants leverage against multi-accounting, a customer assertion may
carry **identity anchors** — attributes the merchant already knows:

```
customer_id:      cust_alice
anchors:
  membership_no:  ARS-00123456
  household_id:   hh-8812
  payment_fp:     pf-3f9a…   (hashed card/payment-method fingerprint)
```

Policy scopes may key on any anchor, so a merchant can say "one active purchase
execution per *household* per event" rather than per account. Bruiser never
computes these anchors; it consumes them. This is the honest line between "we
control concurrency" and "we solve identity".

## 5. Core primitive: the Execution Lease

```
ACQUIRE ──> ACTIVE ──(RENEW)*──> RELEASED | EXPIRED | REVOKED | HANDED_OFF
```

A lease is:

- **short-lived** (default TTL 60 s) and **renewable** up to a **max lifetime**
  (default 15 min) — so an abandoned agent cannot hold a domain indefinitely;
- **revocable** by the merchant or by the customer (via precedence rules);
- **fenced** — each grant in a domain has a strictly increasing fence number;
- **scoped** to exactly one scarcity domain;
- **transferable** via handoff (agent → browser, browser → agent, agent → agent);
- **auditable** — every transition is an audit event;
- **idempotent** — a principal re-acquiring its own domain gets its existing lease
  back, so a retry storm from one agent is harmless.

A lease is **not** an inventory hold. It is permission to attempt one.

If another Alice-controlled principal requests an execution in the same domain
while one is ACTIVE, it does not reach the merchant. It receives:

```
409 BUSY
  active_execution: exe_839281
  holder:           agent:shopping-agent-123
  expires_at:       2026-10-04T10:03:31Z
  retry_after_ms:   4000
  watch:            /v1/executions/exe_839281/watch     (SSE / long-poll)
```

## 6. Scope of concurrency: scarcity domains

Policy is configurable, not hard-coded to "one per customer globally". Domain keys
are built from dimensions:

```
customer                           one execution per customer, merchant-wide
customer + resource                one per customer per event          (default for ticketing)
customer + resource_pool           one per customer per price band / stand
anchor:household + resource        one per household per event
customer + principal_type          separate budgets for browsers and agents
```

Example merchant policy (see the technical design for the full schema):

```yaml
domains:
  - name: purchase-per-event
    match:      { action: purchase }
    scope:      [customer, resource]
    max_active: 1
    lease_ttl:  60s
    precedence: [browser, agent]        # browser may preempt agent
  - name: discovery
    match:      { action: search }
    control:    none                    # search is abundant
```

The architecture must not make later dimensions (agent reputation tier, membership
tier, time windows) hard to add.

## 7. Browser ↔ agent handoff

Alice's agent holds the execution. Alice opens the club website and sees:

```
Your booking assistant is currently active.
[Continue with assistant]   [Take control]
```

"Take control" performs a **preemptive handoff**: the agent's execution becomes
`HANDED_OFF`, a new execution with fence `n+1` is granted to the browser principal,
atomically. The agent learns this on its next renew (410 Gone, reason
`HANDED_OFF`) and must stop. Because the merchant checks the fence (or introspects
on purchase), any in-flight request from the agent with its old token is rejected
downstream.

Handoff is governed by a **precedence** list in policy. Default: a human-present
browser outranks an agent; agents cannot preempt browsers; equal ranks require a
cooperative handoff (the holder releases *to* a named principal).

## 8. Absorbing concurrency

10,000 agents from one customer must not produce 10,000 downstream requests.

V1 model:

- exactly `max_active` GRANTs per domain;
- everything else receives BUSY with a `retry_after` hint and a **watch** channel
  so agents wait on an event rather than polling;
- BUSY is answered from gateway-local knowledge wherever possible (no store
  round-trip — denial is always safe);
- per-principal and per-customer rate limits back-stop badly behaved agents.

V1.5 adds a **bounded waiting set** per domain (`max_waiters`, default 1): the
next-in-line is promoted on release/expiry and must claim within a short window.

Out of scope: fair queueing *across* customers. That is the merchant's waiting
room. Bruiser sits behind it.

## 9. Integration: Bruiser must be authoritative

**The authority requirement.** Bruiser can only guarantee its policy if every path
to the protected allocation operation (`hold`, `purchase`, and anything else that
consumes or reserves scarce inventory) passes Bruiser's admission check. Direct
paths must be removed, restricted or otherwise made unable to circumvent policy.
Integration is therefore defined as *closing the unenforced paths*, and every
deployment ships with an **authority check** that probes the allocation endpoints
without a valid execution and confirms they refuse.

Bruiser is platform-agnostic and does not depend on a partnership with any
ticketing platform. First customers are clubs where a technically viable path
exists to make Bruiser authoritative. Three enforcement patterns cover the mixed
setups we expect; all share the same lease core and policy engine.

| Pattern | Where enforcement happens | When it fits |
|---------|---------------------------|--------------|
| **P1 — In-app middleware** | The merchant's checkout code verifies the Bruiser execution token (SDK, offline against JWKS, optional introspection at purchase). | Club controls its checkout code. Bruiser stays out of the data path; an outage stops new grants but not authorised purchases. |
| **P2 — Edge / API-gateway** | The merchant's existing reverse proxy, API gateway or CDN worker (NGINX `auth_request`, Envoy `ext_authz`, Kong/Tyk plugin, Cloudflare Worker…) asks Bruiser `/v1/authorize` before forwarding allocation requests. | Club or platform fronts checkout with an edge it controls but cannot change the application. |
| **P3 — Bruiser reverse proxy** | Bruiser itself terminates allocation traffic and forwards to the merchant/platform through a *merchant adapter*, attaching the fence. | Neither the app nor an edge can be changed; or for the simulator and demos. |

In every pattern the origin must only accept allocation traffic that has passed
the enforcement point (network policy, mTLS or shared secret between edge and
origin, WAF rule requiring a Bruiser header). The authority check verifies this.

**Transparent enforcement.** Enforcement cannot depend on agents choosing to use
Bruiser. In P2 and P3 Bruiser identifies the customer from the merchant's existing
session credential and, on the first allocation request, **acquires the execution
on the customer's behalf**. A second agent presenting the same customer identity
receives BUSY (or is preempted per precedence) exactly as a Bruiser-aware agent
would. Bruiser-aware clients gain explicit acquire/renew/watch/handoff and better
UX; unaware clients are still controlled. Agent-facing SDKs and the MCP tooling
(§10) are adoption aids, not the enforcement mechanism.

The adapter interface (`search / hold / release / purchase / cancel`) is designed so
future adapters (ticketing platforms, ecommerce, hotels, restaurants, appointments)
are additive. V1 ships one adapter: the simulator.

## 10. Bruiser Protocol

The long-term asset is an open, interoperable protocol for autonomous access to
scarce inventory. Protocol v0 is defined as: the HTTP+JSON API, the execution token
format and claims, the policy semantics, the audit event vocabulary, and the
merchant verification rules.

| Primitive | V1 status |
|-----------|-----------|
| IDENTITY (sessions, principals, anchors) | implemented |
| ACQUIRE / RENEW / RELEASE / REVOKE / HANDOFF | implemented |
| HOLD / ALLOCATE / PURCHASE / CANCEL | admission-checked at the enforcement point (P1–P3); pass-through via adapter in P3 |
| DISCOVER / QUOTE | uncontrolled pass-through; protocol names reserved |
| REFUND | reserved |

The protocol, its schemas, specifications and SDKs are **Apache-2.0**, so that
developers, agent frameworks and alternative gateway implementations can adopt them
freely. The architecture is protocol-first — OpenAPI, JSON Schema, token spec and a
conformance suite — so nothing about it is bound to the Go implementation. Native
agent-facing integrations (MCP server / tool definitions, agent SDKs) follow at
V1.5; they improve the agent's experience but are never required for enforcement.

## 11. Deployment

Customer-controlled infrastructure. A merchant runs Bruiser in their own AWS,
Azure, GCP, private cloud, Kubernetes or on-premise environment and retains control
of data, network, auth, policy, logs and availability.

Runtime footprint for V1: **one static binary + PostgreSQL**. Nothing else is
required. Developer environment: `docker compose up`. Enterprise: Helm chart,
horizontal scaling, external Postgres (RDS/Cloud SQL/etc.). Self-hosting is also
the primary mechanism for data residency: the merchant's data never leaves the
merchant's environment.

Bruiser provides software, protocol, updates and support — not production hosting.
The one hosted Bruiser is the **public sandbox/demo** (`sandbox.bruiser-gateway.com`):
a sales and developer-discovery asset where developers can obtain a throwaway
merchant, try the API and SDKs, and watch the demo scenarios. It runs the same
binary and is not a production offering.

## 12. Security posture

- Ed25519-signed execution tokens with key rotation via JWKS.
- Customer assertions verified against merchant-configured JWKS; principals bound
  to sessions; sessions short-lived.
- Merchant admin API with hashed API keys or mTLS; RBAC (admin / operator /
  viewer / auditor).
- Replay protection via short expiry, `jti`, idempotency keys and fences.
- Tenant isolation enforced in every query; optional Postgres RLS.
- Rate limiting per principal, customer and merchant.
- **Allocation fails closed.** If Bruiser cannot prove an execution is authorised,
  it does not grant.
- Append-only audit log; health and readiness endpoints; metrics, traces, logs.
- Threat model and external security review before first production deployment.

## 13. Distributed correctness

Multiple gateway instances receive requests simultaneously and must never grant
more than policy permits. V1 achieves this by making grants **serialise on a
per-domain row in PostgreSQL** (row lock, store clock for expiry, fence increment
in the same transaction). Denials do not need the store. The lease store is behind
an interface so a different coordination backend can be added later without
changing the API or semantics.

This is verified by a dedicated torture harness (many gateways, many agents,
process kills, store pauses, clock skew, latency injection) with an offline
invariant checker over the full history. It runs in CI.

## 14. Demonstration

`docker compose --profile demo up` starts: 3 gateway instances behind a load
balancer, Postgres, **SimTix** (a simulated ticketing system with 1,000 seats,
time-limited holds and its own request log), an **agent swarm** generator, and a
live dashboard.

Scenarios:

| Scenario | Without Bruiser | With Bruiser |
|----------|-----------------|--------------|
| 1 customer × 10,000 agents × 1 event | 10,000 hold attempts hit SimTix; hold churn; Alice may end up with several seats | 1 GRANTED, 9,999 BUSY; SimTix sees one execution; Alice gets one seat |
| 1,000 customers × 10 agents × 1,000 seats | 10,000 concurrent holds thrash 1,000 seats; legitimate customers see "unavailable" while holds expire | ≤1,000 downstream executions; each customer's agents coalesce |
| Handoff | — | Alice clicks "Take control"; agent's next renew returns 410; browser completes purchase; agent's stale token rejected by SimTix |
| Bypass attempt | agent goes straight to SimTix and succeeds | the same request is refused at the origin; the authority check reports no open paths |
| Unaware agent | — | an agent that has never heard of Bruiser hits checkout with the club session cookie; Bruiser acquires transparently; the customer's second agent gets BUSY |

The dashboard shows side by side: downstream requests, active executions, BUSY
responses, seats-per-customer distribution, SimTix p99 latency. The point must
land in seconds.

## 15. Admin interface

Functional, not pretty. Merchant sees gateway status, active customers, active and
waiting executions, denials, expirations and allocation activity. Per customer:
current execution, holder principal, resource, timestamps, `[Revoke]` and
`[Take control]`. Policy view and validation. Audit search: "why was this request
allowed, denied or delayed?"

## 16. Auditability

Every transition emits an event:

```
CUSTOMER_AUTHENTICATED   PRINCIPAL_AUTHENTICATED
EXECUTION_REQUESTED      EXECUTION_GRANTED        EXECUTION_DENIED
EXECUTION_BUSY           EXECUTION_QUEUED         EXECUTION_RENEWED
EXECUTION_RELEASED       EXECUTION_EXPIRED        EXECUTION_REVOKED
EXECUTION_HANDED_OFF     POLICY_UPDATED           STORE_UNAVAILABLE
```

Each event carries merchant, customer, principal, domain, execution, fence, policy
rule matched, store timestamp and a decision reason. Events are append-only and
exportable (JSONL, webhook, OTel). Retention is configurable per merchant; the
default is 13 months, subject to legal/security review and individual merchant
requirements.

## 17. Product tiers and licensing

| Tier | Price | Purpose | Contents |
|------|-------|---------|----------|
| **Community** (source-available) | £0 | adoption, credibility, protocol spread | gateway core under BSL 1.1 (production use permitted; offering Bruiser as a hosted service to third parties is not), protocol spec and SDKs under Apache-2.0, simulator, basic admin, Compose deployment, community support. Genuinely usable by a capable team. |
| **Core** | £20k/yr within a fair-use envelope | production for clubs and mid-size operators | commercial licence, supported releases and upgrades, full policy engine, admin UI, audit export and retention tooling, analytics, Helm chart, SSO for admin, documented integrations, commercial support, security updates |
| **Enterprise** | from £100k/yr | mission-critical, high-volume | everything in Core plus advanced policy, bounded waiting, multi-region, enterprise auth, HA guidance, compliance pack, premium adapters, SLA, dedicated support, deployment assistance |

**Licensing.** Protocol, schemas, specifications and SDKs: Apache-2.0, to maximise
adoption. Gateway core: BSL 1.1 (subject to legal review), with an Additional Use
Grant permitting production use and forbidding use of the software to provide a
competing hosted Bruiser service, and a Change Licence of Apache-2.0 after the
change date. BSL is not an OSI-approved open-source licence, so the gateway tier is
described honestly as *source-available*; the open-source assets are the protocol
and SDKs. The repository stays private until the boundary is formally decided.

**Fair-use envelope.** Pricing stays simple: no per-request metering. Core is
bounded by a documented envelope (indicative dimensions: one production deployment,
a single legal entity, up to a stated peak of concurrent executions and events per
year — numbers to be set with the first design partners). The gateway reports plain
usage figures (peak concurrent executions, executions per month, events protected)
in the admin UI so both sides can see where a deployment sits; it never throttles
on them.

The rule for the boundary: Community must be good enough that a developer builds
with it and a small merchant could run it; Core must be the obvious choice the
moment someone's job depends on it.

## 18. Why customers keep paying

The problem evolves; Bruiser keeps the control layer current. Ongoing value:
protocol evolution, security updates, agent-framework compatibility, policy engine
development, new adapters, anonymised industry benchmarks, and (later) a trusted
agent identity/reputation network. The merchant is paying for a maintained
standard, not for a lock.

## 19. Positioning and moat

**Bruiser is not a distributed-lock product.** Whether V1 coordinates through
PostgreSQL, or a later version through Redis or something else, is an
implementation detail and must never appear in positioning. The product is the
customer-identity, execution-control, lease, concurrency, policy, queueing,
handoff, audit and merchant-integration layer that sits around scarce-inventory
transactions. Bruiser is not a bot detector, not a DDoS product, not a ticketing
platform and not a locking service; it is a control layer for autonomous commerce.

A competent team can build `customer_id + lock` in a week. Defensibility is built
around: protocol adoption, agent-side SDKs/tooling, merchant adapters and
enforcement patterns, the authority check and integration know-how, policy engine,
handoff UX, audit/fairness evidence for disputes, operational expertise, security
track record, benchmarks and — later — reputation network effects. The ambition is
for Bruiser to be the recognised admission standard between agents and scarce
inventory.

## 20. Go-to-market

Vertical: football ticketing — scarce inventory, concentrated demand, supporter
frustration, public fairness pressure, existing infrastructure, high-value
transactions, extreme spikes.

The pitch is not "we stop bots". It is:

> **One supporter remains one supporter, regardless of how many agents they deploy.**
> You control the infrastructure. Bruiser provides the control layer.

Two motions run in parallel: OSS → developer adoption → protocol familiarity →
production; and outbound → design-partner clubs → Core → Enterprise → references.
Do not wait for OSS traction before selling Core.

There are no confirmed design partners yet; acquiring them is an immediate
commercial priority that runs alongside development. Each club conversation is
also an integration-discovery exercise ([06-integration-discovery.md](06-integration-discovery.md)):
where is the admission point, who controls it, and which enforcement pattern makes
Bruiser authoritative there. Clubs with a viable path are qualified; clubs whose
allocation path cannot be closed are deferred, not force-fitted. Bruiser does not
wait on any ticketing-platform partnership to validate the product.

## 21. Where Bruiser sits

```
Internet
  ↓  CDN / WAF / DDoS (unchanged)
  ↓  Merchant waiting room (inter-customer fairness, unchanged)
  ↓  Merchant IdP (customer authentication, unchanged)
  ↓  Bruiser Gateway — customer concurrency, execution leases, policy, audit
  ↓  Merchant ticketing / commerce system (verifies execution token)
  ↓  Inventory
```

Bruiser complements CDN, WAF, DDoS, waiting rooms, payments and ticketing. It
replaces none of them.

## 22. Principles

1. Do not detect humans vs AI. Authenticate the customer, control the execution.
2. Bruiser is authoritative at the admission point, or it is not deployed. Close
   the bypass paths first.
3. Enforcement never depends on the agent cooperating. Aware agents get a better
   experience; unaware agents are still controlled.
4. Separate discovery from allocation. Search is abundant; allocation is controlled.
5. Denial needs no coordination; only grant does. Design around it.
6. Never be a single point of failure unnecessarily. Prefer the enforcement
   pattern that keeps Bruiser out of the data path when the merchant can support it.
7. The merchant remains in control — of infrastructure, data, identity and policy.
8. Do not build a ticketing system, an identity system or a lock product. A lease
   is not a hold; coordination technology is an implementation detail.
9. Fail closed on allocation, open on discovery.
10. Do not over-engineer V1. The primitive must be flawless before anything else.

## 23. Not in V1

AI/agent detection · ML fraud detection · reputation network · blockchain · payment
processing · a ticketing platform · a universal inventory database · many merchant
integrations · lotteries / inter-customer fair queueing · mobile apps · a
proprietary agent · microservices · cloud-specific infrastructure · Redis or any
second datastore.

## 24. Definition of success

A developer deploys Bruiser and demonstrates, on multiple gateway instances under
chaos: **1 customer, 10,000 agents, 1 scarce event, exactly 1 authorised
execution at any moment — never a duplicate grant.** Then a merchant writes their
own policy and watches it enforced, revokes an execution, hands control to a
browser, and answers "why was this request denied?" from the audit log.

The result must be reliable, secure, observable, self-hostable, easy to integrate
(a middleware, not a project), easy to demonstrate, and credible to an enterprise
CTO on first read of the design.

## 25. Long-term vision

The immediate product is a customer concurrency gateway. The long-term company is
**the control layer for autonomous commerce**: who the customer is, which agent
acts for them, what they are authorised to do, how much concurrent purchasing
power they have, how reservations are controlled, how execution transfers, and how
it is all audited.

First proof point: one customer remains one customer, no matter how many agents
they deploy.
