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
# Dev customer assertion (HMAC JWT, membership number)
TOKEN=$(./bin/bruiser assertion 1001234)

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

Dev assertions default to membership `1001234` (Alice in the Arsenal-like lab):

```bash
./bin/bruiser assertion 1001234
```

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
| [docs/07-club-profile-arsenal.md](docs/07-club-profile-arsenal.md) | Unverified Arsenal-like standing analogue |
| [docs/08-make-authoritative.md](docs/08-make-authoritative.md) | Edge / Proxy / Embedded placement + Authority Check |
| [protocol/v0-draft.md](protocol/v0-draft.md) | Bruiser Protocol v0 draft |

## Licensing (intended, pending legal review)

Protocol, schemas, specifications and SDKs: Apache-2.0. Gateway core: BSL 1.1.
No licence files are committed until the OSS/Core boundary is formally decided.

## Status

M0–M2 are in this tree. M3 against the **unverified Arsenal-like** profile
covers Edge, Proxy, Go Embedded SDK, Authority Check, and EAF. Node/Python
SDKs remain. Stage A commercial discovery runs in parallel.

## Arsenal-like lab (best guess until discovery)

Identity is a 7-digit membership number. The box office is treated as a
platform origin Bruiser sits in front of (Edge), not club-owned checkout
(Embedded). Invented allocation routes: `POST /api/events/{event}/holds` and
`POST /api/orders`.

```bash
export BRUISER_DATABASE_URL=postgres://bruiser:bruiser@127.0.0.1:5432/bruiser?sslmode=disable
make serve            # :8080 control plane; BRUISER_PROXY_ADDR=:8081 for Proxy
make simtix           # :8090 origin (lockdown) + :8091 Edge analogue
make authority-check  # Overall Result PASS
make eaf-demo         # unaware swarm; observed EAF ~N×, downstream 1×
```

Two logins of membership `1001234` against `POST /api/events/ars-che/holds`
via the Edge: first hold is created, second session receives `409 BUSY`.
The same cookie is `ALREADY_HELD`. Direct origin holds without
`X-Bruiser-Origin-Secret` are `403`. With origin lockdown off, Authority
Check reports Overall Result FAIL and names the open path.
