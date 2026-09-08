# Bruiser Gateway — Execution Plan

This plan sequences the work as milestones with verifiable exit criteria rather
than dates. Each milestone is sized by what changes and how invasive it is, so
progress can be judged by what is demonstrably true, not by elapsed time.

Assumed team: one or two engineers plus the founder on the commercial track. Every
milestone ends with something runnable and a tagged release. **Stage A commercial
discovery starts immediately**, before significant engineering investment; it is a
core product activity, not a post-MVP sales exercise.

Effort key — **S**: a few focused sessions, one subsystem. **M**: several
subsystems, meaningful test surface. **L**: cross-cutting, needs its own test
harness or external dependency.

---

## Milestone map

```
M0 Foundations
 └─ M1 The primitive (single node)
     └─ M2 Distributed correctness  ◄── the gate that proves the thesis
         ├─ M3 Merchant integration & authority (Embedded / Edge / Proxy, transparent enforcement, Authority Check, SimTix)
         ├─ M4 Policy engine
         └─ M5 Handoff & revoke
             └─ M6 Demo
                 └─ M7 Admin & observability
                     └─ M8 Hardening & V1 release
                         └─ Post-V1: Core / Enterprise / protocol
```

M3, M4 and M5 are independent of each other once M2 lands and can run in parallel
if there are two engineers. M3 is the largest of the three and should start first;
its Edge/Proxy sub-tracks are the ones most likely to be reshaped by design-partner
discovery. Stage A commercial discovery starts immediately, in parallel with M0,
and is a core product activity — not a post-MVP sales exercise.

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
- `protocol/` directory established as the Apache-2.0 boundary (OpenAPI skeleton,
  JSON Schema stubs, `v0-draft.md`); ADR log started (`04-decisions.md`).
- Repository laid out for the Apache-2.0 / BSL 1.1 split (technical design §16).
  Licence files are drafted but not committed until legal review signs off; the
  repository stays private.

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
- Idempotent re-acquire by the same principal (ALREADY_HELD); BUSY response shape.
- Heartbeat renewal: `POST .../renew` and `POST .../heartbeat` extend TTL;
  `heartbeat_after_ms` on ACTIVE responses (default interval 20 s, lease TTL 60 s).
- Recovery: reconnect before expiry resumes the same execution (TTL extended,
  session rebound); after expiry a new execution may be acquired.
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
- Recovery: disconnect and reconnect before expiry returns the same execution id
  with a later `expires_at`; after expiry a new GRANT is allowed.
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
- Invariant checker I1–I7 (technical design §12) with minimal-history dump on failure.
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

## M3 — Merchant integration and authority (L)

**Goal:** a merchant can make Bruiser authoritative at their admission point via
any of the three deployment methods (Embedded, Edge, Proxy), enforcement works for
clients that have never heard of Bruiser, the Authority Check proves it, and EAF
is measurable.

This milestone grew from M to L after the founder decisions on authority (Q13) and
platform-agnostic integration (Q2). It is the milestone most shaped by design-partner
conversations; the integration-discovery questionnaire
([06-integration-discovery.md](06-integration-discovery.md)) feeds it. Stage A
discovery should already be underway before this milestone starts.

**Standing analogue until discovery.** Engineering does not wait on a club
call. The lab merchant is an unverified Arsenal-like profile
([07-club-profile-arsenal.md](07-club-profile-arsenal.md), ADR-022): platform
box office, membership-number identity, Edge first, Embedded blocked. Invented
routes and SimTix are labelled as guesses and are replaced when a questionnaire
comes back.

Scope

- **Embedded — in-application middleware.** Token verification rules and
  `POST /v1/introspect`. `sdk/go` (`net/http`), `sdk/node` (Express/Fastify),
  `sdk/python` (ASGI). Each: JWKS fetch/cache, offline verification, fence tracking
  hook, optional introspection, optional per-execution local limits, one-line install.
