# Staging IdP / JWKS validation

The lab never exercised a real identity provider. That is a **staging
item**, not a product redesign. Production identity requirements stay as
they are: JWKS, issuer, audience, expiry, merchant binding, fail-closed
cache. Do not enable `BRUISER_DEV_ASSERTIONS` to make staging easier.

Do not commit IdP credentials, private keys, client secrets, or JWTs.

## What must be exercised

Run these in order against the **staging** Bruiser + origin. Record
evidence in the template below.

| # | Step | What “pass” looks like |
|---|------|-------------------------|
| 1 | Customer authentication | Merchant IdP login succeeds for a **test** customer (not a real fan account if avoidable). |
| 2 | Real JWT issuance | IdP issues a JWT with `sub` (or configured `subject_claim`), `iss`, `aud`, `exp`. Optional `mid` / `merchant_id` matching Bruiser `merchant_id`. |
| 3 | JWKS discovery / fetch | Bruiser process fetches `identity.jwks_url` / `BRUISER_JWKS_URL` (HTTP 200, non-empty keys). |
| 4 | Signature verification | Intact JWT → session created. Tampered payload/signature → **401**. |
| 5 | Issuer validation | JWT with wrong `iss` → **401**. |
| 6 | Audience validation | JWT with wrong `aud` → **401**. |
| 7 | Expiry validation | Expired JWT (`exp` in the past) → **401**. |
| 8 | Merchant binding | JWT with a different `mid` / `merchant_id` than the profile → **401**. (Skip with note if the IdP does not emit the claim.) |
| 9 | Session creation | `POST /v1/sessions` with `Authorization: Bearer <jwt>` → **201/200** and `session_token`. |
| 10 | Acquire | `POST /v1/executions/acquire` with the session → **201** `ACTIVE` (or `ALREADY_HELD` on retry). Second principal, same customer+resource → **409 BUSY** or **202 QUEUED**. |
| 11 | Execution certificate | Acquire / authorize returns an execution JWT + fence. Downstream SDK or origin verifies signature against `/.well-known/bruiser/jwks.json`. |
| 12 | Downstream / origin verification | Hold/purchase at origin with valid execution + fence + origin secret → allocated. Garbage token + valid origin secret → **403**. Missing origin secret → **403**. |

Complementary: [authority-certificate.md](authority-certificate.md) after this path works.

## Procedure (concise)

1. Deploy Bruiser with `BRUISER_ENV=production` (or unset), real staging JWKS/issuer/audience, distinct secrets, merchant profile, closed catalogue.
2. `bruiser config validate <profile.yaml>` — no `FAIL`.
3. `GET /readyz` — `store=ok`, `signing_key=ok`.
4. Authenticate the test customer at the IdP. Capture the access/ID token **in the operator’s secret store**, not in git.
5. Session:

   ```bash
   curl -sS -D- -X POST "$BRUISER/v1/sessions" \
     -H "Authorization: Bearer $STAGING_JWT" \
     -H "Content-Type: application/json" \
     -d '{"principal":{"type":"agent","id":"staging-agent-1"}}'
   ```

6. Acquire:

   ```bash
   curl -sS -D- -X POST "$BRUISER/v1/executions/acquire" \
     -H "Authorization: Bearer $SESSION" \
     -H "Content-Type: application/json" \
     -d '{"resource":"event:fixture-1","action":"hold"}'
   ```

7. Negative cases (expect 401): truncated JWT, wrong `iss`, wrong `aud`, expired token, wrong merchant claim.
8. Origin: present execution + fence + `X-Bruiser-Origin-Secret`. Then repeat with a forged token and the **same** origin secret (expect 403).
9. Authority Check with the same JWT (do not weaken probes):

   ```bash
   bruiser authority-check \
     --front "$EDGE" --origin "$ORIGIN" --control "$BRUISER" --admin "$ADMIN" \
     --identity-token "$STAGING_JWT" \
     --json --out staging-authority-certificate.json \
     --require-certificate
   ```

`--identity-token` is the merchant JWT. It is not a lab HMAC cookie. HMAC
assertions must still be **rejected**. Do not log the token.

## Evidence format (recordable)

Copy this block into the staging ticket. Attach status codes and redacted
headers only (no secrets, no full JWTs).

```
Staging IdP / JWKS evidence
Date (UTC):
Bruiser commit SHA:
Profile path:
BRUISER_ENV:
JWKS URL (hostname only if the path is sensitive):
Issuer:
Audience:
Merchant id:
Catalogue closed (yes/no):

1. Customer auth: PASS/FAIL  notes:
2. JWT issued (alg, kid present): PASS/FAIL  alg=  kid=
3. JWKS fetch: PASS/FAIL  http=
4. Signature (good JWT / tampered): PASS/FAIL  good=  tampered=
5. Issuer mismatch: PASS/FAIL  http=
6. Audience mismatch: PASS/FAIL  http=
7. Expired JWT: PASS/FAIL  http=
8. Merchant mismatch: PASS/FAIL/SKIP  http=
9. POST /v1/sessions: PASS/FAIL  http=
10. Acquire + second principal: PASS/FAIL  first=  second=
11. Execution JWT verified at origin/SDK: PASS/FAIL
12. Origin forged token + valid origin secret: PASS/FAIL  http=

Authority Check overall:
Certificate issued: yes/no
Artifact: staging-authority-certificate.json (probes only; redact tokens)
Operator:
```

## Out of scope

- Redesigning identity
- Committing a lab JWKS private key
- Using `BRUISER_DEV_ASSERTIONS=1` on staging
- Treating discovery `control: none` as fail-open
