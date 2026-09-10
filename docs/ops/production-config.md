# Production configuration

Production is the **default** posture. Unset or unrecognised `BRUISER_ENV` is
production. Lab is explicit: `lab`, `dev`, or `test`.

Unsafe lab defaults cannot silently run as production:

| Default | Production behaviour |
|---------|----------------------|
| Empty / lab placeholder secrets (`change-me*`, `*-secret-dev`) | **FAIL** boot |
| `postgres://bruiser:bruiser@…` (Makefile / Compose DSN) | **FAIL** boot |
| HMAC customer assertions (`BRUISER_DEV_ASSERTIONS=1`) | **FAIL** boot |
| Missing JWKS, issuer, or audience | **FAIL** boot |
| Missing `BRUISER_ORIGIN_SECRET` | **FAIL** boot |
| Missing `BRUISER_ADMIN_ADDR` (or same as public listen) | **FAIL** boot |
| Dry-run, `enforce_percent` 0–99, `enforcement=false`, fail-open | **FAIL** unless `BRUISER_ALLOW_UNSAFE_MODES=1` |
| `extractor: auto` + unsigned customer header | **FAIL** unless `identity.allow_unsigned_header: true` |
| Empty HMAC secret | Allowed (HMAC is unused) |
| `sslmode=disable` on a non-lab DSN | **WARN** (boot continues) |
| Profile `configs/arsenal.yaml` or merchant_id `arsenal` | **WARN** |

Commands:

```bash
# Must FAIL until secrets + a non-lab DSN are in the environment:
BRUISER_ENV=production bruiser config validate configs/production.example.yaml

# Lab drop-in (WARNs on placeholder secrets):
BRUISER_ENV=lab bruiser config validate configs/example.yaml
```

Do not store real customer credentials or production secrets in the repository.
Do not add a hosted Bruiser service. Identity is the merchant’s IdP.

## Checklist

Supply every row. Placeholders in examples are refused as production secrets.

| Setting | Required in production | Notes |
|---------|------------------------|-------|
| `BRUISER_ENV` | Recommended `production` | Unset = production. Never `lab`/`dev`/`test` on a merchant origin. |
| Identity / JWKS | **Yes** | `BRUISER_JWKS_URL` and/or `identity.jwks_url` in the profile. Env wins if both set. |
| Issuer | **Yes** | `BRUISER_ISSUER` / `identity.issuer`. Must match JWT `iss`. |
| Audience | **Yes** | `BRUISER_AUDIENCE` / `identity.audience`. Must match JWT `aud`. |
| Origin secret | **Yes** | `BRUISER_ORIGIN_SECRET`. Path trust only. Never in git. Distinct from edge/admin/operator. |
| Edge secret | **Yes** | `BRUISER_EDGE_SECRET`. `/v1/authorize` requires it. Edge/Proxy inject origin secret from **local** config; authorize never returns it. |
| Admin secret | **Yes** | `BRUISER_ADMIN_SECRET`. Policy writes, revoke-all. |
| Operator secret | **Yes** | `BRUISER_OPERATOR_SECRET`. Distinct. Controls / status. |
| Admin listen | **Yes** | `BRUISER_ADMIN_ADDR` ≠ `BRUISER_HTTP_ADDR`. |
| Database | **Yes** | `BRUISER_DATABASE_URL`. Merchant Postgres. Not `bruiser:bruiser@`. Prefer TLS. |
| Resource catalogue | **Should** | Profile `resources:` closed list. Empty = fold-only (WARN). |
| Execution budget | **Should** | Policy `budget.max_ops` (example `1`). Omit / 0 = unlimited (Authority Check budget probe INCONCLUSIVE). |
| Rate limits | Optional | `BRUISER_RATE_*` (sessions, acquire, renew, release, authorize, merchant, customer, principal, IP). Defaults are non-zero except IP. |
| Session TTL | Optional | `BRUISER_SESSION_TTL` (default 1h). |
| Lease settings | Optional | `BRUISER_LEASE_TTL` (60s), `BRUISER_HEARTBEAT_INTERVAL` (20s), `BRUISER_MAX_LIFETIME` (15m), `BRUISER_SWEEP_INTERVAL` (2s). Lease ≥ heartbeat; max lifetime ≥ lease. |
| Fail-closed | **Yes** | `BRUISER_FAIL_CLOSED=true` (default). `BRUISER_FAIL_OPEN` / `fail_closed=false` needs `BRUISER_ALLOW_UNSAFE_MODES=1`. |
| Logging / audit | Optional | `BRUISER_LOG_LEVEL` (info). `BRUISER_AUDIT_RETENTION` (default 13 months). Purge writes `AUDIT_PURGED`. |
| Profile | **Yes** | `BRUISER_PROFILE` → merchant YAML. Image default is `production.example.yaml` (placeholders). |
| Merchant id | **Yes** | Profile `merchant_id` or `BRUISER_MERCHANT_ID`. Used for binding and signing keys. |
| Public listen | Optional | `BRUISER_HTTP_ADDR` (default `:8080`). |
| Proxy | If Proxy placement | `BRUISER_PROXY_ADDR` + `BRUISER_ORIGIN_URL`. |
| Telemetry | Off | `BRUISER_TELEMETRY` unset/0. Set `1` only if the merchant accepts vendor telemetry. |
| HMAC | Unused | Do not set `BRUISER_DEV_ASSERTIONS`. Omit `BRUISER_DEV_HMAC_SECRET`. A `change-me` value **fails** boot. |
| Dry Run / ramp | Override | Staging Dry Run: `BRUISER_ALLOW_UNSAFE_MODES=1` and `BRUISER_MODE=dry-run` or `BRUISER_ENFORCE_PERCENT=0`. Documented, not silent. |