- **Edge — API gateway / WAF / Worker.** Route rules (path/method → resource/action);
  `POST /v1/authorize`; reference configs for NGINX `auth_request`, Envoy
  `ext_authz`, Kong plugin, Cloudflare Worker, AWS API Gateway authorizer; edge→origin
  secret/mTLS guidance.
- **Proxy — reverse proxy.** `internal/proxy` transparent HTTP proxy applying route
  rules; `internal/adapter` interface, `adapter/memory`, `adapter/simtix`; proxy
  endpoints with fence attached.
- **Transparent enforcement.** Two client classes: Bruiser-aware (explicit APIs)
  and Bruiser-unaware (merchant session, automatic acquire, same rules). Customer
  extractors (JWT, cookie-JWT, introspection, edge-signed header); implicit sessions;
  per-execution in-flight and rate limits.
- **Bruiser Authority Check.** `bruiser authority-check`: route enumeration,
  no-token / expired / tampered / stale-fence probes, common-bypass probes, the
  PASS/FAIL report in the product-feature format (browser / mobile / agent routes,
  expired, tampered, direct bypass), `AUTHORITY_CHECK` audit event. A deployment
  that cannot achieve PASS is not production-ready.
- **EAF instrumentation.** Counters for incoming allocation attempts and authorised
  executions forwarded; `observed_eaf` and `downstream_eaf` gauges.
- **SimTix** (`cmd/simtix`): events, seats, price bands, holds with TTL, purchase,
  cancel, non-atomic per-account limit (toggle), club-style login issuing a session
  JWT, `--enforcement embedded|edge|proxy`, `--origin-lockdown`, request log,
  metrics, reset.
- Integration guide per deployment method: "Make Bruiser authoritative in 30
  minutes", with the Authority Check as the final step. Onboarding checklist for
  design partners.

Exit criteria

- SimTix under Embedded rejects a hold with an expired, tampered or stale-fence
  token and accepts a valid one; the same scenario under Edge (NGINX) and Proxy
  has the identical downstream effect. All three in CI.
- An `unaware` swarm profile (merchant session only, no Bruiser protocol) is
  controlled under Edge and Proxy: one execution per customer, second agent BUSY.
- Authority Check against SimTix with `--origin-lockdown` reports Overall Result
  PASS (including "Direct allocation bypass blocked"); with lockdown off it
  reports FAIL and names the open path. Both in CI.
- 1 customer × 10,000 unaware/aware agents against SimTix reports observed EAF
  ≈ 10,000× and downstream EAF = 1×.
- A Node and a Python sample app verify tokens using the SDKs, in CI.

**Landed against the Arsenal-like analogue (this tree).** `POST /v1/authorize`
and `POST /v1/introspect`; `configs/arsenal.yaml` route rules; SimTix origin
with optional origin lockdown and optional Embedded token verify; Go Edge
analogue plus NGINX `auth_request` and Envoy `ext_authz` references; **Proxy**
on `BRUISER_PROXY_ADDR`; `bruiser authority-check` (Edge and Proxy); unaware
BUSY vs ALREADY_HELD; EAF counters plus `bruiser eaf-demo`; `sdk/go` Protect
with fence tracking; `sdk/node` and `sdk/python` (EdDSA + fence) in CI;
`AUTHORITY_CHECK` persisted on `bruiser authority-check` and
`POST /v1/authority-check`; 1×10,000 as `make eaf-nightly` / nightly workflow
(PR CI remains 200× on Proxy; CLI default 2,000).

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

**Landed in this tree.** First-match YAML compiler (`internal/policy`); scope
dimensions `customer`, `resource`, `resource_pool`, `principal_type`,
`anchor:*`; `on_missing_anchor` deny/fallthrough; `control: none`; Postgres
versioned policies; `GET/PUT /v1/policy` (admin/edge secret); live `max_active`
change without restart; household-cap BUSY with `rule_name`;
`bruiser policy validate`; `protocol/policy.schema.json`. Precedence lists on
the matched rule replace the ADR-024 hard-code when present. `NOTIFY
bruiser_policy` plus a one-second poll reloads other nodes and resets their
busy cache (ADR-026).

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

