# Demo walkthrough (about 90 seconds)

This is the non-engineer script. The headline is EAF: how many allocation
attempts one customer generated versus how many authorised executions reached
the box office.

Standing lab merchant: unverified Arsenal-like profile. Not a customer claim.

## One-liner

```bash
export BRUISER_DATABASE_URL=postgres://bruiser:bruiser@127.0.0.1:5432/bruiser?sslmode=disable
make serve            # :8080 control plane; set BRUISER_PROXY_ADDR=:8081 BRUISER_ORIGIN_URL=http://127.0.0.1:8090
make simtix           # :8090 origin + :8091 Edge
```

Or: `make demo-up` (Compose with Grafana on :3000). Dashboard: http://127.0.0.1:8080/admin

## Script

1. Open `/admin`. Observed EAF starts at 0×. Downstream is the number that
   should stay at 1× for one customer.
2. `make demo-unaware` or `bruiser swarm --front http://127.0.0.1:8091 --n 200`.
   Observed EAF moves toward N×; downstream stays 1×. SimTix created one hold.
3. `make authority-check`. Overall Result PASS, including direct bypass blocked.
4. Take control: a browser handoff (`TestTakeControlHandoffScenario` / the
   product scenario) shows the agent's next renew is 410.
5. Bypass contrast: origin lockdown off → Authority Check FAIL names the open
   path; lockdown on → PASS.

## Make targets

| Target | What it asserts |
|--------|-----------------|
| `make demo-1x10000` | 1 customer × 10,000 unaware agents via Proxy |
| `make demo-1000x10` | 1,000 customers × 10 agents; ≤1 grant each |
| `make demo-handoff` | Points at the CI take-control scenario |
| `make demo-bypass` | Straight to origin (lockdown expected to refuse) |
| `make demo-unaware` | Protocol-ignorant swarm on Edge |
| `make eaf-nightly` | 1×10,000 as a Go test (`BRUISER_EAF_N=10000`) |

Automated regression: `TestDemoAssertion` (50-agent 1×N + NxK + Authority Check
PASS). Full 10,000 is nightly, not every PR.

Hosted sandbox (`sandbox.bruiser-gateway.com`) is M8 / Stage A outbound — not
claimed here.
