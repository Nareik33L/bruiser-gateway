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
backend later.

**Revisit when.** Torture/load testing shows grant-path latency or connection
pressure that the busy cache and pooling cannot absorb, or an enterprise requires
active-active multi-region grants.

## ADR-002 — Token verification (Mode A) is the primary integration; proxy is secondary

**Decision.** Bruiser issues signed execution tokens the merchant verifies. Proxying
allocation calls through Bruiser is supported but not the default recommendation.

**Why.** Keeps Bruiser out of the purchase data path (no SPOF, no latency added to
checkout), makes integration a middleware rather than a re-plumbing, and degrades
safely: an outage stops new grants but not authorised purchases. Ticketing vendors
are far more likely to accept "verify a token" than "route checkout through a
third-party box".

**Consequences.** Revocation is visible to the merchant on the next token check;
introspection on the purchase step closes the window. Needs SDKs in the merchant's
language.

**Revisit when.** Design partners overwhelmingly cannot modify their checkout.

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