**Landed in this tree (store + public API + SimTix scenario).** Cooperative and
preemptive handoff as one transaction (`HANDED_OFF` + successor `fence+1`);
default precedence browser > agent (ADR-024); `can_preempt` on BUSY; customer
revoke subject to precedence; renew after handoff is 410 with `successor_id`;
introspect returns `active: false`; SimTix rejects a stale fence after the
successor token is seen. CI scenario: agent holds → browser takes control →
agent renew 410 → stale purchase 403 → browser purchases. Torture adds
`TestHandoffRevokeInvariants` (I2 fence rise, I4 no zombie renew, still one
ACTIVE, revoke then zero ACTIVE).

---

## M6 — Demonstration (M)

**Goal:** the proposition is obvious within seconds to a non-engineer.

Scope

- `cmd/swarm` with behaviour profiles and `--bypass`; results JSON.
- Dashboard (served by gateway or a small separate binary): live counters via SSE;
  side-by-side "Without Bruiser / With Bruiser" panels; **EAF as the headline
  number** (observed vs downstream); seats-per-customer histogram; downstream
  request counter; SimTix p99; live Authority Check result in the product-feature
  format.
- `docker compose --profile demo up` one-liner; `make demo-1x10000`,
  `make demo-1000x10`, `make demo-handoff`, `make demo-bypass`, `make demo-unaware`.
- A 90-second scripted walkthrough (`docs/demo.md`) and a recorded video. Script
  leads with EAF moving from 10,000× to 1× downstream, then Authority Check PASS.
- Numbers asserted by an automated demo test so the demo cannot silently regress.
- First cut of the **hosted demo**: the Compose demo deployed read-only at
  `sandbox.bruiser-gateway.com` with the live dashboard and a "run scenario" button
  (self-service developer merchants arrive at M8; see ADR-016).

Exit criteria

- On a laptop: 1 × 10,000 completes with 1 GRANTED, 9,999 BUSY, observed EAF ≈
  10,000×, downstream EAF = 1×, SimTix sees one execution; without Bruiser SimTix
  receives 10,000 holds with visible churn.
- 1,000 × 10 completes with each customer holding ≤ 1 seat and downstream
  requests ≈ number of customers.
- Bypass scenario: with lockdown off an agent going straight to SimTix succeeds and
  the Authority Check reports Overall Result FAIL naming the open path; with
  lockdown on the request is refused and the check reports PASS including
  "Direct allocation bypass blocked". Unaware scenario: protocol-ignorant agents
  are controlled under the same EAF.
- Someone outside the team can run the demo from the README without help, and a
  prospect can watch it run at the hosted URL.

**Landed in this tree (lab demo, not hosted sandbox).** `cmd/swarm` and
`bruiser swarm` profiles `1xN` / `NxK` / `unaware` / `bypass` / `handoff`;
`GET /admin` dashboard with SSE EAF headline and last Authority Check;
`make demo-*` plus `make demo-up` (Compose `--profile demo` with Grafana);
`docs/demo.md`; `TestDemoAssertion` so the demo cannot silently regress.
Hosted `sandbox.bruiser-gateway.com` remains M8.

---

## M7 — Admin interface and observability (M)

**Goal:** an operator can see, control and explain what the gateway is doing.

Scope

- Admin UI (templates + htmx + SSE): **EAF as the headline KPI** (observed and
  downstream, per merchant / event / customer, selectable window); status,
  active/waiting executions, per-customer view with `[Revoke]` / `[Take control]`,
  policy view/validate, audit search with "why?" view for a request.
- Admin auth: API keys with RBAC; separate listener.
- Prometheus metrics per technical design §13; OpenTelemetry traces; example
  Grafana dashboard led by EAF; alert rules (store unavailable, grant latency,
  expiry spike, downstream EAF departing 1×).
