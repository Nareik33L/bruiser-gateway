# Integration Discovery Questionnaire

Used in design-partner discovery calls. The purpose is to answer one question per
club: **is there a technically viable path to make Bruiser authoritative at the
scarce-inventory admission point, and which enforcement pattern is it?** A club
with no viable path is deferred with its requirements recorded — never force-fitted.

Secondary purposes: learn which identity anchors exist (feeds policy design), and
learn the club's operational constraints (feeds deployment and support scope).

Run it with the head of digital/IT or their lead engineer present; the ticketing
office answers the commercial and fairness questions. Fill in the summary at the
end during the call.

---

## A. The admission point

The admission point is the operation that reserves or consumes scarce inventory.
Bruiser must be authoritative over it. Everything else follows from mapping it.

1. Walk us through a purchase from "seat selected" to "order confirmed". Which
   system performs each step (club-built, platform, third party)?
2. Which operation first *reserves* inventory (basket add, hold, seat lock)? Which
   operation *commits* it (order create, payment confirm)?
3. Where does that reservation call originate — the browser, a club server, the
   platform's own front end, a mobile app, a partner API?
4. List every route that can reach the reservation or commit operations:
   - web checkout
   - mobile app API
   - legacy / previous-version APIs still live
   - GraphQL or other aggregate endpoints
   - partner, agency, hospitality or reseller APIs
   - internal box-office tools that use the same endpoints
   - the platform's own hosted checkout (if separate hostname)
5. Are there alternative hostnames, origin IPs or environments (staging exposed to
   the internet, regional edges) that reach the same origin?

## B. Control over the path

6. Who owns and can change the **application code** at the admission point?
   (Club → P1 possible.)
7. Is there a **reverse proxy, API gateway, CDN or WAF** the club controls in front
   of the admission point? Which product (Cloudflare, Akamai, Fastly, AWS ALB/API
   Gateway, Azure Front Door, NGINX, Envoy, Kong, Tyk…)? Can it call an external
   authorisation endpoint or run a worker? (Club → P2 possible.)
8. Can the club **route** the admission-point hostnames or paths (DNS, network
   policy, allowlists) so that they are reachable only via a component the club
   deploys? (Club → P3 possible.)
9. Does the ticketing platform expose a **supported pre-allocation hook**, webhook,
   plugin point or external-authorisation feature? Documentation available?
10. Can the origin be configured to **refuse allocation requests that did not pass
    the enforcement point** (shared secret header, mTLS, IP allowlist, network
    policy)? Who would make that change?
11. What is the change process and lead time for each of the above (platform
    vendor ticket, internal sprint, contract change)?

## C. Identity

12. How do supporters authenticate? Club-run IdP, platform-run login, social login,
    SSO? Which protocol (OIDC, SAML, proprietary cookie session)?
13. What identifier represents a supporter across systems? Membership number,
    supporter ID, CRM ID, platform account ID? Which is authoritative and stable?
14. Is the identifier present in the session credential that reaches the admission
    point (a JWT claim, a cookie, a header set by the edge)? If the session is
    opaque, is there an introspection endpoint?
15. Which **anchors** exist and could be asserted: membership number, household /
    address group, family ticket links, payment-method fingerprint, season-ticket
    holder status, membership tier?
16. Can the club sign a customer assertion for Bruiser-aware clients (issue a JWT
    with the supporter ID as `sub`)? Who owns the IdP configuration?
17. Do supporters already delegate to third parties (carers, family, agencies)?
    How is that represented today?

## D. Scarcity and policy

18. Which sales are genuinely scarce (big matches, cup finals, away allocations,
    member presales)? Roughly how many concurrent supporters at peak?
19. What per-supporter limits exist today (tickets per account, per membership,
    per household)? Are they enforced atomically, or have duplicates slipped through
    under load?
20. Is there a **waiting room** or virtual queue (Queue-it, Cloudflare Waiting Room,
    platform-native)? Bruiser sits behind it; confirm the order of components.
21. How would the club want to express its policy in Bruiser's terms: one active
    execution per supporter per event? per household? different rules for members?
22. How should a supporter "take control" from their agent — on the club site, in
    the app, both? Who owns that UI?

## E. Operations and deployment

23. Where would Bruiser run: club cloud account (which provider), platform-hosted,
    on-premise, managed Kubernetes? Who operates it?
24. Is PostgreSQL available (managed service acceptable)? Who owns it?
25. Observability stack (Prometheus/Grafana, Datadog, New Relic, Splunk…)? Where
    should audit events be exported?
26. Security requirements: SSO for admin, RBAC roles needed, penetration-test
    expectations, data-residency constraints, retention requirements (default
    13 months acceptable?).
27. Change windows and freeze periods around fixtures and on-sales.
28. Who is the technical owner during a pilot? Who signs off the authority check?

## F. Commercial and fairness

29. What does the ticketing office experience today: complaints about bots,
    duplicate purchases, disputes, chargebacks, allocations exhausted in seconds?
30. Which of these would be worth the most: fewer duplicate allocations, lower
    platform load during on-sales, an audit trail for disputes, control of agent
    access as a stated policy to supporters?
31. Who signs a £20k Core agreement, and what procurement steps apply?
32. Would the club act as a reference if the pilot succeeds?

---

## Summary (complete during the call)

| Field | Answer |
|-------|--------|
| Club | |
| Ticketing platform / checkout owner | |
| Admission-point operations (reserve / commit) | |
| Known routes to admission point (from A4/A5) | |
| Control: app code (P1) / edge (P2) / routing (P3) / platform hook | |
| Origin lockdown feasible? Owner and lead time | |
| **Viable enforcement pattern** | P1 / P2 / P3 / platform hook / **none** |
| Supporter identifier and where it appears in the session | |
| Anchors available | |
| Waiting room present? | |
| Deployment target and Postgres availability | |
| Pilot technical owner | |
| Blockers | |
| **Qualification** | Design partner / later / deferred (reason) |

## What happens next per outcome

- **P1 viable:** send the SDK integration guide for their language; agree the route
  list; schedule staging pilot after M3.
- **P2 viable:** confirm the edge product; send the matching reference config;
  agree route rules and the origin lockdown mechanism.
- **P3 viable:** agree the routed hostnames/paths, network policy and HA
  expectations; note that Bruiser is in the data path here.
- **Platform hook:** obtain documentation; assess whether it fits P2 or needs an
  adapter variant; record as a requirement for the Core roadmap.
- **None viable:** record requirements and blockers; ask what would need to change
  (usually a platform feature or a contract renewal); stay in touch.
