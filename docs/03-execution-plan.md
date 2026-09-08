# Bruiser Gateway — Execution Plan

This plan sequences the work as milestones with verifiable exit criteria rather
than dates. Each milestone is sized by what changes and how invasive it is, so
progress can be judged by what is demonstrably true, not by elapsed time.

Assumed team: one or two engineers plus the founder on the commercial track. Every
milestone ends with something runnable and a tagged release.

Effort key — **S**: a few focused sessions, one subsystem. **M**: several
subsystems, meaningful test surface. **L**: cross-cutting, needs its own test
harness or external dependency.

---

## Milestone map

```
M0 Foundations
 └─ M1 The primitive (single node)
     └─ M2 Distributed correctness  ◄── the gate that proves the thesis
         ├─ M3 Merchant integration (tokens, SDK, SimTix, proxy)
         ├─ M4 Policy engine
         └─ M5 Handoff & revoke
             └─ M6 Demo
                 └─ M7 Admin & observability
                     └─ M8 Hardening & V1 release
                         └─ Post-V1: Core / Enterprise / protocol
```

M3, M4 and M5 are independent of each other once M2 lands and can run in parallel
if there are two engineers.

---

## M0 — Foundations (S)

**Goal:** a repository a new engineer can clone, build, test and run in minutes.

Scope

- Go module, `Makefile`/`Taskfile`, linting (`golangci-lint`), `go test -race` in CI.
- CI (GitHub Actions): build, unit tests, Postgres service container for
  integration tests, container image build, SBOM.
- `cmd/bruiser serve` skeleton with `/healthz`, `/readyz`, `/metrics`, structured
  logging, config loading (env + file), graceful shutdown.
- Postgres migrations tooling; initial schema from the technical design §4.1.
- Injected `Clock` interface everywhere time is read (prerequisite for torture tests).
- `docs/protocol/v0-draft.md` skeleton; ADR log started (`04-decisions.md`).
- Licence files per the founder's licensing decision (open question Q3); until
  decided, no licence is published and the repo stays private.

Exit criteria

- `make test` green in CI on a clean checkout.
- `docker compose up` starts gateway + Postgres; `/readyz` reflects store health.

---

## M1 — The primitive, single node (M)

**Goal:** acquire / renew / release / expire are correct on one gateway.

Scope

- `internal/lease`: state machine, `Store` interface, Postgres implementation of
  acquire/renew/release with domain row locking, store-clock expiry, fence
  increment.
- Idempotent re-acquire by the same principal; BUSY response shape.
- Expiry as derived state plus a sweeper that materialises `EXPIRED` and emits audit.
- Sessions with dev-mode HMAC customer assertions; principal binding.
- Execution tokens (PASETO v4.public), JWKS endpoint, key generation CLI.
- Audit emitter writing every transition in the same transaction as the mutation.
- Public API v1 for sessions, acquire, renew, release, GET status, watch (long-poll
  first; SSE in M2 with NOTIFY).
- Hard-coded single rule (`customer + resource`, `max_active: 1`) pending M4.
- Property tests (`rapid`) on the state machine; store contract tests on real Postgres.

Exit criteria

- 1 customer × 1,000 in-process agents against one node: exactly one ACTIVE at any
  moment, verified by a test that inspects the DB at random points while load runs.
- Lease expires without any renew; renew past `max_lifetime` is refused; a released
  domain is re-acquirable immediately.
- Every state transition present in `audit_events`, checked by test.

---

## M2 — Distributed correctness (L) — **thesis gate**

**Goal:** the invariant holds across multiple gateways under faults, and we can
prove it repeatedly.

Scope

- Busy cache with hedge ratio; `LISTEN/NOTIFY` fan-out for watch/SSE.
- Fail-closed behaviour on store unavailability (503 + `Retry-After`; readiness
  flips; discovery unaffected). Per-request deadlines.
- `cmd/torture`: N gateways (in-process first, then containers), M×K principals,
  randomised ops, fault injection (kill node, pause store, latency, clock skew,
  drop NOTIFY, restart mid-transaction), full history capture.
- Invariant checker I1–I7 (technical design §11) with minimal-history dump on failure.
- Compose profile with 3 gateways behind Caddy.
- Nightly long-running torture job in CI; a short deterministic-seed variant on
  every PR.