- Audit export: JSONL file, webhook, OTel log exporter. Audit retention
  (default 13 months) with purge/archive job and `AUDIT_PURGED` events.
- Usage figures panel (peak concurrent executions, executions/month, resources
  protected, peak EAF) — informational only, for fair-use transparency.
- Authority Check last-run result in the product-feature format, with `[Run check]`.

Exit criteria

- From the UI alone: see merchant EAF; find Alice, see her per-customer EAF and
  active execution, revoke it, and read the audit trail explaining a prior BUSY
  response with rule name and reason; run the Authority Check and read PASS.
- Grafana dashboard renders all headline metrics during the demo, EAF first.
- Retention job purges (or archives) events older than the configured window in a
  test with a shortened window; the purge is itself audited.

**Landed in this tree (operator surface).** `GET /admin` (htmx-style page +
SSE); EAF headline, usage figures, active executions with admin revoke, audit
search and JSONL export; last Authority Check plus `[Run check]`;
`BRUISER_AUDIT_RETENTION` default 13 months with `AUDIT_PURGED`; Grafana
dashboard provisioned under the demo Compose profile (Prometheus scrape of
`/metrics`). V1 admin auth is the admin/edge secret (`BRUISER_ADMIN_SECRET`),
not a full API-key RBAC table. OpenTelemetry traces remain a follow-on; the
scrape path is Prometheus.

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
- Protocol v0.1 spec finalised in `protocol/`; OpenAPI document; JSON Schemas;
  token spec; audit vocabulary; SDK docs.
- Operations docs: deployment, upgrade, backup/restore, incident runbooks, and the
  authority-check runbook for go-live.
- **Independent external security review** (budgeted, founder decision Q10);
  findings triaged; no open high/critical before release.
- **Licensing execution:** legal review of BSL 1.1 parameters; LICENSE files
  committed; `protocol/`, `sdk/`, `deploy/edge/` published to a public
  `bruiser-protocol` repository under Apache-2.0; gateway repository published
  under BSL 1.1 once the OSS/Core boundary is formally signed off.
- **Hosted sandbox** completed: self-service throwaway merchants, hosted docs,
  abuse limits, nightly reset (ADR-016).
- Versioning and release process; changelog; signed tags.

Exit criteria

- All torture, demo, integration, authority and load suites green in CI.
- Fresh Kubernetes cluster → Helm install → demo passes, documented step by step.
- Security review has no open high/critical findings.
- Trademark clearance for "Bruiser Gateway" complete (blocks public launch, not the
  tag).
- V1.0.0 tagged; protocol repository public under Apache-2.0; gateway published
  under BSL 1.1 per the formal boundary decision.

---

## Post-V1 tracks

**V1.5 (protocol and ecosystem)**

- Bounded waiting set (`waiting.mode: bounded`) with promotion and claim window;
  `EXECUTION_QUEUED` semantics. **Landed in this tree:** QUEUED + promote +
  leave + waiter expiry; MCP `protocol/mcp/tools.json`; Go/Node/Python agent
  clients. Claim-window tuning and a full MCP *server* remain follow-on.
- MCP server / tool definitions so agent frameworks acquire, renew, release and
  hand off natively; reference agent using it against SimTix. These improve the
  agent's experience (watch instead of retry, clean handoff); enforcement never
  depends on them (ADR-002a).
- Agent-side SDK clients (not only merchant verification).
- Protocol conformance test suite that any "Bruiser-compatible gateway" can run.
- Additional customer extractors and edge references driven by design-partner
  environments.

**Core (commercial)**

- Admin SSO (OIDC), audit export scheduling and archive targets, analytics views,
  supported upgrade tooling, first real adapter or platform-hook variant (chosen
  with the first design partner).

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
| Enforcement parity | same scenarios under Embedded, Edge (NGINX) and Proxy with identical downstream effect | every PR |
| Authority | `authority-check` against SimTix with lockdown on (Overall PASS) and off (FAIL, named path) | every PR |
| Demo assertion | Compose profile; headline numbers checked | nightly and before release |
| Load | k6 scenarios; published p50/p99 | before release |
| Security | `govulncheck`, container scanning, dependency review | every PR |

