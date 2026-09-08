# Decisions from Founder Review

The open questions raised in the v2 brief were answered by the founder. This file
records each answer, the decision it became, and where the plan changed. Anything
still genuinely open is listed at the end.

| # | Topic | Founder decision | Where it landed |
|---|-------|------------------|-----------------|
| Q1 | Identity authority | Anchor to the club's existing authenticated supporter/customer identity. Membership/supporter IDs preferred; existing account login acceptable initially. No separate Bruiser identity system unless necessary. | Brief §4; design §5.1; ADR-015 |
| Q2 | Checkout ownership / integration | Mixed setups expected. Platform-agnostic; no dependency on any ticketing-platform partnership. First customers are clubs with a technically viable path to make Bruiser authoritative at the admission point: club-controlled checkout, a supported platform integration point, or a reverse-proxy/API-gateway arrangement. Bruiser sits in front of existing infrastructure, never replaces it. | Brief §9, §20, §22; design §9 (Embedded / Edge / Proxy); ADR-002; execution plan M3, commercial track Stage A, R6 |
| Q3 | Licensing | Protocol, schemas, specifications and SDKs: Apache-2.0. Gateway core: BSL 1.1, subject to legal review. Repository private until the OSS/Core boundary is formally decided. | Brief §10, §17; design §16; ADR-014; execution plan M0, M8 |
| Q4 | Team / language | 1–2 engineers comfortable in Go. Go preferred for the gateway; architecture must not become unnecessarily language-dependent. | Design rule 9 (protocol-first); ADR-008 unchanged |
| Q5 | Hosted sandbox | Yes — encouraged as a sales and developer-discovery asset. Production stays self-hosted. | Brief §11; design §15; ADR-016; execution plan M6, M8 |
| Q6 | Inter-customer fairness | Out of scope for V1 and V1.5. Not replacing waiting-room products. | Unchanged (brief §2, §8; ADR-006) |
| Q7 | Design partners | None confirmed. Immediate commercial priority alongside development; use club conversations to validate integration requirements. | Brief §20; execution plan commercial track (Stage A starts immediately, before significant engineering investment), R13; `06-integration-discovery.md` |
| Q8 | Agent ecosystem | Yes to MCP server / tool definitions and agent SDKs eventually, but enforcement must never depend on agents voluntarily using Bruiser tooling. | Brief §9.2, §10, philosophy 3; design §5.1a, §9; ADR-002a; V1.5 track |
| Q9 | Data residency / retention | Merchant self-hosting is the primary residency control. Retention configurable, default 13 months, subject to legal/security review and merchant requirements. | Brief §11, §16; design §13; ADR-018; execution plan M7 |
| Q10 | External security review | Yes; budgeted; part of M8 production-readiness criteria. | Execution plan M8 exit criteria |
| Q11 | Name | Keep "Bruiser" / "Bruiser Gateway". Trademark clearance before significant commercial investment or launch. | Execution plan commercial track item 5; M8 exit criteria |
| Q12 | Pricing | Core £20k/yr within a documented fair-use envelope (not unlimited). Enterprise from £100k+/yr. Keep pricing simple; no per-request metering initially. | Brief §17; design §13 (usage figures, informational only); ADR-017 |
| Q13 | Authority / bypass prevention | Bruiser must be authoritative over the scarce-inventory operation it protects. Direct paths that bypass Bruiser must be removed, restricted or otherwise prevented. Target architecture: client/agent → Bruiser → existing ticketing/commerce system → inventory. | Brief §9, §9.1 Authority Check, philosophy 2; design rule 7, §9.5; ADR-002; execution plan M3, M6 bypass demo, R4, R12 |
| — | Key product principle | Bruiser is not a distributed-lock product. Coordination technology is an implementation detail. The product is the identity, execution-control, lease, concurrency, policy, queueing, handoff, audit and merchant-integration layer around scarce-inventory transactions. Positioned as a control layer for autonomous commerce — not bot detection, DDoS, a ticketing platform or a locking service. | Brief §19 (binding messaging rule), philosophy 8; design rule 5; ADR-001, ADR-020; commercial track Stage A outreach copy |

## v2.2 founder additions (folded in)

| Addition | Where it landed |
|----------|-----------------|
| Authority is the requirement; deployment method is an implementation detail | Brief §9, philosophy 2; ADR-002 |
| Rename P1/P2/P3 → Embedded / Edge / Proxy | All docs; they are deployment methods, not products |
| Bruiser Authority Check as named product feature and go-live gate | Brief §9.1; design §9.5; M3/M6/M7; demo output format |
| Execution Amplification Factor as primary KPI | Brief §15.1; design §13; ADR-019; M3/M6/M7 |
| Transparent enforcement as named principle; two client classes | Brief §9.2, philosophy 3; ADR-002a |
| Binding commercial messaging rule | Brief §19; ADR-020 |
| Stage A starts immediately, before significant engineering | Brief §20; execution plan commercial track; `06-integration-discovery.md` |

## v2.3 — heartbeat renewal and recovery

| Addition | Where it landed |
|----------|-----------------|
| Heartbeat renewal every 20–30 s (default 25 s); missed heartbeat expires at TTL (default 60 s) | Brief §5; design §4.3; ADR-023; protocol RENEW/HEARTBEAT; `BRUISER_HEARTBEAT_INTERVAL` |
| Recovery: reconnect before expiry resumes the same execution; after expiry a new execution may be acquired | Brief §5, §24; design §6; M1 exit criteria; store resume path |

## Still open (do not block engineering)

| Item | Owner | Needed by |
|------|-------|-----------|
| BSL 1.1 parameters (Additional Use Grant wording, Change Date) confirmed by counsel; OSS/Core boundary signed off | Founder + counsel | M8 publication |
| Fair-use envelope numbers (peak concurrent executions, events/year, deployments) | Founder, with first design partners | Core licence template, M8 |
| Trademark search result for "Bruiser Gateway" | Founder + counsel | Before hosted demo goes public (M6) and before launch |
| Which three clubs become design partners, and which deployment method each needs | Founder (commercial track Stage A, starts immediately) | Shapes M3 scope; ideally before M3 starts |
| Anchor set per design partner (membership number, household, payment fingerprint availability) | Discovery calls | M4 |