Exit criteria

- Torture harness runs ≥ 10⁶ operations across 3+ nodes with all fault types
  enabled and zero invariant violations, on three consecutive nightly runs.
- Killing any single gateway or pausing Postgres for 30 s never produces a
  duplicate grant; recovery is automatic and audited (`STORE_UNAVAILABLE`).
- Busy-cache hit ratio > 95 % in the 1 × 10,000 scenario; store ops for that
  scenario in the low hundreds.

Risk

- This milestone can reveal a need to change the store design. Nothing in M3–M5
  depends on store internals, only on the `Store` interface, so the blast radius is
  contained. Do not start M6 until M2 is green.

---

## M3 — Merchant integration (M)

**Goal:** a merchant can enforce Bruiser with a middleware, or via proxy, and we
have a realistic simulated merchant.

Scope

- Token verification rules and `POST /v1/introspect`.
- `sdk/go` verification middleware (`net/http`), then `sdk/node` (Express/Fastify
  middleware) and `sdk/python` (ASGI middleware). Each: JWKS fetch/cache, offline
  verification, fence tracking hook, optional introspection, one-line install.
- `internal/adapter` interface; `adapter/memory`; `adapter/simtix`.
- Proxy endpoints (Mode B) with fence attached to downstream calls.
- **SimTix** (`cmd/simtix`): events, seats, price bands, holds with TTL, purchase,
  cancel, non-atomic per-account limit (toggle), Mode A verification via `sdk/go`,
  request log, metrics, reset.
- Integration guide: "Add Bruiser to an existing checkout in 30 minutes".

Exit criteria

- SimTix in Mode A rejects a hold with an expired, tampered or stale-fence token
  and accepts a valid one, covered by tests.
- Same scenario passes through Mode B proxy with identical downstream effect.
- A Node and a Python sample app verify tokens using the SDKs, in CI.

---

## M4 — Policy engine (M)

**Goal:** merchants express scarcity domains in YAML and the gateway enforces them.

Scope

- Policy schema (JSON Schema), compiler, evaluator (first-match), `control: none`
  rules, `fallback`.
- Scope dimensions: `customer`, `resource`, `resource_pool`, `principal_type`,
  `anchor:<name>`; `on_missing_anchor`.
- Per-rule `lease_ttl`, `max_lifetime`, `max_active`, `precedence`.
- Policy storage in Postgres with versions; admin API get/put/validate/history;
  hot reload via NOTIFY; `POLICY_UPDATED` audit; `bruiser policy validate` CLI.
- Anchors carried in customer assertions and sessions.
- Replace M1's hard-coded rule.

Exit criteria

- Table-driven tests cover every dimension and precedence combination.
- Changing `max_active` from 1 to 2 live results in a second grant for the next
  acquire without restart; existing executions keep their terms.
- A household-scoped rule blocks a second account in the same household, audited
  with `rule_name` and reason.

---

## M5 — Handoff and revoke (M)

**Goal:** the execution belongs to the customer, demonstrably.

Scope

- Cooperative handoff (`holder → named principal`) and preemptive handoff
  (`browser takes control from agent`) as one atomic store operation with `fence+1`
  and `successor_id`.
- Precedence evaluation; `can_preempt` in BUSY responses.
- Revoke by admin and by customer (subject to precedence); reasons; audit.
- Renew after handoff/revoke returns 410 with reason; watch streams the transition.
- SimTix rejects the preempted agent's in-flight request by fence; introspection
  returns `active: false` immediately.
- Torture harness extended with handoff/revoke operations; checker I2/I4 exercised.

Exit criteria

- Scripted scenario: agent holds → browser takes control → agent's next renew is
  410 → agent's stale purchase is rejected by SimTix → browser purchases. Runs in CI.
- Torture run with handoff enabled: zero violations.

---

## M6 — Demonstration (M)

**Goal:** the proposition is obvious within seconds to a non-engineer.

Scope

- `cmd/swarm` with behaviour profiles and `--bypass`; results JSON.
- Dashboard (served by gateway or a small separate binary): live counters via SSE;
  side-by-side "Without Bruiser / With Bruiser" panels; seats-per-customer
  histogram; downstream request counter; SimTix p99.
