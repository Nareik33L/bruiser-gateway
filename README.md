# Bruiser Gateway

A control layer between autonomous software agents and systems holding scarce
inventory. One customer may run many agents; Bruiser ensures only the
merchant-permitted number of authorised executions for that customer act on a
scarce resource at a time.

> One supporter remains one supporter, regardless of how many agents they deploy.

## Documents

| Document | Purpose |
|----------|---------|
| [docs/01-product-brief.md](docs/01-product-brief.md) | Product and technical brief (v2.2): vision, proposition, model, authority and deployment methods (Embedded / Edge / Proxy), Authority Check, EAF, tiers and licensing, GTM, product philosophy. Starts with what changed and why. |
| [docs/02-technical-design.md](docs/02-technical-design.md) | V1 design: stack, data model, storage and concurrency, identity and tokens, API, policy, adapters, deployment methods and the Authority Check, simulator, torture harness, observability (incl. EAF), security, deployment, repository and licence layout. |
| [docs/03-execution-plan.md](docs/03-execution-plan.md) | Milestones M0–M8 with exit criteria, testing strategy, commercial track (Stage A starts immediately), risk register, V1 definition of done. |
| [docs/04-decisions.md](docs/04-decisions.md) | Architecture decision log. |
| [docs/05-decisions-from-founder-review.md](docs/05-decisions-from-founder-review.md) | Founder answers to the open questions, where each landed in the plan, and what remains open. |
| [docs/06-integration-discovery.md](docs/06-integration-discovery.md) | Questionnaire for Stage A discovery calls: find the admission point, pick Embedded / Edge / Proxy, qualify the club. |

## Licensing (intended, pending legal review)

Protocol, schemas, specifications and SDKs: Apache-2.0. Gateway core: BSL 1.1.
No licence files are committed until the OSS/Core boundary is formally decided;
the repository is private until then.

## Status

Planning. No code yet. Engineering starts at M0 (foundations). **Stage A
commercial discovery starts immediately**, before significant engineering
investment, using [docs/06-integration-discovery.md](docs/06-integration-discovery.md).
