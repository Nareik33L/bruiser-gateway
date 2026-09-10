# Bruiser Gateway — Production deployment (RC1)

Canonical operator pack: **[docs/13-production-readiness.md](13-production-readiness.md)**.

Use that pack for production configuration, staging IdP/JWKS, the closed
catalogue, Authority Check certificates, Dry Run, ramp, rollback, and
licensing/signing checklists.

The notes below are the short RC1 reminder. They do not replace the runbook.

## Secrets

Generate distinct values. Never commit them.

```bash
openssl rand -base64 32   # BRUISER_ADMIN_SECRET
openssl rand -base64 32   # BRUISER_OPERATOR_SECRET
openssl rand -base64 32   # BRUISER_EDGE_SECRET
openssl rand -base64 32   # BRUISER_ORIGIN_SECRET
```

`BRUISER_DEV_HMAC_SECRET` is unused in production. HMAC assertions require
`BRUISER_ENV=lab` and `BRUISER_DEV_ASSERTIONS=1`. Production refuses lab
placeholders and the lab `bruiser:bruiser@` DSN.

Unset `BRUISER_ENV` is production. Lab must be `BRUISER_ENV=lab`.

## Identity

```
BRUISER_ENV=production
BRUISER_DEV_ASSERTIONS=0
BRUISER_JWKS_URL=https://idp.example/.well-known/jwks.json
BRUISER_ISSUER=https://idp.example
BRUISER_AUDIENCE=bruiser
```

Profile: `configs/production.example.yaml` (placeholders). Bruiser consumes
the merchant’s customer identifier. It does not mint one.

## Go-live gate

```
Deploy → config validate → /readyz → Authority Check (certificate) → Dry Run → ramp
```

```bash
bruiser config validate configs/production.example.yaml
bruiser doctor --profile configs/production.example.yaml --front <edge> --origin <origin>
bruiser authority-check --front <edge> --origin <origin> --control <bruiser> --admin <admin> \
  --identity-token "$STAGING_JWT" --json --out authority-certificate.json --require-certificate
```

Emergency rollback: `PUT /v1/admin/controls {"enforce_percent":0}` — observation stays on.

See [ops/runbook.md](ops/runbook.md).
