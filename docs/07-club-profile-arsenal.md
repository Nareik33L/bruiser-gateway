# Club profile: Arsenal-like Premier League club

**Status: unverified best-guess.** Standing design-partner analogue until Stage A
discovery confirms or replaces it. Nothing here is a claim about Arsenal FC as a
customer. It is a working model of *a club that looks like Arsenal from public
ticketing artefacts*, so engineering can proceed without waiting on a call.

Replace any row the moment a discovery call lands. Confidence is relative to
public information, not to a signed-off integration.

Public sources used: Arsenal help centre (account, purchase guides),
`eticketing.co.uk/arsenal`, `web-identity.tmtickets.co.uk/uk_arsenal` (OIDC login
return URL on arsenal.com women ticketing pages). Dated September 2026.

---

## Standing decisions (until contradicted)

| Decision | Guess | Confidence | What discovery must confirm |
|----------|-------|------------|-----------------------------|
| Ticketing platform | Ticketmaster **eTicketing** (box office on `eticketing.co.uk/arsenal`; identity on `tmtickets.co.uk`) | High | Contracted vendor, API ownership, whether a pre-allocation hook exists |
| Checkout owner | **Platform, not the club.** Club site (`arsenal.com`) is content/membership; allocation happens on the box-office host | High | Whether any hold/purchase still runs on club-owned code |
| Identity | **7-digit membership number**, issued on email registration, linked to SSO on first box-office login | High | Claim name in the session token (`sub` vs `membership_no`); whether it is in a JWT the edge can read |
| Household / network | "My Network" on the box office = family/friends the member can buy for. Treat as a **network_id** anchor, not a true household | Medium | Whether a stable network/household id is asserted, or only a list of other membership numbers |
| Membership tiers | Red / Silver / Gold / Junior / Season Ticket / Hospitality exist commercially; **not used in V1 policy** | Medium | Whether allocation rules differ by tier (then a policy dimension, not V1) |
| Admission point | `POST` hold / add-to-basket on the box office, then `POST` checkout/order | Medium (path shape invented) | Exact paths, mobile app, Ticket Exchange, hospitality, box-office till |
| Viable deployment | **Edge** first (auth in front of allocation APIs), **Proxy** if they can route those hostnames. **Embedded blocked** until the platform allows a middleware | Medium | Who controls the WAF/CDN/API gateway in front of `eticketing.co.uk/arsenal`; whether origin lockdown is possible |
| Waiting room | Assume a vendor waiting room (Queue-it or platform-native) **in front of Bruiser** | Low | Product and whether Bruiser sees only admitted traffic |
| Peak | Big home match on-sale: tens of thousands of concurrent supporters, extreme agent amplification | Low (order-of-magnitude) | Actual peak concurrent checkouts and bot/agent complaints |
| Data residency | UK, self-hosted in club or UK cloud account | Medium | AWS/Azure/GCP, who operates Postgres |
| Fair-use | One production deployment, men's + women's first-team on-sales | Low | Envelope numbers |

### Deployment method we will build against

```
Internet
  → CDN / WAF / waiting room          (unchanged; club or platform)
  → Edge admission (Bruiser /v1/authorize)   ← we assume this is viable
  → Box office origin (eTicketing analogue)  ← origin lockdown
  → Inventory
```

If discovery finds the club cannot put anything in front of the box office and
the platform has no hook, this profile is **not a V1 customer** (questionnaire
outcome: none). We still keep the lab, because Championship / League One clubs
with more control look like a *simpler* version of the same model.

Embedded remains in the product (SDK) for clubs that do own checkout; it is not
the Arsenal-like path.

---

## Filled questionnaire (assumed)

### A. Admission point

1. Seat selected on `eticketing.co.uk/arsenal` (platform UI) → hold/basket on
   platform → checkout/payment on platform → inventory. Club site does not
   allocate seats.
2. First reservation: hold / add-to-basket. Commit: complete purchase / create
   order. (Exact names unknown.)
3. Browser (and likely a mobile WebView/app) call the box office origin. Agents
   will call the same origin with a stolen or issued session.
4. Assumed routes to protect (invented, representative):

   | Route | Action | Control |
   |-------|--------|---------|
   | `GET /api/events`, `GET /api/events/{event}` | search | none |
   | `POST /api/events/{event}/holds` | hold | Bruiser |
   | `POST /api/orders` | purchase | Bruiser |

   Also assumed live, **must be confirmed**, and treated as extra Authority Check
   targets if they exist: mobile API, Ticket Exchange, Ticket Transfer,
   hospitality, partner/reseller, in-stadia box office, GraphQL.

