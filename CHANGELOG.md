# Changelog

## Unreleased

### Added

- Transparent dry-run (`BRUISER_MODE=dry-run` or admin controls): same path
  and policies, never blocks, records `WOULD_ALLOW` / `WOULD_QUEUE` /
  `WOULD_REJECT` / `WOULD_EXPIRE`. Admin **What Bruiser would have stopped**
  (`GET /v1/admin/dry-run`) including would-have EAF.
- Emergency controls: enforcement/queue kill switch, waiter drain, revoke-all,
  lease TTL override, fail-closed, route-level `disabled_actions`. Audited.
- `bruiser doctor` — PASS / WARN / FAIL (config, identity, routes, upstream,
  signing keys, persistence, queue, authority, limits, metrics, audit).
- `bruiser config validate` (also `profile validate` prints actionable issues).
- `/readyz` reports store, signing key, mode, enforcement, upstream.
- Post-core roadmap: [docs/09-post-core-capabilities.md](docs/09-post-core-capabilities.md).

- Optional bounded intra-customer queue (`waiting.mode: bounded`, `max_waiters`).
  Customer-concurrency only: one ACTIVE per customer/domain; extra agents of
  that customer QUEUED (HTTP 202), overflow BUSY. A second customer is never
  lined up behind the first. Promote on release, revoke, and expire. Same
  principal re-acquire while queued is idempotent.
- OIDC / JWKS identity extractor (`identity.extractor: oidc`, `jwks_url`).
  RS256, EdDSA, optional issuer/audience. JWKS cached 5 minutes.
- Agent-side Go client (`sdk/go` `Client`: session, acquire, renew, release, get).
- MCP tool catalogue (`protocol/mcp/tools.json`). Convenience only.
- Helm: migrate Job, HPA, PDB, NetworkPolicy, ServiceMonitor.
- `govulncheck` CI job.
- k6 EAF load profile (`deploy/k6/eaf.js`).
- Ops runbook, threat model, protocol token/audit notes, this changelog.
- HTTP body limit (1 MiB) on session and acquire.

### Changed

- Protocol capabilities advertise `QUEUE`.
- Admin usage figures include `queue_depth`.
- Admin dashboard shows queue depth and live waiters.
- `POST .../release` (and `/leave`) dequeues a waiter; sweeper expires stale waiters.
- Node and Python SDKs include a thin agent `Client`.
- CycloneDX SBOM in CI; Grafana panel for `bruiser_queue_depth`.
- Per-request 8s deadline except watch/SSE.

Humans still own: LICENSE/counsel, trademark, external security review,
hosted sandbox DNS, Stage A outbound, image signing keys.
