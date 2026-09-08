# Bruiser Protocol v0 (draft)

Status: draft. This directory is intended to be licensed Apache-2.0 once legal
review completes. Until then the repository remains private and no licence file
is attached.

The Go gateway is one implementation of this protocol.

## Primitives (v0.1-draft)

| Primitive | HTTP | V1 |
|-----------|------|----|
| IDENTITY | `POST /v1/sessions` | yes |
| ACQUIRE | `POST /v1/executions/acquire` | yes |
| RENEW | `POST /v1/executions/{id}/renew` | yes |
| RELEASE | `POST /v1/executions/{id}/release` | yes |
| WATCH | `GET /v1/executions/{id}/watch` | yes (long-poll) |
| GET | `GET /v1/executions/{id}` | yes |
| REVOKE | `POST /v1/executions/{id}/revoke` | M5 |
| HANDOFF | `POST /v1/executions/{id}/handoff` | M5 |
| AUTHORIZE | `POST /v1/authorize` | yes (Edge) |
| INTROSPECT | `POST /v1/introspect` | yes |

## Identity

A merchant-signed customer assertion (OIDC ID token or, in development, HS256 JWT)
is exchanged for a Bruiser session bound to a principal (`agent` or `browser`).
`customer_id` is the merchant's existing supporter identity. Bruiser does not
mint customer identifiers.

Dev assertion claims: `sub` (customer_id), `exp`, optional `bruiser_anchors` object.

## Execution token

JWT/EdDSA (Ed25519), `kid` in header, JWKS at `/.well-known/bruiser/jwks.json`.

Claims: `iss=bruiser/{merchant}`, `sub` customer, `exe`, `dom`, `res`, `act`,
`prn`, `fnc` (fence), `exp` = lease `expires_at`, `jti`.

A token must never outlive its lease. Renewal issues a new token with a new `exp`
and `jti` and the same `fnc`.

## Concurrency semantics

For a given scarcity domain, ACTIVE executions never exceed `max_active`.
Denial (BUSY) needs no coordination beyond observing an ACTIVE row. Only GRANT
serialises on the domain.

BUSY response: 409 with `active_execution_id`, `holder`, `expires_at`,
`retry_after_ms`, `watch`, `can_preempt`.

## AUTHORIZE (Edge)

`POST /v1/authorize` is the Edge admission check. The WAF/gateway/worker
sends the original method and path (`X-Original-Method`, `X-Original-URI`,
or JSON `{method,path,event_id}`), the merchant session cookie, and
`X-Bruiser-Edge-Secret`. Bruiser maps the route, extracts the customer,
acquires transparently if the route is controlled, and returns `ALLOW` plus
`X-Bruiser-Execution` / `X-Bruiser-Fence` / `X-Bruiser-Origin-Secret`, or
`409 BUSY`. Uncontrolled routes (search) return `ALLOW` without a lease.