5. Assumed extra hosts: `www.eticketing.co.uk/arsenal`, possible `m.` or app API,
   staging. Authority Check must be told the list; it cannot invent it.

### B. Control over the path

6. Application code at admission point: **platform**. Embedded not viable without
   a Ticketmaster (or successor) integration.
7. Edge: **assumed possible** via whatever WAF/CDN sits in front of the box
   office, or a club-controlled reverse proxy the DNS could point at. Product
   unknown (Cloudflare / Akamai / Fastly / TM's own). This is the highest-leverage
   discovery question.
8. Routing: **assumed possible** for a dedicated allocation hostname if they will
   CNAME it. Less likely for the public `eticketing.co.uk` name.
9. Platform hook: **unknown**. Do not depend on it.
10. Origin lockdown: **required**. Shared secret or mTLS from Edge to origin.
    Who can change TM origin config is unknown — if they cannot, Edge is cosmetic.
11. Lead time: treat as "platform ticket + club change window around fixtures".
    Do not assume a two-week integration.

### C. Identity

12. Email registration + SSO. First box-office login **links SSO to membership
    number**. Protocol: OIDC (tmtickets identity).
13. Authoritative customer id: **7-digit membership number**.
14. Assumed present in a session JWT the Edge can read (`sub` = membership
    number). If the session is opaque, we need introspection — flag as a blocker.
15. Anchors: `membership_no` (same as customer id), `network_id` (optional,
    derived from My Network — **not** enabled in V1 policy until confirmed).
    Payment fingerprint: not assumed (PCI).
16. Bruiser-aware agents: club/IdP can mint a customer assertion with
    `sub` = membership number. Dev HMAC stands in until OIDC JWKS is configured.
17. Delegation: My Network / managed permissions. A carer buying for a Junior is
    still **one membership's execution** unless policy later keys on network.

### D. Scarcity and policy

18. Scarce: Premier League home on-sales, cup ties, North London derby, away
    ballots. Women's on-sales similar but smaller.
19. Per-supporter ticket caps exist today (typically a handful of seats); they
    are **not** Bruiser's job. Bruiser caps concurrent *execution*, not seats.
20. Waiting room assumed in front of Bruiser.
21. V1 policy: **one active purchase/hold execution per membership number per
    event**.
22. Take control: club site or box office banner. Needs a page the club owns;
    `arsenal.com` account area is the likely surface.

### E. Operations

23. Self-hosted in a UK cloud account (AWS as the default guess).
24. Postgres available as RDS/equivalent.
25. Observability: unknown; export Prometheus + JSON logs.
26. Retention 13 months; UK GDPR; DPO at club.
27. Freeze around men's home on-sales.
28. Pilot owner: head of digital / ticketing systems. Authority Check sign-off
    required.

### F. Commercial

29. Public fairness pressure and bot/agent narrative around big on-sales.
30. Pitch: one supporter remains one supporter; EAF as the on-sale number.
31. Core £20k inside fair-use; procurement unknown.
32. Reference: do not assume until a pilot is signed.

---

## Lab merchant `arsenal`

The running software uses this profile as `BRUISER_MERCHANT_ID=arsenal`:

- customer_id = 7-digit membership number (dev examples: `1001234` Alice)
- cookie `boxoffice_session` (JWT, `sub` = membership number)
- Edge `/v1/authorize` + origin lockdown on SimTix (the eTicketing analogue)
- policy `purchase-per-event`: max_active 1, scope customer+resource
- event id `ars-che` as the scarce fixture analogue (Arsenal v Chelsea)

SimTix is not Arsenal's system and not Ticketmaster. It is a box office that
behaves like the public shape above so Edge, transparent enforcement and the
Authority Check have something real to bite.

Runnable in this tree:

```bash
make serve           # Bruiser :8080, profile configs/arsenal.yaml
make simtix          # origin :8090 (lockdown on) + Edge analogue :8091
make authority-check # Overall Result PASS with lockdown; FAIL if SIMTIX_ORIGIN_SECRET is empty
```

`POST /login` on SimTix sets `boxoffice_session` with `sub` = membership
number. Two logins of `1001234` cannot both hold `ars-che`. A copied cookie
can; that is one execution, not a bypass.

---

## What we will not pretend

- That Arsenal is a design partner.
- That we know the real hold/purchase URLs.
- That Ticketmaster will let us sit in front of `eticketing.co.uk` without a
  conversation.
- That My Network is a household id.
- That Embedded is the path for this profile.
