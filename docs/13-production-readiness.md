# Bruiser Gateway — Production readiness (V1)

Canonical V1: `main` @ `fee0ff9eb6b580664627fdd70765f8f6b9f22191`.

External security retest: **0 Critical, 0 High**. Verdict: **PRODUCTION CANDIDATE**.

This pack takes that candidate to a merchant staging deployment and a
controlled production rollout. V1 is **feature-frozen**. Nothing here adds
product features, changes the security model, or expands scope.

Invariant: **ONE CUSTOMER. ONE EXECUTION.**

## Operator gate

```
Deploy
  → Config validation
  → Readiness / health
  → Authority Check
  → Certificate issued
  → Dry Run (0% enforce, 100% observe)
  → Progressive enforcement (10 / 25 / 50 / 75 / 100)
```

Emergency rollback is Dry Run again: `enforce_percent=0` (observation stays on).

## Documents

| # | Deliverable | Path |
|---|-------------|------|
| 1 | Production configuration | [ops/production-config.md](ops/production-config.md) |
| 2 | Staging IdP / JWKS validation | [ops/staging-idp.md](ops/staging-idp.md) |
| 3 | Closed resource catalogue | [ops/resource-catalogue.md](ops/resource-catalogue.md) |
| 4 | Authority Check certificate | [ops/authority-certificate.md](ops/authority-certificate.md) |
| 5–6 | Deployment, Dry Run, ramp, rollback, incidents | [ops/runbook.md](ops/runbook.md) |
| 7 | Release, licensing, signing keys | [ops/release-and-licensing.md](ops/release-and-licensing.md) |
| 8 | Final readiness checklist | this file, below |
| — | Example production profile | [configs/production.example.yaml](../configs/production.example.yaml) |
| — | Example catalogue | [configs/production-catalogue.example.yaml](../configs/production-catalogue.example.yaml) |

Related: [12-production-deploy.md](12-production-deploy.md) (short RC1 notes),
[ops.md](ops.md) (metrics, backup tables, ramp formula).

## Final readiness checklist

A competent platform engineer can take this repository and:

- [ ] Configure production (`BRUISER_ENV` unset or `production`; JWKS, issuer, audience, origin/edge/admin/operator secrets, Postgres, profile, catalogue)
- [ ] Keep every secret out of git (environment / SealedSecret / vault)
- [ ] Verify identity: OIDC JWT → JWKS fetch → sig / iss / aud / exp / merchant → session
- [ ] Lock the origin (`BRUISER_ORIGIN_SECRET`; execution JWT + fence required)
- [ ] Run Authority Check with `--require-certificate` against the live origin
- [ ] Start Dry Run (`BRUISER_ALLOW_UNSAFE_MODES=1` + `enforce_percent=0` or `mode=dry-run`)
- [ ] Ramp 10 → 25 → 50 → 75 → 100 with a stable customer hash
- [ ] Roll back to 0% without losing observation
- [ ] Rotate origin/edge/admin secrets and JWKS
- [ ] Revoke sessions / executions and review audit
- [ ] Respond to store/gateway/JWKS/origin failure using shipped behaviour only

Tick each item against the linked runbook. Do not invent controls that are not in the binary.

## DONE BY CURSOR

- Production remains the default posture; lab is `lab` / `dev` / `test` only.
- Production refuses lab/placeholder secrets, lab `bruiser:bruiser@` DSNs, unsigned HMAC assertions, and dry-run / partial ramp / fail-open unless `BRUISER_ALLOW_UNSAFE_MODES=1`.
- HMAC is unused in production (empty is allowed; `change-me` is still refused).
- Profile JWKS / issuer / audience / merchant overlay the environment when env vars are unset.
- Closed catalogue example and grammar documentation.
- Authority Check `--identity-token` for a merchant-minted staging JWT (probes not weakened).
- Operator runbook, staging IdP evidence template, certificate criteria, release/licensing checklist.
- Helm / image default profile is `configs/production.example.yaml` (placeholders). Compose / `make serve` stay lab.

## REQUIRES HUMAN / MERCHANT

See the exact list in [ops/release-and-licensing.md](ops/release-and-licensing.md#requires-human--merchant).

In short: real IdP, real secrets, real catalogue, staging evidence, certificate on the live origin, Dry Run observation, enforcement ramp, LICENSE/counsel, trademark, signing-key custody, branch protection, go-live approval. This repository is not published as open source from this work.
