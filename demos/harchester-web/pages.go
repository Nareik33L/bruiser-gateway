package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Nareik33L/bruiser-gateway/demos/seed"
)

func (s *server) css(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(clubCSS))
}

func (s *server) clubPage(w http.ResponseWriter, r *http.Request, title, body string) {
	m, logged := s.current(r)
	who := "Sign in"
	href := "/login"
	if logged {
		who = m.FirstName
		href = "/account"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s · Harchester United</title><link rel="stylesheet" href="/assets/club.css"></head>
<body>
<header class="nav">
  <a class="brand" href="/"><span class="crest">HUFC</span><span>Harchester United</span></a>
  <nav>
    <a href="/news">News</a>
    <a href="/fixtures">Fixtures</a>
    <a href="/members">Members</a>
    <a href="%s">%s</a>
  </nav>
</header>
<main>%s</main>
<footer>
  <p>Harchester United Football Club · Dragon's Lair · Established 1896</p>
  <p>Fictional club for a closed demonstration. Not affiliated with any real club or broadcast.</p>
</footer>
</body></html>`, title, href, who, body)
}

func (s *server) pageHome(w http.ResponseWriter, r *http.Request) {
	s.clubPage(w, r, "Home", fmt.Sprintf(`
<section class="hero">
  <p class="kicker">Home · Premier League</p>
  <h1>Harchester United</h1>
  <p class="sub">Saturday 15:00 at the Dragon's Lair. Members' sale now open for the visit of %s.</p>
  <a class="btn" href="/tickets/hfc-ars">Buy tickets</a>
</section>
<section class="grid">
  <article><h2>Club news</h2><p>Okafor: "This group knows what the Lair sounds like on a Saturday."</p><a href="/news">Read news</a></article>
  <article><h2>Fixtures</h2><p>Four matches listed, one on sale. Members first.</p><a href="/fixtures">See fixtures</a></article>
  <article><h2>Membership</h2><p>Seven-digit membership. One supporter, one membership.</p><a href="/members">Join</a></article>
</section>`, s.opponent))
}

func (s *server) pageNews(w http.ResponseWriter, r *http.Request) {
	s.clubPage(w, r, "News", `
<h1>News</h1>
<article class="story"><p class="kicker">Match preview</p><h2>United prepare for a sold-out Lair</h2><p>The members' sale for Saturday's Premier League fixture is open. Season-ticket holders in Gold and Season Ticket tiers have already been written to.</p></article>
<article class="story"><p class="kicker">Academy</p><h2>Two academy graduates named in the matchday squad</h2><p>The first-team staff have named a 21-man squad that includes two players who came through the Harchester schoolboys' side.</p></article>
<article class="story"><p class="kicker">Club</p><h2>Dragon's Lair hospitality lounge reopens</h2><p>The West Stand lounge returns for Saturday after a short refurbishment. Hospitality is not part of the members' sale.</p></article>`)
}

func (s *server) pageFixtures(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString(`<h1>Fixtures</h1><ul class="fixtures">`)
	for _, ev := range seed.Events(s.opponent) {
		badge := "Scheduled"
		link := ""
		if ev.OnSale {
			badge = "Members' sale open"
			link = ` <a class="btn small" href="/tickets/hfc-ars">Tickets</a>`
		}
		if ev.SoldOut {
			badge = "Sold out"
		}
		fmt.Fprintf(&b, `<li><strong>%s</strong><span>%s · %s · %s</span><em>%s</em>%s</li>`,
			ev.Name, ev.Venue, ev.Kickoff, ev.Competition, badge, link)
	}
	b.WriteString(`</ul>`)
	s.clubPage(w, r, "Fixtures", b.String())
}

func (s *server) pageMembers(w http.ResponseWriter, r *http.Request) {
	s.clubPage(w, r, "Members", `
<h1>Membership</h1>
<p class="lede">A Harchester membership is a seven-digit number, issued on registration. It is how the club knows you at the turnstile and in the ticket office.</p>
<p>Junior · Bronze · Silver · Gold · Season Ticket.</p>
<p><a class="btn" href="/login">Member sign in</a></p>`)
}

func (s *server) pageLogin(w http.ResponseWriter, r *http.Request) {
	s.renderLogin(w, r, "")
}

func (s *server) renderLogin(w http.ResponseWriter, r *http.Request, errMsg string) {
	next := r.URL.Query().Get("next")
	errHTML := ""
	if errMsg != "" {
		errHTML = `<p class="banner">` + errMsg + `</p>`
	}
	s.clubPage(w, r, "Sign in", fmt.Sprintf(`
<h1>Member sign in</h1>
%s
<form method="post" action="/login" class="form">
<input type="hidden" name="next" value="%s">
<label>Membership number <input name="membership_number" inputmode="numeric" autocomplete="username" required></label>
<label>Password <input type="password" name="password" autocomplete="current-password" required></label>
<button class="btn" type="submit">Sign in</button>
</form>`, errHTML, next))
}

func (s *server) pageAccount(w http.ResponseWriter, r *http.Request) {
	m, ok := s.current(r)
	if !ok {
		http.Redirect(w, r, "/login?next=/account", http.StatusFound)
		return
	}
	s.clubPage(w, r, "My tickets", fmt.Sprintf(`
<h1>Hello, %s</h1>
<p>Membership %s · %s · %d loyalty points</p>
<h2>My tickets</h2>
%s
<form method="post" action="/logout"><button class="btn ghost" type="submit">Sign out</button></form>`,
		m.FirstName, m.MembershipNumber, m.MembershipTier, m.LoyaltyPoints, s.fetchOrders(r, m.MembershipNumber)))
}

func (s *server) fetchOrders(r *http.Request, membership string) string {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(s.simtixInternal, "/")+"/_internal/orders?membership="+membership, nil)
	if err != nil {
		return `<p class="muted">Ticket office unavailable.</p>`
	}
	req.Header.Set("X-Harchester-SSO-Secret", s.sso)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return `<p class="muted">No tickets on this membership yet.</p>`
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return `<p class="muted">No tickets on this membership yet.</p>`
	}
	var body struct {
		Orders []struct {
			ID    string `json:"order_id"`
			Seats int    `json:"seats"`
		} `json:"orders"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || len(body.Orders) == 0 {
		return `<p class="muted">No tickets on this membership yet.</p>`
	}
	var b strings.Builder
	b.WriteString(`<ul class="fixtures">`)
	for _, o := range body.Orders {
		fmt.Fprintf(&b, `<li><strong>%s</strong><span>%d ticket</span></li>`, o.ID, o.Seats)
	}
	b.WriteString(`</ul>`)
	return b.String()
}

func (s *server) pageMatch(w http.ResponseWriter, r *http.Request) {
	m, logged := s.current(r)
	elig := ""
	if logged && m.EligibleForArsenal {
		elig = `<p class="ok">You are eligible for this sale.</p>`
	} else if logged {
		elig = `<p class="banner">Your membership is not eligible for this members' sale.</p>`
	}
	s.clubPage(w, r, "Tickets", fmt.Sprintf(`
<p class="kicker">Premier League · Members' sale</p>
<h1>Harchester United vs %s</h1>
<p class="lede">Saturday 15:00 · Dragon's Lair · 500 returned seats</p>
%s
<p><a class="btn" href="/tickets/hfc-ars/buy">Buy tickets</a></p>
<p class="muted">You will complete your purchase with the club's ticketing partner.</p>`, s.opponent, elig))
}

const clubCSS = `
:root{--bg:#f4efe6;--ink:#1c1917;--muted:#57534e;--purple:#5b2a8c;--orange:#e85d04;--line:#e7e0d4}
*{box-sizing:border-box}html,body{margin:0;background:var(--bg);color:var(--ink);font-family:"Iowan Old Style",Palatino,Georgia,serif}
a{color:var(--purple)}
.nav{display:flex;justify-content:space-between;align-items:center;padding:18px 28px;border-bottom:1px solid var(--line);background:#f7f3eb;position:sticky;top:0}
.brand{display:flex;gap:12px;align-items:center;text-decoration:none;color:var(--ink);font-weight:700;letter-spacing:.04em}
.crest{width:42px;height:42px;border-radius:50%;background:var(--purple);color:#f4efe6;display:grid;place-items:center;font-size:11px;font-weight:800;letter-spacing:.04em}
nav a{margin-left:20px;text-decoration:none;color:var(--ink);font-size:15px}
main{width:min(920px,calc(100% - 40px));margin:0 auto;padding:36px 0 64px}
.hero{padding:48px 0 24px}
.kicker{letter-spacing:.28em;text-transform:uppercase;font-size:11px;color:var(--orange);font-weight:700;font-family:ui-sans-serif,system-ui,sans-serif}
h1{font-size:clamp(2.2rem,6vw,4.2rem);line-height:.95;font-weight:500;margin:8px 0 16px}
.sub,.lede,.muted{color:var(--muted);line-height:1.55}
.btn{display:inline-block;background:var(--purple);color:#f4efe6;text-decoration:none;padding:12px 18px;border-radius:999px;font-family:ui-sans-serif,system-ui,sans-serif;font-weight:700;border:0;cursor:pointer;font-size:14px}
.btn.small{padding:6px 12px;font-size:12px}
.btn.ghost{background:transparent;color:var(--ink);border:1px solid var(--line)}
.grid{display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin-top:32px}
.grid article{background:#fff;border:1px solid var(--line);border-radius:16px;padding:18px}
.story{background:#fff;border:1px solid var(--line);border-radius:16px;padding:20px;margin:16px 0}
.fixtures{list-style:none;padding:0}
.fixtures li{background:#fff;border:1px solid var(--line);border-radius:14px;padding:16px;margin:10px 0}
.fixtures span,.fixtures em{display:block;color:var(--muted);font-style:normal;margin-top:4px;font-family:ui-sans-serif,system-ui,sans-serif;font-size:14px}
.form label{display:block;margin:14px 0;font-family:ui-sans-serif,system-ui,sans-serif}
.form input{width:100%;padding:10px;border:1px solid var(--line);border-radius:10px;background:#fff;font:inherit}
.banner{background:#fff1e6;border:1px solid var(--orange);color:#9a3412;padding:12px;border-radius:10px}
.ok{background:#f3e8ff;color:var(--purple);padding:12px;border-radius:10px}
footer{padding:24px 28px;border-top:1px solid var(--line);color:var(--muted);font-size:13px;font-family:ui-sans-serif,system-ui,sans-serif}
@media(max-width:800px){.grid{grid-template-columns:1fr}nav a{margin-left:12px;font-size:13px}}
`
