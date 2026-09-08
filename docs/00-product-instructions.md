# Bruiser Gateway — Product Instructions (v2.4)

Binding. Engineering is judged against §34. Design detail lives in
[02-technical-design.md](02-technical-design.md).

Working name: **Bruiser Gateway** · Domain: bruiser-gateway.com

---

## 1. Product vision

Bruiser Gateway is a merchant-controlled execution control layer for autonomous
commerce.

Its core purpose is to ensure **one customer remains one customer, regardless
of how many agents they deploy.**

A customer may use one browser, multiple tabs, multiple devices, or thousands
of autonomous agents. Bruiser must recognise those requests as belonging to
the same authenticated customer and control access to scarce-inventory
execution accordingly.

Bruiser is not a ticketing platform, identity provider, bot detector, DDoS
service, waiting-room replacement, or distributed-lock product.

Distributed coordination technology such as Redis may be used internally, but
it is an implementation detail and must never define the product.

## 2. Product philosophy

**Principle 1 — Customer, not agent, is the unit of concurrency.** Bruiser
controls execution at the customer level. If Alice has 10,000 agents, Bruiser
must not treat them as 10,000 independent customers.

**Principle 2 — Identity is merchant-owned.** Bruiser does not create or own
the merchant’s customer identity system. The merchant keeps login, SSO,
membership, accounts, authentication and entitlement. Bruiser is
identity-aware, not identity-authoritative.

The merchant authenticates the customer. Bruiser governs what that
authenticated customer can do against scarce inventory.

Identity extraction is **configuration**, not a bespoke project: JWT, cookie
JWT/session, session introspection, edge-signed identity headers, and other
standard authenticated-session mechanisms.

## 3. Merchant-controlled / self-hosted

Bruiser is deployed inside the merchant’s own infrastructure. The merchant
controls infrastructure, networking, authentication, customer data, inventory,
execution state, audit, retention, availability, security and policies.

The vendor does not need to receive customer data.

```
Customer / Browser / Agent
          │
          ▼
Merchant infrastructure
          │
     Bruiser Gateway
          │
          ▼
Existing ticketing / commerce system
          │
          ▼
Scarce inventory
```

Customer IDs, membership numbers, payment information, ticket information,
execution state and audit records remain inside the merchant’s environment.

Any telemetry sent to Bruiser (the vendor) must be explicitly opt-in and must
not contain personally identifiable customer information by default.

**Default telemetry: disabled.**

## 4. Easy to install

Bruiser is a drop-in product, not a consulting-led integration.

Do not make discovery calls a prerequisite for determining whether Bruiser can
work. Build the generic product first. Common deployment patterns are
configurable adapters.

The merchant installs Bruiser on an existing infrastructure pattern and
configures identity, resource scope, concurrency and lease — they do not
custom-build Bruiser for every merchant.

## 5. Standard enforcement patterns

These are deployment patterns for the **same product**, not separate products.

| Pattern | Placement |
|---------|-----------|
| **Embedded** | Middleware inside the merchant application |
| **Edge** | Existing API gateway / reverse proxy / CDN / WAF / Worker calls Bruiser before the scarce operation |
| **Proxy** | Bruiser reverse-proxies the existing ticketing/commerce API |

The merchant must not have to redesign its ticketing platform.

## 6. Authority is the requirement

The integration mechanism does not matter. Bruiser must be authoritative over
the scarce-inventory operation it is protecting.

`Client → Bruiser → Allocation API` must be the authoritative path. A parallel
`Client → Allocation API` path must be prevented, restricted, or otherwise
unable to bypass Bruiser.

## 7. Bruiser Authority Check

`bruiser authority-check` is a first-class go-live gate and a sales/demo
artefact. A production deployment is not ready unless it PASSes for all known
scarce-inventory routes. The demo includes a visible attempted bypass that
Bruiser prevents.

## 8. Transparent enforcement

Enforcement must not depend on an agent using Bruiser’s SDK, MCP server or
protocol. An agent that only knows the merchant’s existing API is still
subject to Bruiser.

Bruiser-aware agents get a better experience (acquire, renew, watch, release,
handoff). Bruiser-unaware clients receive the same rules.

Transparent **dry-run** (`BRUISER_MODE=dry-run` or admin controls) uses this
same path and the same policies, records `WOULD_*` decisions, and never
blocks. It is how a club answers “what would Bruiser have done?” before
enforcement. Supporting ops (`bruiser doctor`, `config validate`, emergency
controls, `/readyz`) are scoped in
[09-post-core-capabilities.md](09-post-core-capabilities.md) and must not
delay the core product.

## 9. Separate discovery from allocation

Do not block ordinary browsing. Search, quote, browse and availability stay
inexpensive. Hold and purchase are the controlled boundary. The boundary is
configurable.

## 10–12. Execution lease, heartbeat, recovery, handoff

Lifecycle: Acquire → Renew* → Release | Expire | Revoke | Handoff.

Defaults: lease TTL **60s**, heartbeat **20s**. Both configurable. Missed
heartbeats expire the lease. Reconnect before expiry resumes the same
execution. After expiry a new execution may be acquired. Bruiser must not
permanently bind a customer to a tab or device.

A browser may take control of an agent’s execution. The customer stays the
same identity; the execution moves.

## 13–14. In-flight limits, queueing, coalescing

A granted execution must not become a tunnel that hammers origin. Per
execution: max in-flight, rate, burst, backpressure.

