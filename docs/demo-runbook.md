# Harchester demo runbook

Vocabulary (ADR-020): Bruiser is the **authoritative control layer** that
ensures one customer remains one customer. Never: middleware, proxy, API
gateway, bot detection, Redis lock, DDoS.

Start from **Reset demo**, enforcement **100 %**.

## Scenario 1 — Normal purchase (~2 min)

1. Open the club site. Confirm no Bruiser branding.
2. Sign in as `1001234` / `password`.
3. Fixtures → Arsenal → Buy tickets.
4. Land on SimTix already signed in → Best available → Checkout → Confirmation.
5. Console: attempts 1, forwarded 1, observed EAF 1.0×.
6. Optional: sign in as `1000002` and show SimTix refusing eligibility.

## Scenario 2 — 10,000-agent swarm, one supporter (flagship, ~3 min)

Agent Lab: membership `1001234`, 10,000 agents, single-supporter preset, Launch.

Expect: 10,000 authenticated · 10,000 allocation attempts · **1 execution
forwarded** · ~9,999 held back · observed EAF ≈ 10,000× · downstream EAF 1× ·
**1 seat sold**.

## Scenario 3 — Dry Run (~2 min)

Reset demo → Dry Run → same swarm.

Expect: attempts forwarded; would-have-blocked ≈ 9,999; seats churn on SimTix.
Authority Check → **FAIL** (open path). Reset.

## Scenario 4 — Progressive rollout (~3 min)

Preset **1,000 supporters × 10**. Run at 10 %, then 50 %, then 100 %, resetting
executions and seats between runs. Same supporter stays in the same bucket.

## Close — Authority Check (~1 min)

At 100 %, Run Authority Check → PASS including "Direct allocation bypass
blocked" → show the JSON certificate.