---

## Commercial track (starts immediately, in parallel with M0–M8)

Stage A commercial discovery starts **before significant engineering investment**.
It is a core product activity, not a post-MVP sales exercise: the conversations
validate integration requirements that shape M3, and M3 is on the critical path.
There are no confirmed design partners. The track is sequenced by which
engineering artefact each stage needs.

**Stage A — starts immediately; needs only these documents**

Objectives, against a list of 20–30 football clubs:

- identify the ticketing platform
- determine the viable deployment method (Embedded / Edge / Proxy)
- confirm the identity source
- confirm that authority can be achieved
- qualify or disqualify the opportunity
- secure 2–3 design partners

1. **Target list.** 20–30 clubs across the Premier League, Championship and League
   One. For each, from public information: ticketing platform, whether checkout is
   club-run or platform-run, presence of a CDN/WAF/API gateway, membership scheme,
   supporter-ID scheme, recent fairness incidents, ticketing-office and
   IT/digital contacts.
2. **Outreach.** Two entry points per club: ticketing/supporter-services (pain
   owner) and head of digital/IT (integration owner). Message is the proposition,
   never the deployment method or the coordination technology:

   > The authoritative control layer for autonomous commerce that ensures one
   > customer remains one customer, regardless of how many agents they deploy.

3. **Discovery calls** run against
   [06-integration-discovery.md](06-integration-discovery.md). Output per club:
   admission-point map, viable deployment method (Embedded / Edge / Proxy / none),
   identity source, whether Authority Check PASS is achievable, decision-maker,
   blockers. Clubs with no viable method are deferred with their requirements
   recorded — not force-fitted.
4. **Design-partner offer.** Target 2–3 signed partners. Terms: pilot on the club's
   staging environment, free during pilot, joint Authority Check sign-off as the
   go-live gate, in exchange for requirements access, a reference and first option
   on Core pricing. Prefer a spread of deployment methods across the partners so
   V1 proves all of them.
5. **Legal and brand.** Instruct counsel on BSL 1.1 parameters and the OSS/Core
   boundary; trademark search and filing for "Bruiser Gateway" (must clear before
   public launch or significant marketing spend); draft Core licence and SLA
   templates; fair-use envelope wording.

**Stage B — needs the M2 proof (torture results) and design (M2–M5)**

6. **Technical validation with partner engineers.** Walk their engineers through the
   design, the invariant, the deployment method chosen for them, EAF and the
   Authority Check. Their objections become M3 scope; their environment becomes an
   enforcement-parity test case.
7. **Pricing validation.** Five conversations with ticketing-office and IT leads
   validating £20k / £100k+, the fair-use envelope dimensions, and what "supported"
   must mean.
8. **Security narrative.** External review of the threat model and design early
   enough to cite in sales conversations; the full independent review lands at M8.

**Stage C — needs the demo (M6 onwards)**

9. **Demo assets.** Hosted demo at `sandbox.bruiser-gateway.com`; 90-second video
   led by EAF and Authority Check PASS; live demo in every prospect meeting.
10. **Positioning content.** Launch post; protocol draft published for comment on the
    public protocol repository; a fairness/dispute-resolution note for
    supporter-liaison and ticketing-office staff. Messaging rule (ADR-020) is
    binding: never Redis lock, bot detection, DDoS, ticketing platform,
    middleware, proxy or API gateway; always the authoritative control layer.
11. **Pilot go-lives.** Staging pilots with each partner using the onboarding
    checklist; Authority Check PASS is the go/no-go; convert to Core at V1.

**Stage D — from V1**

12. Reference customers, case studies with agreed metrics (EAF, duplicate
    allocations prevented, disputes resolved from audit), and the first
    Enterprise conversations.

---

## Risk register

