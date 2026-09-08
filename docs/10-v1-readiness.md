# Bruiser Gateway — V1 readiness

Feature-complete V1 is **frozen**. RC1 closed the independent security
review without adding commercial features. This document records whether
the existing implementation is production-functional, what was proven,
and what remains a deployment limitation. See [docs/11-rc1.md](11-rc1.md).

Operational edges closed in this revision: process-local EAF gauges
documented (do not sum), unsigned header identity documented as a
merchant trust-boundary, and a long-churn soak (`make soak`) added.

Reproduce everything here with:

```bash
make test-race
make sdk-test
make v1-accept
# Optional 1×10,000 origin-hold proof (not default CI):
make eaf-nightly
```

## 1. PASS — proven

| Claim | Evidence |
|-------|----------|
| One customer + one agent executes; origin records one hold | `TestV1AcceptanceLifecycle`, `TestEAFUnawareSwarm` |
| Same customer + second agent is controlled | Proxy/Edge 409 BUSY; agent acquire 409; 10%/100% ramp in-bucket |
| Agent multiplication does not multiply origin executions | 200-agent swarm + 3×250 multi-customer swarm: origin `held` equals unique customers |
| Different customers execute independently | Lifecycle + `TestQueueDoesNotLineUpOtherCustomers` + multi-customer swarm |
| Release / leave dequeue the next same-customer waiter | `TestHTTPBoundedQueue`, `TestHTTPLeaveQueue`, store queue tests |
| Lease expiry frees the execution; sweeper can promote | `TestAcquireAfterExpiryGrantsNew`, `TestBoundedQueuePromoteOnExpire` |
| Lease renewal keeps a valid execution alive | `TestHeartbeatAndReconnect`, replica renew |
| Stale / tampered / missing execution credentials rejected | Authority Check + `TestEmbeddedRejectsMissingAndTampered` + handoff fence |
| Replayed / fenced credentials rejected | Handoff: old fence → 410 / stale fence on Embedded origin |
| Cancellation (revoke) works | Customer revoke, `TestEmergencyDrainAndRevokeAll` |
| Browser/agent handoff works | `TestTakeControlHandoffScenario`, store handoff races |
| Emergency 0% disables enforcement, keeps observe/evaluate/record | `TestV1AcceptanceEmergencyZeroKeepsObservation`, `TestDryRunNeverBlocks` |
| 100% full enforcement | `TestOneHundredPercentEnforcesSecondAgent` |
| 10% customer-stable assignment; 90% still recorded | `TestProgressiveRampDeterministicAndScoped`, `TestV1AcceptanceRampSemantics` |
| Authority over the locked Proxy/Edge pattern | `bruiser authority-check` PASS with origin lockdown + listed paths + control-plane edge secret |
| Store races cannot grant two actives when `max_active=1` | `TestConcurrentAcquireExactlyOne` and `internal/store/postgres/race_test.go` |
| Store outage fail-closed | `TestFailClosedOnStoreOutage` (503 / 5xx, not ALLOW) |
| Persistence across replica / new process | `TestPersistenceAcrossReplica`, `TestRestartNewProcessSameStore` |
| Embedded / Edge / Proxy share the same admission rule | Placement parity + Embedded token test |
| Installed Embedded / Edge / Proxy behave the same | `TestV1DeploymentAcceptance` — auth, identity, acquire, renew, release, queue, handoff, revoke, expiry, origin lockdown, Dry Run, 10%, 100%, Authority Check |
| One customer cannot produce two active origin executions under attack | `TestV1AdversarialOneCustomer` — 400-way acquire, renew/release, reconnect, expiry resurrection, handoff/revoke replay, identity and merchant switching, unlisted routes, direct origin, malformed/replayed credentials, replica split, store outage, process restart, timeouts |
| Admin is not unlocked by the edge secret or `?secret=` | `TestSecurityAdminAndEdgeSecrets` |
| Resume is observed but not counted as a new origin forward | `TestMetricsResumeDoesNotCountForwarded` |
| Distinct admin vs edge secrets in the lab | `internal/testlab` + `startServer` |

## 2. FAIL — remaining

None remaining in the V1 surface after the fixes below. These *were*
broken and are closed in this slice:

- Cross-customer renew: `holderMatches` compared principal id only, so
  Bob/`agent-1` could heartbeat Alice/`agent-1`. Now customer (and
  merchant) must match.
- Dry-run / 0% Proxy path did not stamp `X-Bruiser-Origin-Secret`, so
  origin lockdown 403'd traffic that must “continue normally.”
- EAF headline hard-coded downstream = 1; resume counted as forwarded.
- Lab admin secret aliased the edge secret; `?secret=` leaked on `/admin`.

This revision: `make soak` (10,000 agents, 30 minutes) stayed at
**ACTIVE=1** through replica kill + replacement: 3.94M requests, 0
errors, p50 ≈ 30ms / p99 ≈ 60ms, heap 14–34MB, goroutines ~340 then 34
at stop. `TestSoakChurnSmoke` is the CI harness proof. The soak
harness session-token slice is mutex-protected so reconnect cannot
race acquire under `-race`. `make test-race` and `make v1-accept` are
green on this revision. RC1 re-ran the same soak after the security
layer; see [docs/11-rc1.md](11-rc1.md).

