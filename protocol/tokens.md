# Bruiser tokens

See also [v0-draft.md](v0-draft.md). Intended Apache-2.0 after legal review.

## Customer assertion

Merchant-owned. Bruiser consumes it; it never mints a customer id.

| Kind | How Bruiser reads it |
|------|----------------------|
| HS256 JWT (lab / cookie / bearer) | Shared HMAC. Subject from `sub` or `identity.claim`. |
| OIDC / JWKS | `identity.extractor: oidc` + `jwks_url`. RS256 or EdDSA. Optional `issuer` / `audience`. |
| Opaque session | `introspect` POST to the merchant IdP. |
| Edge-signed header | `v1:<customer_id>:<exp>:<hmac_hex>` |
| Trusted header | `X-Customer-Id` (or `identity.header`) behind an authenticating edge |

## Session token

EdDSA JWT issued by Bruiser after a valid assertion. Claims: `mid`, `sub`
(customer), `pty`, `pid`, `sid`, optional `anc`. Presented as
`Authorization: Bearer` on acquire/renew/release/handoff/revoke/get/watch.

## Execution token

EdDSA JWT. Claims: `mid`, `sub`, `exe`, `dom`, `res`, `act`, `prn`, `fnc`
(fence), `exp` = lease expiry, `jti`. JWKS at
`/.well-known/bruiser/jwks.json`. A token must not outlive its lease.
Renewal issues a new token with a new `exp`/`jti` and the same fence.

Embedded SDKs verify the token and reject a stale fence.
