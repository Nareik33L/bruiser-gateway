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
| RENEW | `POST /v1/executions/{id}/renew` | yes (heartbeat) |
| HEARTBEAT | `POST /v1/executions/{id}/heartbeat` | yes (alias of RENEW) |
| RELEASE | `POST /v1/executions/{id}/release` | yes |
| WATCH | `GET /v1/executions/{id}/watch` | yes (long-poll) |
| GET | `GET /v1/executions/{id}` | yes |
| REVOKE | `POST /v1/executions/{id}/revoke` | yes |
| HANDOFF | `POST /v1/executions/{id}/handoff` | yes |
| AUTHORIZE | `POST /v1/authorize` | yes (Edge) |
| INTROSPECT | `POST /v1/introspect` | yes |
| POLICY | `GET/PUT /v1/policy` | yes (NOTIFY + poll reload) |
| ADMIN | `GET /admin`, `/v1/admin/*` | yes |
| AUTHORITY_CHECK | `POST/GET /v1/authority-check` | yes (audit `AUTHORITY_CHECK`) |

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

## Heartbeat and recovery

While an execution is ACTIVE, the client sends a heartbeat every 20–30 seconds
(default 25 s). Each heartbeat is RENEW (or the `/heartbeat` alias) and extends
`expires_at` to `min(now() + ttl, max_lifetime_at)`. Grant, renew, and authorize
ALLOW responses include `heartbeat_after_ms`. If heartbeats stop, the lease
expires after the TTL (default 60 s) and is released automatically.

Reconnect before expiry resumes the **same** execution (ALREADY_HELD extends TTL
and rebinds the session to the caller). After expiry, a new execution may be
acquired according to merchant policy.

On Edge AUTHORIZE, the same merchant cookie presenting again is the heartbeat;
unaware clients do not call RENEW.

## Handoff and revoke

Default precedence: `browser` outranks `agent`. Equal ranks cannot preempt.

- Preemptive handoff: a higher-rank principal (typically a browser) calls
  `POST /v1/executions/{id}/handoff` with `mode=preempt`. The holder's execution
  becomes `HANDED_OFF` with `successor_id`; a new ACTIVE execution is granted
  with `fence+1` to the caller. The old holder's next renew is `410 Gone`.
- Cooperative handoff: the holder calls handoff with `to: {type, id}`.
- BUSY responses set `can_preempt: true` when the caller outranks the holder.
- Revoke: `POST /v1/executions/{id}/revoke` by the holder or a higher-rank
  principal of the same customer. Introspection of a revoked or handed-off token
  returns `active: false`. Downstream fence checks reject the preempted token
  once a higher fence has been seen.

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

## Policy

`GET` / `PUT /v1/policy` (admin or edge secret) reads and activates a YAML
document. Schema: `protocol/policy.schema.json`. First matching `domains[]`
rule wins. `PUT` applies to new acquires only. `bruiser policy validate`
compiles a file without activating it.

Denied acquires return `403 {error: DENIED, reason, rule_name}`. BUSY includes
`rule_name`. `reason=missing_anchor` when a required `anchor:*` scope dimension
is absent and the rule does not fall through.

