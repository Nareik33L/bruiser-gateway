# Bruiser Gateway

A control layer between autonomous software agents and systems holding scarce
inventory. One customer may run many agents; Bruiser ensures only the
merchant-permitted number of authorised executions for that customer act on a
scarce resource at a time.

> The authoritative control layer for autonomous commerce that ensures one
> customer remains one customer, regardless of how many agents they deploy.

## Quick start

```bash
# Postgres must be reachable (docker compose or local).
export BRUISER_DATABASE_URL=postgres://bruiser:bruiser@127.0.0.1:5432/bruiser?sslmode=disable
export BRUISER_DEV_HMAC_SECRET=dev-secret-change-me
make serve
```

Or: `docker compose -f deploy/compose/docker-compose.yml up --build`

```bash
# Dev customer assertion (HMAC JWT)
TOKEN=$(./bin/bruiser assertion cust_alice)

# Session for an agent
SESSION=$(curl -sS -X POST localhost:8080/v1/sessions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"principal":{"type":"agent","id":"shopping-agent-1"}}' | jq -r .session_token)

# Acquire an execution
curl -sS -X POST localhost:8080/v1/executions/acquire \
  -H "Authorization: Bearer $SESSION" \
  -H "Content-Type: application/json" \
  -d '{"resource":"event:ars-che","action":"purchase"}'
```

A second agent for the same customer and resource receives `409 BUSY`.

`GET /healthz` liveness, `GET /readyz` store readiness, `GET /metrics` Prometheus.

## Documents

| Document | Purpose |
|----------|---------|
| [docs/01-product-brief.md](docs/01-product-brief.md) | Product brief (v2.2) |
| [docs/02-technical-design.md](docs/02-technical-design.md) | V1 design |
| [docs/03-execution-plan.md](docs/03-execution-plan.md) | Milestones M0–M8 |
| [docs/04-decisions.md](docs/04-decisions.md) | Architecture decision log |
| [docs/05-decisions-from-founder-review.md](docs/05-decisions-from-founder-review.md) | Founder decisions |
| [docs/06-integration-discovery.md](docs/06-integration-discovery.md) | Stage A discovery questionnaire |
| [protocol/v0-draft.md](protocol/v0-draft.md) | Bruiser Protocol v0 draft |

## Licensing (intended, pending legal review)

Protocol, schemas, specifications and SDKs: Apache-2.0. Gateway core: BSL 1.1.
No licence files are committed until the OSS/Core boundary is formally decided.

## Status

M0 foundations and M1 execution-lease primitive are in this tree. Distributed
correctness (M2), deployment methods and the Authority Check (M3) come next.
Stage A commercial discovery runs in parallel.