## 3. WARN — limitations that do not block V1

| Item | Why it is not a V1 blocker |
|------|----------------------------|
| `unmatched: allow` | Discovery fail-open by design. `config validate` FAILs if origin lockdown is off. Authority Check probes common forgotten paths on the front **and** the origin. |
| Admin cookie is not `Secure` | Lab HTTP. Terminate TLS at the ingress and do not expose `/admin` publicly without a network policy. |
| Prometheus EAF gauges are process-local | Do not sum `bruiser_observed_eaf` or `bruiser_downstream_eaf` across replicas. Cluster EAF is `sum(allocation_attempts_total) / sum(executions_forwarded_total)`. See `docs/ops.md` and `deploy/prometheus/eaf.rules.yaml`. |
| Busy cache is in-process | Correctness is the store. A replica restart rebuilds the cache. |
| 10,000-agent burst is nightly | Default CI uses 200 + 3×250. `make eaf-nightly` is the 1×10,000 burst. `make soak` is the 30-minute 10k churn. |
| `fail_closed=false` and `enforcement=false` | Operator-chosen fail-open. Default is fail-closed on allocation. |
| Header-extractor identity | Unsigned headers are rejected unless the caller is a trusted edge. Production requires JWKS/OIDC. |
| P1 items | Full MCP server, OTel, admin SSO, Testcontainers, published k6, Helm ZDT, protocol v1 — not required for Stage A. |

## 4. Tests added or strengthened

- `internal/torture/soak_test.go` — long-churn soak (renew/reconnect/release/queue + replica restart)
- `internal/check/adversarial_test.go` — final attack pass: one customer, one active origin execution
- `internal/check/deploy_accept_test.go` — one suite against Embedded, Edge, and Proxy install paths
- `internal/check/v1_accept_test.go` — lifecycle, ramp, authority, placement, amplification
- `internal/check/check.go` — spoofed identity, unlisted paths, origin-secret spoof, removed proxy headers, authorize edge secret, PASS/FAIL/WARN
- `internal/check/proxy_test.go` / `eaf_nightly_test.go` — origin `held` assertion
- `internal/store/postgres/race_test.go` — acquire/release, renew/revoke, expiry, duplicates, waiter promote, handoff
- `internal/api/public/v1_readiness_test.go` — secrets, fail-closed, persistence, drain/revoke-all, metrics, 100%, admin cookie
- SimTix `GET /api/events/{id}` now returns `held` / `seats` so origin proof is observable

## 5. Commands

```bash
# Full race suite (required)
make test-race

# SDKs
make sdk-test

# Focused V1 acceptance
make v1-accept

# Authority Check against a running lab
make authority-check
# or:
bruiser authority-check --front http://127.0.0.1:8091 --origin http://127.0.0.1:8090 --control http://127.0.0.1:8080

# 1 customer × 10,000 agents (origin held must stay 1)
make eaf-nightly

# Long churn (default 30m × 10,000 agents). Not default CI.
make soak
```

## 6. Remaining production blockers

Engineering blockers for a **supported** deployment (Proxy or Edge in
front of every allocation route, origin lockdown on, distinct secrets,
Postgres reachable):

- None identified. Go-live requires
  `bruiser authority-check --front … --origin … --control …` PASS,
  a dedicated `BRUISER_ADMIN_SECRET`, and origin lockdown with every
  hold/purchase path listed. `make test-race` passed on this revision.

Still **human-owned** (not software gaps):

- External security review
- LICENSE / counsel
- Image signing keys
- Hosted sandbox DNS
- Stage A partner outbound
- Fair-use envelope

## 7. Assumptions

- Supported V1 placements are **Embedded**, **Edge**, and **Proxy** as
  already implemented. Authority is the requirement; placement is a
  method.
- Origin lockdown (`X-Bruiser-Origin-Secret`) is on for Edge/Proxy.
  Embedded origins verify the execution JWT + fence.
- Embedded Dry Run does not mint an execution token. A `RequireExecution`
  origin therefore stays locked at 0% / out-of-ramp; the app is expected
  to continue without calling origin under Bruiser's token. Edge/Proxy 0%
  stamps the origin secret so unaware traffic still reaches the locked
  origin. `TestV1DeploymentAcceptance` asserts both. `bruiser
  authority-check` is the Edge/Proxy front; Embedded authority is missing
  / tampered / valid execution JWT against the origin.
- Postgres is the backing store (one logical database, one or more
  Bruiser replicas).
- Merchant identity is already authenticated; Bruiser consumes it.
- `make eaf-nightly` is the reproducible 10k burst; `make soak` is the
  30-minute 10k churn. Default CI stays smaller so it remains runnable.
- V1 is frozen. Remaining work is human-owned (LICENSE, counsel, external
  review, signing keys, sandbox DNS, Stage A outbound).
- No new product features were added. Admin login now sets a cookie
  instead of putting the secret in the query string — a security fix of
  the existing admin surface.