## Validation errors (what you will see)

`bruiser config validate` prints `FAIL` / `WARN` / `PASS` per field.

| Symptom | Cause |
|---------|--------|
| `production identity is unconfigured; required: BRUISER_JWKS_URL, …` | No JWKS/issuer/audience in env **or** profile |
| `production refuses lab/placeholder secrets: BRUISER_…` | `change-me`, `*-secret-dev`, empty required secret |
| `production refuses the lab database URL` | `bruiser:bruiser@` DSN |
| `BRUISER_DEV_ASSERTIONS requires BRUISER_ENV=lab` | HMAC mode outside lab |
| `production refuses BRUISER_MODE=dry-run without BRUISER_ALLOW_UNSAFE_MODES=1` | Dry Run not acknowledged |
| `production policy is fail-open (unmatched: allow)` | Fail-open profile without override |
| `production refuses extractor: auto with an unsigned customer header` | Unsigned header not opted in |
| `resources[n] is not a canonical resource ID` | Catalogue entry failed grammar |
| `empty catalogue: inbound IDs fold only` | WARN — enable `resources:` for defence in depth |
| `BRUISER_ADMIN_ADDR must be distinct from BRUISER_HTTP_ADDR` | Admin on the public listener |

Startup logs one banner:

```
bruiser startup env=production mode=enforce enforce_percent=100 fail_open=false identity=jwks
```

## Secrets

Generate distinct values. Never commit them.

```bash
openssl rand -base64 32   # BRUISER_ADMIN_SECRET
openssl rand -base64 32   # BRUISER_OPERATOR_SECRET
openssl rand -base64 32   # BRUISER_EDGE_SECRET
openssl rand -base64 32   # BRUISER_ORIGIN_SECRET
```

Configuration examples (`configs/production.example.yaml`, Helm `values.yaml`) contain **placeholders only**. Helm `change-me` values fail `ValidateSecrets` until replaced via `--set`, SealedSecret, or an external Secret.

`BRUISER_DEV_HMAC_SECRET` is a lab customer-assertion key. Production refuses HMAC assertions. Authority Check may mint a **client-only** HMAC token to prove the server rejects it; that value is not a production secret.

## Identity

Bruiser **consumes** the merchant customer identifier. It does not mint one.

Production extractors: `oidc` / `jwks` (and cookie/bearer JWT verified against JWKS). Allowed algorithms: RS256, EdDSA, ES256. HS256 only in lab with `BRUISER_DEV_ASSERTIONS=1`.

Request-time checks (fail closed):

1. JWKS fetch (5 minute cache; fetch failure → unauthorized, not a skip)
2. Signature
3. Issuer (required in production)
4. Audience (required in production)
5. Expiry (`exp`, 5s leeway)
6. Merchant binding (`mid` / `merchant_id` claim vs configured merchant id, when the claim is present)

## Example

Copy [configs/production.example.yaml](../../configs/production.example.yaml). Replace `merchant-placeholder`, `idp.example`, route paths, and catalogue IDs. Keep `unmatched: deny` unless Dry Run acknowledgement is intentional.