- `docker compose --profile demo up` one-liner; `make demo-1x10000`,
  `make demo-1000x10`, `make demo-handoff`.
- A 90-second scripted walkthrough (`docs/demo.md`) and a recorded video.
- Numbers asserted by an automated demo test so the demo cannot silently regress.

Exit criteria

- On a laptop: 1 × 10,000 completes with 1 GRANTED, 9,999 BUSY, SimTix sees one
  execution; without Bruiser SimTix receives 10,000 holds with visible churn.
- 1,000 × 10 completes with each customer holding ≤ 1 seat and downstream
  requests ≈ number of customers.
- Someone outside the team can run the demo from the README without help.

---

## M7 — Admin interface and observability (M)

**Goal:** an operator can see, control and explain what the gateway is doing.

Scope

- Admin UI (templates + htmx + SSE): status, active/waiting executions, per-customer
  view with `[Revoke]` / `[Take control]`, policy view/validate, audit search with
  "why?" view for a request.
- Admin auth: API keys with RBAC; separate listener.
- Prometheus metrics per technical design §12; OpenTelemetry traces; example
  Grafana dashboard; alert rules (store unavailable, grant latency, expiry spike).
- Audit export: JSONL file, webhook, OTel log exporter.

Exit criteria

- From the UI alone: find Alice, see her active execution, revoke it, and read the
  audit trail explaining a prior BUSY response with rule name and reason.
- Grafana dashboard renders all headline metrics during the demo.

---

## M8 — Hardening and V1 release (L)

**Goal:** an enterprise CTO can read the design, run the tests, deploy with Helm
and find no obvious gaps.

Scope

- Threat model (`docs/security/threat-model.md`); rate limits; body/deadline
  limits; token hygiene; secrets handling; distroless non-root images; image
  signing; SBOM.
- Real IdP support: OIDC/JWKS customer assertions; key rotation runbook.
- Helm chart with HPA, PDB, NetworkPolicy, ServiceMonitor, migration Job;
  zero-downtime upgrade test.
- Load test (k6) profiles and published numbers.
- Protocol v0.1 spec finalised in `docs/protocol/`; OpenAPI document; SDK docs.
- Operations docs: deployment, upgrade, backup/restore, incident runbooks.
- External security review scheduled; findings triaged.
- Versioning and release process; changelog; signed tags.

Exit criteria

- All torture, demo, integration and load suites green in CI.
- Fresh Kubernetes cluster → Helm install → demo passes, documented step by step.
- Security review has no open high/critical findings.
- V1.0.0 tagged; OSS repository published per licensing decision.

---

## Post-V1 tracks

**V1.5 (protocol and ecosystem)**

- Bounded waiting set (`waiting.mode: bounded`) with promotion and claim window;
  `EXECUTION_QUEUED` semantics.
- MCP server / tool definitions so agent frameworks acquire, renew, release and
  hand off natively; reference agent using it against SimTix.
- Agent-side SDK clients (not only merchant verification).
- Protocol conformance test suite that any "Bruiser-compatible gateway" can run.

**Core (commercial)**

- Admin SSO (OIDC), audit retention policies and export scheduling, analytics
  views, supported upgrade tooling, first real adapter (chosen with design partner).

**Enterprise**

- Multi-rule policy composition, time-windowed and tier-based rules, fair bounded
  queueing across principals of one customer, multi-region store strategy
  (documented patterns first; active-active later), compliance pack, premium
  adapters, agent identity/reputation inputs to policy.

---

## Testing strategy (cross-cutting)

| Layer | Tooling | Runs |
|-------|---------|------|
| Unit | `go test -race` | every PR |
| Property | `rapid` on state machine and policy evaluator | every PR |
| Store contract | real Postgres via `testcontainers-go` | every PR |
| API contract | OpenAPI-driven tests; SDK sample apps | every PR |
| Torture (short) | 3 in-process nodes, fixed seeds, all faults | every PR |
| Torture (long) | 5 containers, random seeds, ≥ 10⁶ ops | nightly |
| Demo assertion | Compose profile; headline numbers checked | nightly and before release |
| Load | k6 scenarios; published p50/p99 | before release |
| Security | `govulncheck`, container scanning, dependency review | every PR |

