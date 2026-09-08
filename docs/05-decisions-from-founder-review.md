# Open Questions for the Founder

The plan proceeds on the stated assumptions. Answers change scope or ordering as
noted; none of them blocks M0–M2.

| # | Question | Current assumption | What changes if the answer differs |
|---|----------|--------------------|------------------------------------|
| Q1 | **Who is the identity authority at target clubs?** Do they have supporter/membership IDs and SSO we can anchor on, or only email logins? | Clubs have a membership/supporter number and a login; we consume a signed assertion from their IdP. | Weak identity → identity anchors (M4) move earlier and become a headline feature; sales messaging leans harder on "levers, not magic". |
| Q2 | **Do target clubs control their own checkout, or does a ticketing platform (Ticketmaster, SeatGeek, SecuTix, Future Ticketing, Tixngo…) own it?** Which platforms? | Mixed; at least some design partners can add middleware to their checkout. | Platform-owned checkout → Mode B proxy rises in priority and a platform partnership/adapter must precede club sales (risk R6). |
| Q3 | **Licensing.** Protocol and SDKs permissive (Apache-2.0) is recommended regardless. For the gateway core: Apache-2.0 (maximum adoption), BSL 1.1 (blocks hosted competitors, converts to open source later) or AGPL? | Undecided; repo stays private until decided. | Decides what is published at M8 and how the OSS/Core boundary is drawn. |
| Q4 | **Team.** Who builds this and in what language are they strongest? | 1–2 engineers comfortable in Go. | A TypeScript-only team would justify revisiting ADR-008 before M1. |
| Q5 | **Is a hosted public sandbox acceptable for sales** (`demo.bruiser-gateway.com`), given the product is self-hosted? | Yes, as a sales asset only. | If not, the demo is video + local Compose only. |
| Q6 | **Inter-customer fairness.** Is it firmly out of scope (Bruiser sits behind the club's waiting room), or is replacing Queue-it-style products a future ambition? | Out of scope for V1 and V1.5. | If in scope long-term, the V1.5 bounded waiting design should be built to generalise. |
| Q7 | **Design partners.** Are any clubs already in conversation? Tier? | None yet. | An existing partner sets the first real adapter and pulls M3 details forward. |
| Q8 | **Agent-side ecosystem.** Do we want to publish an MCP server / tool definitions so agent frameworks speak Bruiser natively, and if so how early? | Yes, at V1.5. | Earlier → adds an agent-side SDK to M3. |
| Q9 | **Data residency and retention.** UK GDPR posture, audit retention defaults, any club requiring EU/UK-only processing? | Self-hosted by the merchant solves residency; retention configurable, default 13 months. | Specific requirements feed the M8 compliance notes and Core audit features. |
| Q10 | **Budget for an external security review** before the first production deployment? | Yes, scheduled at M8. | If not, M8 exit criteria weaken and the sales narrative should not claim external review. |
| Q11 | **Name.** Is "Bruiser" final? It reads as aggressive for a fairness product aimed at supporter trust. | Final for engineering; naming is a brand decision. | Rename is cheap before M8, expensive after. |
| Q12 | **Pricing units.** Is Core a flat £20k regardless of scale, or does it have a ceiling (e.g. customers/events per year) above which Enterprise applies? | Flat within a documented fair-use envelope. | Affects what the gateway must meter and report (Core analytics). |
