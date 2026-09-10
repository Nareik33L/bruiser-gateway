# Fly.io — Harchester United Compose origin

Operator: Kieran logs into Fly himself. **No Fly tokens, no Cloudflare tokens,
and no production secrets in git or chat.**

This is packaging for the **existing** Compose demo. It does not replace
`demos/`, does not import `internal/` from demo apps, and does not slim the
stack to fit a free Worker.

Canonical local path remains `make demo-up-local` /
`deploy/compose/docker-compose.demo.yml`.

## Free-allowance blockers (read this first)

Do **not** shrink the demonstration to dodge billing. If Fly's free allowance
cannot run the full stack honestly, say so and pay for the Machine size, or
keep presenting from a laptop.

Fly's **legacy** free allowance (orgs on Hobby/Launch/Scale **before**
2024-10-07): **3 × shared-cpu-1x 256MB** Machines and **3GB** volume total.

New orgs: a **free trial** (about **2 VM hours** or 7 days; trial Machines
**auto-stop after ~5 minutes**). That is not an always-on demo host.

| Approach | Machines | Fits 3×256MB always-on? | Flagship 10k swarm? |
|----------|----------|-------------------------|---------------------|
| One Fly app per Compose service (postgres, gateway, harchester, simtix, loadlab, admin, caddy) | 6–7 | **No** (count) | No |
| Managed Fly Postgres + app | 2+ | Count maybe; **RAM no** | Unlikely on 256MB |
| **One packed Machine** (`deploy/fly`) + volume-backed Postgres | 1 | Count yes; **256MB RAM no** | Needs ~2GB |

**Blockers we will not paper over:**

1. **RAM.** Postgres 16 + gateway + five Go processes + Caddy idle is already
   above 256MB. The 10,000-agent load-lab (500-wide) will OOM a free VM. Honest
   size in `fly.toml`: **shared-cpu-2x / 2048MB**.
2. **Machine count.** `fly launch` from the Compose file (one app per service)
   exceeds three free VMs. Do not use that path on the free allowance.
3. **Always-on.** Trial auto-stop and 2 VM-hours cannot host a standing demo.
   Always-on requires a payment method after trial. Set
   `auto_stop_machines = false` and `min_machines_running = 1`.
4. **IPv4.** Dedicated IPv4 is extra (~$2/app). Cloudflare orange-cloud CNAME
   to `<app>.fly.dev` avoids a dedicated v4 on Fly.
5. **Volume.** Seeded `harchester` (10k supporters) plus Bruiser `sessions` /
   `audit_events` after swarms: start at **3GB** (legacy volume cap). Grow if
   audit fills the disk; do not delete `audit_events` (C10).
6. **Managed Fly Postgres** is a second Machine and usually wants ≥1GB. Prefer
   **volume-backed Postgres in the packed Machine** so the topology stays “one
   origin running Compose-equivalent processes”.

If the org still has the legacy 3×256MB allowance only, the full demo **does
not fit**. Keep scope; use the 2GB Machine (paid) or present locally.

## Preferred topology

```
supporter → Cloudflare (orange) → Fly HTTP :8080 (Caddy)
                                    ├ club.bruiser-gateway.com    → :8100 harchester
                                    ├ tickets.bruiser-gateway.com → :8091 simtix edge
                                    └ admin.bruiser-gateway.com   → :8110 admin
                                        (Access: kiedl33@outlook.com)
                              private on the Machine:
                                    postgres :5432 (volume)
                                    gateway :8080-internal (not published)
                                    simtix origin :8090
                                    load-lab :8120
```

One Fly app, one Machine, one volume. Same processes as Compose. Caddy is the
origin front-door; Cloudflare is DNS + TLS + Access, not the app.

Hostnames are env (`DEMO_*_HOST`) with production defaults locked to
`club` / `tickets` / `admin.bruiser-gateway.com`. Apex/`www` stay on Worker
`bruiser-gateway`.

## Deploy (Kieran, after `fly auth login`)

From the repo root. Do not paste tokens into Cursor.

```bash
fly auth login
fly apps create bruiser-harchester-demo   # once
fly volumes create harchester_pg --region lhr --size 3 -a bruiser-harchester-demo
# Optional: fly secrets set … (names in deploy/fly/secrets.example)
bash deploy/fly/deploy.sh
fly certs add club.bruiser-gateway.com
fly certs add tickets.bruiser-gateway.com
fly certs add admin.bruiser-gateway.com
```

Then Cloudflare DNS + Access per [09-harchester-cloudflare.md](09-harchester-cloudflare.md).

`fly.toml` pins **2GB RAM** on purpose. Do not edit it down to 256MB to “stay
free”.

Health: `https://<app>.fly.dev/healthz` should return `ok` before you orange-cloud
the CNAMEs.

## Equivalent: Compose on a single Fly Machine

Fly does not run `docker-compose.demo.yml` as an orchestrator (no Docker-in-Docker
on a standard Machine). The packed image is the equivalent: same binaries, same
three databases, same unpublished gateway/load-lab.

If you later run a full VM (not Fly Machines) that *can* run Compose, use
`docker compose -f deploy/compose/docker-compose.demo.yml --profile hostnames up`
and `deploy/caddy/Caddyfile.demo` (localhost + locked production server blocks).
Public ticket URL: `SIMTIX_PUBLIC_URL=https://tickets.bruiser-gateway.com`
(`deploy/compose/harchester.hosted.env.example`).

## Nightly reset

```bash
fly ssh console -a bruiser-harchester-demo -C 'curl -sS -X POST http://127.0.0.1:8110/reset -H "Cookie: admin_session=harchester-ok"'
```

Schedule that from a laptop cron or a Fly Machine cron once the origin is up.
Does not delete Bruiser audit rows.

## Out of scope here

- Deploying from this agent (no Fly token).
- Pointing `bruiser-gateway.com` / `www` at this app.
- A Worker rewrite, Durable Objects, or `workers.dev` stand-in.
