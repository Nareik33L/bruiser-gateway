# Changelog

## Unreleased

### Added

- Optional bounded intra-customer queue (`waiting.mode: bounded`, `max_waiters`).
  One ACTIVE, up to N QUEUED (HTTP 202), overflow BUSY. Promote on release,
  revoke, and expire. Same principal re-acquire while queued is idempotent.
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