When many agents of one customer attempt a scarce operation: one ACTIVE,
others BUSY, optionally a bounded intra-customer queue. Duplicate requests
are coalesced where safe. Do not let 10,000 agents independently retry the
origin. Inter-customer waiting rooms remain out of scope.

## 15. Execution Amplification Factor

`EAF = incoming allocation attempts ÷ authorised executions forwarded`.

Primary merchant-facing KPI. Report incoming attempts, forwarded executions,
and absorbed amplification.

## 16–17. Adapter layer and initial integrations

Generic adapter around `search / hold / release / purchase / cancel`. Not
every merchant needs every operation. Add integrations without changing the
core engine.

Build generic integrations first: HTTP reverse proxy, NGINX, Cloudflare
Worker, Node middleware, API gateway authorizers, Docker Compose,
Kubernetes / Helm. Not Ticketmaster/SeatGeek/SecuTix-specific partnerships.

## 18. Identity extraction

Configurable extractors. Merchant authenticates; Bruiser receives enough
authenticated identity to enforce customer-level policy. Do not require
unnecessary personal data.

## 19. Security

Authentication, signed credentials, short-lived execution tokens, expiry,
replay/stale-fence/tamper protection, tenant isolation, secrets, encryption,
RBAC, audit, rate limits, fail-closed allocation, Authority Check. Independent
security review before first production deployment.

## 20–21. Admin and audit

Dashboard: active executions, concurrency, lease expiry, queue depth, absorbed
vs forwarded, EAF, origin load reduction, policy, audit, health, version,
fair-use figures (informational only — not Core price enforcement).

Audit must answer who, which client, acquire/renew/release/revoke/handoff,
which policy, rejects, bypass attempts. Default retention **13 months**,
configurable, with purge/archive.

## 22–23. Multi-tenancy and sandbox

Production is primarily merchant-controlled and can be single-tenant. Hosted
sandbox (`sandbox.bruiser-gateway.com`) uses the same Helm chart where
practical and is the proving ground for multi-tenant isolation. Sandbox is a
sales/developer asset, not the default production architecture.

## 24. Protocol and SDKs

Protocol is independent of the gateway implementation: discover, quote,
allocate, hold, purchase, cancel, execution.acquire/renew/release/revoke/handoff.

MCP / tool definitions are V1.5 agent-facing convenience. Enforcement never
depends on them.

## 25. Licensing

**Apache-2.0** (intended): protocol, schemas, SDKs, public specifications,
edge integration components, developer tooling.

**Gateway core:** BSL 1.1 subject to legal review. Proposed for counsel:
production use permitted; competing hosted Bruiser services prohibited;
change to Apache-2.0 after four years.

Do not describe the BSL core as open source. The free tier is **Bruiser
Community**, not “Bruiser OSS”.

Honest positioning: *Open protocol and SDKs. Source-available commercial
gateway.* Repository stays private until the split is formally approved. No
LICENSE files until then.

## 26. Commercial tiers

| Tier | Price | Role |
|------|-------|------|
| **Bruiser Community** | £0 | Developer adoption, small deployments, ecosystem. Genuinely useful. |
| **Bruiser Core** | £20,000/year | Production self-hosted licence, support, standard integrations, fair-use envelope (reported, not auto-enforced). |
| **Bruiser Enterprise** | £100,000+/year | HA, multi-region, enterprise auth, compliance, premium integrations, SLA. |

## 27–28. Value proposition and what Bruiser is not

Not “stopping AI”. Message: *Your customers are gaining unlimited digital
manpower. Bruiser makes sure one customer remains one customer.*

Two benefits: **fairness** (cannot multiply purchasing power by deploying
agents) and **load reduction** (redundant attempts do not all reach the
expensive transaction layer). Bruiser does not reduce legitimate customers;
it reduces redundant execution.

Do not initially build: a ticketing platform, inventory DB, payments, an AI
agent, human-vs-AI detection, blockchain, reputation network, inter-customer
waiting rooms, lotteries, a mobile app, a bespoke adapter per vendor, or
unnecessary microservices. Do not try to tell human from AI. The authenticated
customer is the identity.

## 29. V1 success test

1 customer + 10,000 agents + 1 scarce event = 1 authorised execution.

The demo must show the swarm, one ACTIVE, BUSY for the rest, origin seeing
one execution, heartbeat, handoff, expiry/recovery, bypass rejected,
Authority Check PASS, EAF. Installable from documented configuration.

## 30–32. Development philosophy and GTM

Build the generic product first. Commercial discovery runs **in parallel**
to validate demand, environments and design partners — not to decide whether
Bruiser can be built.

The product must answer: *Can I put Bruiser in front of my existing system?*
The answer is which standard pattern fits, not whether Bruiser must be
custom-built.

Initial vertical: **football ticketing** as the wedge. Then concerts, retail
drops, reservations, hotels, flights, appointments, auctions, other scarce
digital inventory.

Two tracks in parallel. Do not wait for discovery to engineer. Do not wait
for engineering completion to talk to clubs.

## 33. Moat

The lock is not the moat. Defensibility is protocol, easy deployment,
integrations, agent ecosystem, execution semantics, security, policy,
Authority Check, operations, and becoming the control protocol between
autonomous agents and scarce-inventory merchants.

## 34. North-star

*Can a merchant install Bruiser in front of an existing scarce-inventory
system and reliably enforce “one customer, one active execution” without
handing customer data or infrastructure control to Bruiser?*

If yes, the product is doing its job.

If the solution requires Bruiser to host customer data, replace
authentication, replace ticketing, detect AI agents, or build bespoke
integrations for every merchant, the architecture is moving in the wrong
direction.
