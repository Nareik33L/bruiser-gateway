# Hosting notes — Harchester United demo

**The demo is the Compose stack.** Club site, SimTix (origin + Go Edge), admin
console, load-lab, Postgres, and the **real** Bruiser gateway. That scope is
unchanged (founder direction: do not change it). This file is hosting notes
only. It is not a product redesign and not a Worker rewrite of the demo.

A `workers.dev` Worker is **not** a substitute for this environment. Do not
replace `demos/` or `deploy/compose/docker-compose.demo.yml` with an in-Worker
analogue.

## Intended hosted shape

One VM (or any host that can run Docker/Compose or `make demo-up-local`)
running the full demo. Cloudflare sits **in front of that origin**:

1. **DNS + proxy** (orange-cloud) for the three public hostnames:
   club, tickets (SimTix Edge), admin.
2. Origin certificate or Caddy ACME on the VM. Gateway and load-lab stay
   **unpublished** (compose network only).
3. **Optional Cloudflare Access** on the admin hostname, in addition to the
   console password.
4. Nightly `Reset demo` on the VM so the environment returns to Scenario 1.

Custom hostnames are a later decision (C12 in the scope). Nothing here
requires buying a domain today; local URLs remain `*:8100` / `:8091` / `:8110`.

## Stretch (not this milestone)

A Cloudflare Worker reference Edge in `deploy/edge/cloudflare-worker/` that
calls `POST /v1/authorize` and forwards to SimTix origin — the same contract
as the Go Edge. That is an M3/D6 stretch item. It does not replace the
gateway, Postgres, club, SimTix origin, admin, or load-lab.

## Local full fidelity (canonical)

```bash
make demo-up-local
# or
docker compose -f deploy/compose/docker-compose.demo.yml up --build
```

See [README-demo.md](../README-demo.md), [demo-runbook.md](demo-runbook.md),
and [demo-presenter-checklist.md](demo-presenter-checklist.md).
