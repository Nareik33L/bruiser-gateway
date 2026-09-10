# Harchester United demonstration environment

**This is not production and not the Bruiser product.** It is a fictional club
and ticketing platform used to show Bruiser Gateway as a Premier League club
would deploy it.

> The supporter never knows Bruiser exists.

Full scope: [docs/08-harchester-demo-scope.md](docs/08-harchester-demo-scope.md).
Runbook: [docs/demo-runbook.md](docs/demo-runbook.md).

## DEMO ONLY — plaintext passwords

Supporter passwords in `demos/seed` are stored in plaintext so the agent
launcher can use deterministic credentials. The environment is fictional and
closed. **Never copy this schema into production.**

| Membership | Password | Notes |
|------------|----------|--------|
| 1001234 | password | Alice Okafor (Gold, eligible) |
| 1000002 | password | Sam Quinn (Junior, not eligible) |
| 1000001–1010000 | password | 10,000 fictional supporters |

## Quick start (local binaries)

Postgres must be running. Create databases `bruiser`, `harchester` (the
gateway also creates its own schema on serve).

```bash
make demo-up-local
```

Then:

- Club site: http://127.0.0.1:8100
- SimTix (edge): http://127.0.0.1:8091
- Admin console: http://127.0.0.1:8110 (password `harchester`)
- Gateway (no public story): http://127.0.0.1:8080

```bash
make demo-check          # Authority Check against the live Edge
make demo-reset          # seats, executions, swarm — not supporter rows
make demo-down-local
```

## Compose

```bash
docker compose -f deploy/compose/docker-compose.demo.yml up --build
```

The gateway has no published port in Compose; only SimTix and the console
reach it.

## Boundary

`demos/**` must not import `internal/**`. `make check-demo-boundary` (and `make ci`)
enforces that. Demo apps talk to Bruiser over HTTP only.
