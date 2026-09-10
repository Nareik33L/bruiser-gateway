# Harchester demo runbook

Vocabulary (ADR-020): Bruiser is the **authoritative control layer** that
ensures one customer remains one customer. Never: middleware, proxy, API
gateway, bot detection, Redis lock, DDoS.

Start from **Reset demo**, **Per-customer limit On**. Hosted:
https://club.bruiser-gateway.com · https://tickets.bruiser-gateway.com ·
https://admin.bruiser-gateway.com (Access: `kiedl33@outlook.com`).

Reset between every scenario. The admin toast confirms seats restored,
executions cleared, and customers online cleared.

**Per-customer limit: Off · Dry run · On** is the presenter control.
On = one customer, one execution (Enforce 100%). Off = no Bruiser hold-back
and no SimTix per-account cap (agents buy until seats run out). Dry run
observes would-block without blocking. Rollout % stays under Advanced.

## Scenario 1 — Normal purchase (~2 min)

1. Open the club site. Confirm no Bruiser branding.
2. Sign in as `1001234` / `password`.
3. Fixtures → Arsenal → Buy tickets.
4. Land on SimTix already signed in → Best available → Checkout → Confirmation.
5. Console: attempts 1, forwarded 1, observed EAF 1.0×.
6. Optional: sign in as `1000002` and show SimTix refusing eligibility.

## Scenario 2 — 10,000-agent swarm, one supporter (flagship, ~3 min)

Agent Lab: **Single supporter**, membership `1001234`, N = **10,000**, Launch.

Expect: 10,000 authenticated · 10,000 allocation attempts · **1 execution
forwarded** · ~9,999 held back · observed EAF ≈ 10,000× · downstream EAF 1× ·
**1 seat sold**. Live feed shows membership `1001234`, status `order` once
and `busy` for the rest. Path is club → Edge hold/order.

## Scenario Off — burn seats (no guardrails, ~2 min)

Reset demo → **Per-customer limit Off** → Agent Lab **Single supporter**
(N = 200). Bruiser is not asked. The SimTix per-account cap is lifted, so
agents that reach origin complete holds **and orders** until the stand
sells out (500 seats).

Expect: **Seats remaining drops well past 4**, **Orders rise** toward
inventory (not stuck at the old 4-ticket cap). Origin 409 is **sold out**,
not Bruiser BUSY. Live feed shows `order` rows for membership `1001234`.

**10 × N** Off still works and burns faster (ten memberships, no cap).
Reset and switch **Per-customer limit On** before the next beat
(that restores origin cap 4 and Bruiser one-execution).

## Scenario 10×N enforce (~2 min)

Reset demo → **Per-customer limit On** → Agent Lab **10 × N** (N = 50 or 200).

Expect: **exactly 10 orders** (one agent per customer purchases) and the
rest **BUSY**. Live feed shows ten membership IDs; one `order` each, others
`busy`. Seats remaining drops by 10.

This is the same one-customer-one-purchase rule as Single, times ten.

## Scenario 3 — Dry Run (~2 min)

Reset demo → **Dry run** → Single 10,000 (or 10×N).

Expect: attempts forwarded; would-have-blocked high; seats churn on SimTix.
Authority Check → **FAIL** (open path). Reset.

## Scenario 4 — Progressive rollout (~3 min)

Preset **1,000 supporters × 10**. Run at 10 %, then 50 %, then 100 %, resetting
executions and seats between runs. Same supporter stays in the same bucket
(per-customer rollout, not a fraction of each customer’s agents).

## Close — Authority Check (~1 min)

At **On**, Run Authority Check → PASS including "Direct allocation bypass
blocked" → show the JSON certificate.

Presenter checklist: [demo-presenter-checklist.md](demo-presenter-checklist.md).