| ID | Risk | Effect | Mitigation |
|----|------|--------|------------|
| R1 | Multi-accounting undermines the per-customer guarantee | thesis looks weak in diligence | State the boundary honestly; identity anchors (household, membership, payment); partner with clubs' membership systems |
| R2 | Merchant integration effort blocks adoption | no production deployments | Three deployment methods (Embedded / Edge / Proxy); SDKs in three languages; reference edge configs; "30-minute" guide per method; SimTix as reference; Stage A discovery shapes M3 |
| R3 | Gateway seen as SPOF for purchases | vendor/CTO rejection | Embedded preferred where possible (out of data path); fail-closed semantics documented; HA Helm chart; Proxy defaults assume in-path HA |
| R4 | Agents or clients bypass the gateway | control is cosmetic | Authority is the requirement (ADR-002); Authority Check is a named product feature and go-live gate; transparent enforcement covers unaware clients; origin lockdown guidance |
| R5 | Commoditisation ("just a lock") | pricing pressure | Messaging rule (ADR-020); protocol, SDKs, deployment methods, Authority Check, EAF, handoff, audit, policy, ops maturity; sell the maintained standard |
| R6 | Ticketing platforms, not clubs, own checkout | Embedded unavailable at many clubs | Platform-agnostic by design; Edge where the club controls the edge/WAF/worker, Proxy where it controls routing; qualify clubs by viable method; defer the rest with requirements recorded; no dependency on platform partnerships |
| R7 | Store design fails torture testing | schedule risk at M2 | Store interface isolates change; alternative coordination backends possible without API change |
| R8 | Over-engineering V1 | delay, dilution | Hard "not in V1" list; milestone exit criteria; M2 gate before demo work |
| R9 | Accessibility/regulatory (agents acting for supporters who need them) | reputational | Handoff preserves customer control; agents are first-class, never blocked as a class |
| R10 | Licence execution stalls (legal review, boundary decision) | public launch delayed | Layout supports the split from M0; Apache-2.0 protocol repository can publish independently of the gateway decision; tag V1 privately if needed |
| R11 | Transparent enforcement misidentifies customers (extractor misconfiguration) | wrong customer denied or two customers merged | Extractor validation tooling; staging pilot with real sessions; authority/extractor checks in onboarding; audit shows extracted identity per decision |
| R12 | Authority check gives false confidence about unknown paths | bypass in production | Report states coverage explicitly; questionnaire forces path enumeration; periodic re-runs; partner engineers sign off the route list |
| R13 | No design partner signs | V1 built without validation | Stage A starts immediately, before significant engineering investment; 20–30 club list; qualify by viable deployment method; broaden to non-football scarce-inventory merchants with the same method if football stalls |

---

## Definition of done — V1

- The invariant holds under the torture suite across ≥ 3 gateways with all fault
  types, repeatedly.
- The 1 × 10,000, 1,000 × 10, handoff, bypass and unaware-agent demos run from one
  command and their numbers are asserted in CI.
- A merchant can make Bruiser authoritative via Embedded (Go, Node or Python
  middleware), Edge (`/v1/authorize` with reference configs) or Proxy, with
  identical downstream effect, and the Authority Check reports Overall Result PASS
  against their environment. A club that cannot achieve PASS is not production-ready.
- Clients that do not speak the protocol are controlled via transparent enforcement.
- Observed EAF and downstream EAF are the headline KPI in the admin UI and demo.
- Policy is expressed in YAML, hot-reloaded, versioned and audited.
- Admin UI supports revoke, take-control, "why was this denied?", EAF, usage
  figures and Authority Check status.
- Metrics, traces, logs, audit export and audit retention exist and are documented.
- Helm install on a fresh cluster passes the demo; hosted sandbox is live.
- Threat model written; independent security review completed; no open high/critical.
- Protocol v0.1, OpenAPI, schemas and SDKs published under Apache-2.0; gateway
  licensed under BSL 1.1 per the signed-off boundary; trademark cleared.
- Everything in `docs/` reflects what shipped.
