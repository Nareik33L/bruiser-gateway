# Authority Check — production / staging gate

Authority Check is an **adversarial security certificate**, not a health
check. `/healthz` and `/readyz` prove process and store liveness.
Authority Check proves the origin cannot allocate around Bruiser, and
that forged / stale / wrong-domain material is rejected.

Do not weaken probes to achieve PASS. A lab Overall PASS is **not** a
certificate.

## Placement in the gate

```
Deploy
  → bruiser config validate <profile>
  → GET /readyz  (and bruiser doctor)
  → bruiser authority-check --front --origin --control --admin --require-certificate
  → Certificate issued
  → Dry Run
  → Progressive enforcement
```

`--origin` is required. A missing origin is not production-ready.

## Command

```bash
bruiser authority-check \
  --front "$EDGE_OR_PROXY" \
  --origin "$ORIGIN" \
  --control "$BRUISER_PUBLIC" \
  --admin "$BRUISER_ADMIN" \
  --identity-token "$STAGING_JWT" \
  --json --out authority-certificate.json \
  --require-certificate
```

| Flag | Purpose |
|------|---------|
| `--front` | Edge/Proxy URL (enforcement front). |
| `--origin` | Box-office origin. Direct bypass probes. |
| `--control` | Bruiser public listener (`/v1/authorize`, `/v1/sessions`). |
| `--admin` | Distinct admin listener. |
| `--identity-token` | Merchant-minted OIDC JWT for **legitimate** session/acquire/authorize probes. Staging/production. Never commit. |
| `--hmac-secret` | Lab only, or a **client-only** throwaway so the HMAC-rejection probe can mint a token the server must refuse. |
| `--store-down` | Optional URL of an instance whose Postgres is already down (fail-closed probe). Without it the probe is INCONCLUSIVE. |
| `--require-certificate` | Exit non-zero unless a certificate is issued. |
| `--json --out` | Recordable artifact. Tokens in cookies/Authorization are redacted. |

`make authority-check` is the **lab** invocation (HMAC cookies, lab secrets).
Overall may be PASS; certificate is **not** issued.

## What the probes still do

The checker hits the **actual origin boundary** and the front:

- Forged execution material + **valid origin secret** → expect **403**
- Stale / tampered fence + valid origin secret → **403**
- Expired / garbage execution + valid origin secret → **403**
- Revoked execution + valid origin secret → **403**
- Wrong resource / wrong merchant material → **403**
- Secret-only access (origin secret, no Bruiser execution) → **403**
- Missing origin secret → **403**
- Bypass paths (direct origin, unlisted allocation, spoofed identity, host/path smuggling)
- Unsigned identity outside a trusted edge
- Nine-variant + Unicode resource corpus under `max_active=1`
- HMAC lab assertion rejected when production identity is on
- Session revocation, replay nonce, budget (INCONCLUSIVE if unlimited)
- Admin surface absent from the public listener
- Fail-closed on store outage (INCONCLUSIVE without `--store-down`)
- Production secret validation on the credentials you passed in
- Fails against a deliberately vulnerable origin (allocation without Bruiser → overall **FAIL**)

INCONCLUSIVE is **never** PASS. Overall may still be PASS when some probes
are INCONCLUSIVE; a **certificate is not issued**.

## What constitutes a successful certificate

`certificate.issued == true` in the JSON report when **all** of:

1. Commit SHA is known (VCS-stamped binary or `BRUISER_COMMIT_SHA`). Unknown SHA → not issued.
2. The target is **production-configured** (`admin /v1/admin/status` `production=true`, or equivalent). Lab/dev cannot be certified.
3. Overall result is `PASS`.
4. **Every** probe status is `PASS` (no FAIL, no WARN, no INCONCLUSIVE).
5. Corpus version is embedded (`corpus_version`, currently `1`).

The certificate object includes `issued_at`, `commit_sha`, `corpus_version`,
`production`, and the probe list. Persist it (`AUTHORITY_CHECK` audit row
when Postgres is reachable) and keep `authority-certificate.json` in the
merchant’s evidence store — not as a secret, but not as marketing.

`--require-certificate` is the go-live gate. Health checks are not a substitute.

## Staging vs lab

| | Lab `make authority-check` | Staging / production |
|--|----------------------------|----------------------|
| Identity | HMAC box-office cookie | `--identity-token` (real IdP JWT) |
| Secrets | Makefile lab values | Merchant secrets from the environment |
| Overall PASS | Possible | Required |
| Certificate | **Not issued** | Required before Dry Run ends |

If legitimate allocation fails because HMAC is off and no `--identity-token`
was passed, that is a **procedure** miss, not a reason to re-enable HMAC
on the server.
