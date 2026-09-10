# Presenter checklist — Harchester United demo

Use with [demo-runbook.md](demo-runbook.md). Vocabulary (ADR-020): Bruiser is
the **authoritative control layer** that ensures one customer remains one
customer. Never: middleware, proxy, API gateway, bot detection, Redis lock,
DDoS.

## Before the room

- [ ] `make demo-up-local` (or Compose) is healthy: club `:8100`, tickets `:8091`, admin `:8110`.
- [ ] Admin password `harchester`. **Per-customer limit On**. Control is Off · Dry run · On. On = one customer, one execution. Off = no limit.
- [ ] **Reset demo** so Scenario 1 starts clean. Confirm the toast: seats remaining, executions cleared, customers online cleared.
- [ ] Browser windows: club (supporter), admin (operator). Do not show the gateway port.
- [ ] Confirm club home has no Bruiser branding.

## Scenario 1 — Normal purchase (~2 min)

- [ ] Sign in `1001234` / `password` (Alice Okafor, Gold).
- [ ] Buy Arsenal tickets → land on SimTix already signed in → hold → checkout.
- [ ] Admin: 1 attempt, 1 forwarded, observed EAF 1.0×, 1 seat sold.
- [ ] Optional: `1000002` / `password` (Sam Quinn) is not eligible.

## Scenario 2 — Flagship swarm (~3 min)

- [ ] Agent Lab: **Single supporter**, `1001234`, **10,000**, Launch.
- [ ] Say: every agent is a real login and a real hold against the **real** gateway.
- [ ] Expect: 10,000 authenticated · **1** execution forwarded · ~9,999 held back (BUSY) · downstream EAF 1× · **1** seat sold.
- [ ] Live feed: membership `1001234`, path club → Edge hold/order, one `order`, rest `busy`.
- [ ] If the run feels slow, blame Postgres session inserts (R4), not "a queue".

## Scenario Off — burn seats (~2 min)

- [ ] **Reset demo** (toast confirms seats/executions cleared; feed empty).
- [ ] **Per-customer limit Off**. Preset **Single supporter**, N = 200. Launch.
- [ ] Watch **Seats remaining** drop well past 4 and **Orders** rise toward inventory. Off lifts Bruiser hold-back **and** the SimTix per-account cap.
- [ ] Origin 409 is sold out, not Bruiser BUSY.
- [ ] **Reset** and set **Per-customer limit On** before the next scenario (cap 4 + one execution).

## Scenario 10×N enforce (~2 min)

- [ ] **Reset demo**. **Per-customer limit On**. Preset **10 × N**. Launch.
- [ ] Expect **10 orders**, rest **BUSY**. One purchase per customer. Live feed: ten memberships.
- [ ] **Reset** before Dry Run / rollout.

## Scenario 3 — Dry Run (~2 min)

- [ ] Reset demo → **Dry Run** → same swarm.
- [ ] Expect would-have-blocked high; seats churn; Authority Check **FAIL**.
- [ ] Reset before the next scenario.

## Scenario 4 — Rollout (~3 min)

- [ ] Preset **1,000 × 10**. Run 10 %, reset, 50 %, reset, 100 %.
- [ ] Same supporter stays in the same bucket (deterministic Edge assignment). Say: 10% means ~100 customers enforced, not “one in ten agents.”

## Close — Authority Check (~1 min)

- [ ] Enforcement **On**. Run Authority Check.
- [ ] Overall **PASS**, including direct allocation bypass blocked.
- [ ] Show the certificate (probes), not a screenshot of Prometheus.

## Hosted URLs (locked)

| Role | URL |
|------|-----|
| Club | https://club.bruiser-gateway.com |
| SimTix | https://tickets.bruiser-gateway.com |
| Admin | https://admin.bruiser-gateway.com (Access: `kiedl33@outlook.com`, then password `harchester`) |

Apex and `www.bruiser-gateway.com` stay on Worker `bruiser-gateway` (marketing).
Gateway and load-lab are not published. Cloudflare is DNS + proxy in front of
the Fly/Compose origin. A Worker Edge is stretch, not this demo. See
[09-harchester-cloudflare.md](09-harchester-cloudflare.md).
