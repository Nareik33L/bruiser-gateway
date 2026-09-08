# Bruiser Gateway — Threat model (lab)

This is an engineering threat model, **not** an independent security review.
A production deployment still needs an external review before go-live
(product instructions §19).

## Assets

| Asset | Why it matters |
|-------|----------------|
| Customer identifier | Merchant-owned. Bruiser must consume it, never mint or leak it to a vendor. |
| Execution lease + fence | Duplicate grants multiply purchasing power. |
| Execution token | Bearer of an authorised scarce-inventory operation. |
| Policy document | Wrong `max_active` or extractor breaks the guarantee. |
| Audit log | Answers who acted, which client, which rule. |
| Admin secret | Operator surface (policy, revoke, export). |

## Trust boundaries

1. **Merchant identity system → Bruiser.** Bruiser trusts a configured extractor
   (cookie JWT, bearer JWT, header, introspection, edge-signed HMAC, OIDC/JWKS).
   A forged session is a forged customer. Unsigned identity headers are only
   safe when a trusted, authenticated edge is the only party that can set them.
2. **Bruiser → origin.** Origin must reject unfenced or secret-less allocation
   (Authority Check). Parallel paths are the primary bypass.
3. **Agent / browser → Bruiser.** Unaware clients are still enforced. MCP/SDK
   are convenience only.
4. **Operator → admin API.** Shared `BRUISER_ADMIN_SECRET` today. Not
   per-operator RBAC.

## Threats and mitigations already in the tree

| Threat | Mitigation |
|--------|------------|
| Many agents of one customer | Domain serialises grants; one ACTIVE (or `max_active`) per scarcity domain |
| Duplicate grant after reconnect | Same principal before expiry is ALREADY_HELD, same execution id |
| Stale holder after handoff | Fence increments; Embedded SDK `FenceCache` rejects `fence < last` |
| Replay of an old token | Short-lived `exp`; fence; origin lockdown secret |
| Store down on allocation | Fail closed (`503` + `Retry-After`) |
| Unmatched discovery traffic | Fail open (`unmatched: allow`) so Proxy can sit in front of a whole origin |
| Busy-cache lying | Stale cache can only cause a spurious BUSY, never a second grant |
| Oversized JSON bodies | `MaxBytesReader` 1 MiB on session/acquire |
| Vendor telemetry / PII | Off unless `BRUISER_TELEMETRY=1`; no customer fields in default logs |
| Intra-customer stampede | Optional bounded queue; overflow is BUSY |

## Out of scope here (still required before production)

- External penetration test and dependency/SBOM review (`govulncheck` is CI only)
- Signed container images and admission keys
- Hosted multi-tenant isolation (sandbox is a sales asset)
- Formal LICENSE / counsel review
- Per-operator admin RBAC

## Residual risk

OIDC/JWKS keys are cached for five minutes. A rotated IdP key is not seen
until the cache expires or the process restarts. Merchants who rotate often
should point Bruiser at a short-TTL CDN in front of JWKS or restart on rotation.

`identity.extractor: header` trusts whatever string the request presents.
That is an operator choice, not a Bruiser-minted identity. Prefer signed
extractors (cookie JWT, bearer JWT, OIDC, edge-signed HMAC). Unsigned
headers are not a V1 blocker when the club already authenticates at the
edge the way the Arsenal-like lab does.
