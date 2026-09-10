# Harchester demo — locked visual brief

Kieran locked the club and box-office look for the Compose demo. Restyle
those two surfaces only. Do not restyle the admin ops console, load-lab
Agent Lab, or gateway.

Reference comps (design source): club home, login, match hub; SimTix event
landing, seat map, checkout.

## Harchester United (`demos/harchester-web`)

Contemporary Premier League club site (Arsenal.com-like density). **Harchester
purple.** Zero Bruiser branding. Opponent remains `DEMO_OPPONENT` (Arsenal).

| Token | Value |
|-------|--------|
| Primary | `#4C1D7A` |
| Nav | `#0B0B0F` |
| Page | white / soft gray `#F3F1F5` |
| Sale accent | `#E85D04` (sparingly) |
| Type | `system-ui` |

Surfaces: sticky dark nav (HU crest disc + Harchester United; News, Fixtures,
Tickets, Members, Sign in); full-bleed hero; members' sale home; membership
login card; match hub with eligibility banner and partner-purchase note.

Photography lives in `demos/harchester-web/static/` (`hero-stadium.jpg`,
`news-matchday.jpg`, `news-player.jpg`, `match-banner.jpg`).

## SimTix (`demos/simtix`)

Independent Ticketmaster-like box office. **SimTix brand only** — not club
chrome.

| Token | Value |
|-------|--------|
| Navy | `#0B1B3D` |
| Accent | `#026CDF` |
| UI | light commercial |

Surfaces: event landing (breadcrumb, from £45, Find tickets); top-down SVG
stadium bowl (available seats blue, selected green); checkout with order
summary and fictional card form.

Event still: `demos/simtix/static/event-hero.jpg`.

Hold → checkout still posts `POST /api/events/{id}/holds` then
`POST /api/orders`. SSO handoff and Edge authorize are unchanged.

## Admin console (do not restyle here)

Ivory `#f3eee4` / charcoal `#1c1917` / rust `#b44a32`. Presenter control
stays **Per-customer limit Off · Dry run · On**.
