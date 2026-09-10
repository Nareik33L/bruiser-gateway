# Hosting notes — Harchester United demo

**The demo is the Compose stack.** Club site, SimTix (origin + Go Edge), admin
console, load-lab, Postgres, and the **real** Bruiser gateway. That scope is
unchanged. This file is hosting notes only. It is not a product redesign and
not a Worker rewrite of the demo.

A `workers.dev` Worker is **not** a substitute for this environment. Do not
replace `demos/` or `deploy/compose/docker-compose.demo.yml` with an in-Worker
analogue.

Always-on origin: **Fly.io** running that same stack. Cloudflare is DNS
(proxied) + TLS + Access on admin only. Operator logins (Cloudflare, Fly) stay
with Kieran; no API tokens belong in this repo or in chat. See
[10-harchester-fly.md](10-harchester-fly.md) for the Fly recipe and free-allowance
blockers.

## Locked public hostnames

Zone **`bruiser-gateway.com`** (already in Cloudflare). Hostnames are config
(`DEMO_CLUB_HOST` / `DEMO_TICKETS_HOST` / `DEMO_ADMIN_HOST`); Compose locally
keeps `*.localhost`. Production lock:

| Role | Hostname | Origin | DNS |
|------|----------|--------|-----|
| Club site | `club.bruiser-gateway.com` | Harchester `:8100` | Proxied CNAME → Fly app `*.fly.dev` |
| SimTix Edge | `tickets.bruiser-gateway.com` | SimTix Edge `:8091` | Proxied CNAME → same Fly app |
| Admin console | `admin.bruiser-gateway.com` | Admin `:8110` | Proxied CNAME → same Fly app; **Access** |
| Gateway | — | Compose network only | **No DNS** |
| Load-lab | — | Compose network only | **No DNS** |

**Do not point the apex or `www` at Fly.** Marketing already lives on Worker
`bruiser-gateway`:

- `bruiser-gateway.com` (apex)
- `www.bruiser-gateway.com`

Those records stay on the existing Worker. Harchester is the three hostnames
above only.

Local URLs remain `http://127.0.0.1:8100` / `:8091` / `:8110`.

## Cloudflare DNS (after Fly has an IPv6 / `*.fly.dev`)

In the `bruiser-gateway.com` zone, **orange-cloud (Proxied)**:

| Type | Name | Content | Proxy |
|------|------|---------|--------|
| CNAME | `club` | `<app>.fly.dev` | Proxied |
| CNAME | `tickets` | `<app>.fly.dev` | Proxied |
| CNAME | `admin` | `<app>.fly.dev` | Proxied |

Replace `<app>` with the Fly app name from `deploy/fly/fly.toml` (default
`bruiser-harchester-demo`). Proxied **A/AAAA** to the Fly anycast addresses is
an equivalent if CNAME is unused; still orange-cloud. Do not grey-cloud except
briefly for `fly certs add`. Do not add DNS for gateway or load-lab. Do not
point `@` or `www` at Fly.

SSL/TLS mode for the zone (or a configuration rule scoped to the three
hostnames): **Full (strict)** once the origin presents a valid certificate
(Fly-managed cert for those names, or a Cloudflare Origin CA cert on Caddy).
Until origin certs exist, temporarily grey-cloud the CNAMEs so `fly certs add`
can complete, then orange-cloud again. Do not use Flexible SSL.

### Origin TLS (Caddy / Fly)

- **Fly packed Machine (`deploy/fly`):** Fly terminates TLS on `*.fly.dev`.
  Caddy listens **HTTP** on `:8080`. Cloudflare connects to `<app>.fly.dev`
  after you add the three `fly certs add` names *or* you install a Cloudflare
  Origin CA certificate and set SSL to Full (strict). If Full (strict) fails
  on SNI, set an origin override / origin server name to `<app>.fly.dev`.
- **Compose on a VM (`deploy/caddy/Caddyfile.demo`, profile `hostnames`):**
  Caddy routes by `Host`. Behind orange-cloud, prefer a **Cloudflare Origin CA**
  certificate on Caddy (`tls /certs/origin.pem /certs/origin.key`) rather than
  Let's Encrypt HTTP-01 (the ACME client would see Cloudflare, not the visitor).
- Gateway and load-lab never get certificates or public ports.

## Cloudflare Access (admin only)

Protect **`admin.bruiser-gateway.com` only**. Club and tickets stay public
(password on the console is still required).

1. Zero Trust → **Access controls** → **Applications** → **Add** → Self-hosted.
2. Application domain: `admin.bruiser-gateway.com` (path `/`, subdomain not
   wildcard).
3. Identity: enable **One-time PIN** (email) so Outlook works without a
   corporate IdP. Microsoft Entra is optional.
4. Policy: **Allow**. Include → **Emails** → `kiedl33@outlook.com`.
   Action Allow. Session duration: 24 hours is enough for a demo day.
5. Do **not** add club or tickets applications.

Access is in addition to the console password (`harchester` in the demo). Both
must pass.

## Nightly reset

On the Fly Machine (or VM), cron `Reset demo` so a hosted session starts at
Scenario 1. Gateway audit rows are retained (C10).

## Stretch (not this environment)

A Cloudflare Worker reference Edge in `deploy/edge/cloudflare-worker/` that
calls `POST /v1/authorize` and forwards to SimTix origin — same contract as the
Go Edge. Stretch only. Not a rewrite of club, SimTix, admin, load-lab, Postgres,
or the gateway.

## Local full fidelity (canonical)

```bash
make demo-up-local
# or
docker compose -f deploy/compose/docker-compose.demo.yml up --build
```

See [README-demo.md](../README-demo.md), [demo-runbook.md](demo-runbook.md),
[demo-presenter-checklist.md](demo-presenter-checklist.md), and
[10-harchester-fly.md](10-harchester-fly.md).