---

## Commercial track (in parallel with M2–M8)

The engineering plan is only half the plan. These actions do not depend on code
being finished, and several feed requirements back into M3/M4.

1. **Design-partner programme.** Target 3 clubs (Championship / League One clubs
   with in-house or flexible ticketing are the most likely to control their
   checkout). Offer: pilot on their staging environment, free during pilot, in
   exchange for requirements, a reference and first option on Core pricing.
2. **Checkout-ownership survey.** For each target club: who runs checkout, which
   platform, is there an API, can middleware be added? The answers decide the first
   real adapter and whether platform partnerships are needed before club sales
   (see risk R6).
3. **Identity survey.** What customer identifiers and anchors do clubs already have
   (membership numbers, supporter IDs, household records)? Feeds M4 anchor design.
4. **Demo assets** from M6: hosted read-only demo video; optionally a public sandbox
   at `demo.bruiser-gateway.com` (a hosted instance for sales only; does not change
   the self-hosted product model).
5. **Positioning content.** "One supporter remains one supporter" launch post; the
   protocol draft published for comment; a fairness/dispute-resolution note aimed
   at supporter-liaison and ticketing-office staff, not just CTOs.
6. **Licensing and trademark.** Decide OSS licence (Q3); file trademark for
   "Bruiser Gateway"; prepare Core licence terms and SLA template.
7. **Pricing validation.** Five conversations with ticketing-office and IT leads at
   target clubs validating the £20k / £100k tiers and what "supported" must mean.
8. **Security narrative.** Have the threat model and design reviewed by an external
   party early enough to cite in the first sales conversations.

---

## Risk register

| ID | Risk | Effect | Mitigation |
|----|------|--------|------------|
| R1 | Multi-accounting undermines the per-customer guarantee | thesis looks weak in diligence | State the boundary honestly; identity anchors (household, membership, payment); partner with clubs' membership systems |
| R2 | Merchant integration effort blocks adoption | no production deployments | Mode A middleware in three languages; Mode B proxy; "30-minute integration" guide; SimTix as reference |
| R3 | Gateway seen as SPOF for purchases | vendor/CTO rejection | Mode A keeps Bruiser out of data path; fail-closed semantics documented; HA Helm chart |
| R4 | Agents bypass the gateway | control is cosmetic | Enforcement is defined as the merchant requiring the token; first integration step is closing the unauthenticated path |
| R5 | Commoditisation ("just a Redis lock") | pricing pressure | Protocol, SDKs, handoff, audit, policy, ops maturity; sell the maintained standard |
| R6 | Ticketing platforms, not clubs, own checkout | club cannot deploy Mode A | Survey early; adapter strategy; platform partnerships; target clubs with own platforms first |
| R7 | Store design fails torture testing | schedule risk at M2 | Store interface isolates change; alternative coordination backends possible without API change |
| R8 | Over-engineering V1 | delay, dilution | Hard "not in V1" list; milestone exit criteria; M2 gate before demo work |
| R9 | Accessibility/regulatory (agents acting for supporters who need them) | reputational | Handoff preserves customer control; agents are first-class, never blocked as a class |
| R10 | Licence choice deters adoption or invites cloud-hosting competitors | strategic | Permissive licence for protocol/SDKs regardless; decide gateway licence with counsel (Q3) |

---

## Definition of done — V1

- The invariant holds under the torture suite across ≥ 3 gateways with all fault
  types, repeatedly.
- The 1 × 10,000, 1,000 × 10 and handoff demos run from one command and their
  numbers are asserted in CI.
- A merchant can enforce Bruiser with a middleware (Go, Node or Python) or via proxy.
- Policy is expressed in YAML, hot-reloaded, versioned and audited.
- Admin UI supports revoke, take-control and "why was this denied?".
- Metrics, traces, logs and audit export exist and are documented.
- Helm install on a fresh cluster passes the demo.
- Threat model written; external security review completed; no open high/critical.
- Protocol v0.1 and OpenAPI published; SDK docs published.
- Everything in `docs/` reflects what shipped.
