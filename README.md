# Bruiser Gateway

A control layer between autonomous software agents and systems holding scarce
inventory. One customer may run many agents; Bruiser ensures only the
merchant-permitted number of authorised executions for that customer act on a
scarce resource at a time.

> One supporter remains one supporter, regardless of how many agents they deploy.

## Documents

| Document | Purpose |
|----------|---------|
| [docs/01-product-brief.md](docs/01-product-brief.md) | Product and technical brief (v2.1): vision, proposition, model, enforcement and authority, tiers and licensing, GTM, principles. Starts with what changed and why. |
| [docs/02-technical-design.md](docs/02-technical-design.md) | V1 design: stack, data model, storage and concurrency, identity and tokens, API, policy, adapters, enforcement patterns (P1/P2/P3) and the authority check, simulator, torture harness, observability, security, deployment, repository and licence layout. |
| [docs/03-execution-plan.md](docs/03-execution-plan.md) | Milestones M0–M8 with exit criteria, testing strategy, commercial track (starts now), risk register, V1 definition of done. |
| [docs/04-decisions.md](docs/04-decisions.md) | Architecture decision log. |
| [docs/05-decisions-from-founder-review.md](docs/05-decisions-from-founder-review.md) | Founder answers to the open questions, where each landed in the plan, and what remains open. |
| [docs/06-integration-discovery.md](docs/06-integration-discovery.md) | Questionnaire for design-partner discovery calls: find the admission point, pick the enforcement pattern, qualify the club. |

## Licensing (intended, pending legal review)

Protocol, schemas, specifications and SDKs: Apache-2.0. Gateway core: BSL 1.1.
No licence files are committed until the OSS/Core boundary is formally decided;
the repository is private until then.

## Status

Planning. No code yet. Engineering starts at M0 (foundations); the commercial
track (design-partner discovery) starts now.
