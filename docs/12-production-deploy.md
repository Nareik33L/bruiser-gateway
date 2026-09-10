# Bruiser Gateway — Production deployment (RC1)

Do not enable development assertions in production.

## 1. Secrets

Generate distinct values. Never commit them.

```bash
openssl rand -base64 32   # BRUISER_ADMIN_SECRET
openssl rand -base64 32   # BRUISER_EDGE_SECRET
openssl rand -base64 32   # BRUISER_ORIGIN_SECRET
```

`BRUISER_DEV_HMAC_SECRET` is lab-only. Production uses JWKS.

Rotation: replace the secret on the edge and origin together, then
restart Bruiser. Admin/edge/origin must stay pairwise distinct.
`config validate` rejects lab placeholders when `BRUISER_ENV=production`.

## 2. Identity

```
BRUISER_ENV=production
BRUISER_DEV_ASSERTIONS=0
BRUISER_JWKS_URL=https://idp.example/.well-known/jwks.json
BRUISER_ISSUER=https://idp.example
BRUISER_AUDIENCE=bruiser
```

`BRUISER_ENV` is required (`lab` or `production`). Empty is not lab.
HMAC customer assertions require `BRUISER_ENV=lab`.

Profile:

```yaml
identity:
  extractor: oidc
  jwks_url: https://idp.example/.well-known/jwks.json
  issuer: https://idp.example
  audience: bruiser
  subject_claim: sub
```

Bruiser consumes the merchant’s customer identifier. It does not mint one.

## 3. Placement

Authority is the requirement. Embedded / Edge / Proxy are methods.

- **Edge:** `/v1/authorize` with `X-Bruiser-Edge-Secret`. Configure
  `ORIGIN_SECRET` on the edge (NGINX `set $bruiser_origin_lock`,
  Cloudflare `ORIGIN_SECRET`, Go Edge `OriginSecret`). Authorize never
  returns the origin secret.
- **Proxy:** Bruiser stamps the origin secret from its own config after
  admit. Inbound client `X-Bruiser-*` headers are stripped.
- **Embedded:** verify the execution JWT + fence at the origin
  (`sdk/go` `Protect`). Optionally call `/v1/introspect` for revoke.

Origin lockdown (`BRUISER_ORIGIN_SECRET`) stays on for Edge/Proxy so
direct origin holds fail.

## 4. Go-live checklist

1. `bruiser config validate <profile.yaml>` → no FAIL
2. `bruiser doctor --profile <profile> --front <edge> --origin <origin>`
3. `bruiser authority-check --front <edge> --origin <origin> --control <bruiser>` → PASS
4. Distinct admin / edge / origin secrets
5. Every hold/purchase path listed; origin lockdown on
6. Start at `enforce_percent=0` or 10, then raise

## 5. Rollback

Redeploy the previous image tag. Additive RC1 tables
(`execution_budget`, `replay_keys`, `sessions.revoked_at`) can remain.
An older binary ignores them.

## 6. Rate limits

Set `BRUISER_RATE_*` for sessions, acquire, renew, release, authorize,
merchant, customer, principal, and optional IP. Metrics:
`bruiser_rate_limited_total{class=…}`.
