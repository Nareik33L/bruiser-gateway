package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Nareik33L/bruiser-gateway/demos/seed"
)

func (s *server) clubPage(w http.ResponseWriter, r *http.Request, title, body string) {
	s.clubPageClass(w, r, title, "", body)
}

func (s *server) clubPageClass(w http.ResponseWriter, r *http.Request, title, bodyClass, body string) {
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
<body class="%s">
<header class="nav">
  <a class="brand" href="/"><span class="crest">HU</span><span>Harchester United</span></a>
  <nav>
    <a href="/news">News</a>
    <a href="/fixtures">Fixtures</a>
    <a href="/tickets/hfc-ars">Tickets</a>
    <a href="/members">Members</a>
    <a class="nav-cta" href="%s">%s</a>
  </nav>
</header>
%s
<footer>
  <p>Harchester United Football Club · Dragon's Lair · Established 1896</p>
  <p>Fictional club for a closed demonstration. Not affiliated with any real club or broadcast.</p>
</footer>
</body></html>`, title, bodyClass, href, who, body)
}

func (s *server) pageHome(w http.ResponseWriter, r *http.Request) {
	s.clubPage(w, r, "Home", fmt.Sprintf(`
<section class="hero">
  <div class="hero-inner">
    <h1>Members' sale open</h1>
    <p class="sub">Harchester United vs %s · Saturday 15:00 · Dragon's Lair</p>
    <a class="btn" href="/tickets/hfc-ars">Buy tickets</a>
  </div>
</section>
<div class="wrap">
  <section class="section">
    <h2>Latest news</h2>
    <div class="news-grid">
      <a class="news-card" href="/news">
        <div class="news-photo" style="background-image:url('/assets/news-matchday.jpg')"></div>
        <div class="body">
          <p class="kicker">Matchday</p>
          <h3>Dragon's Lair ready for %s — members' sale now live</h3>
          <p class="meta">Today · Tickets</p>
        </div>
      </a>
      <a class="news-card" href="/news">
        <div class="news-photo" style="background-image:url('/assets/news-player.jpg')"></div>
        <div class="body">
          <p class="kicker">First team</p>
          <h3>Squad update ahead of Saturday's clash</h3>
          <p class="meta">Yesterday · News</p>
        </div>
      </a>
      <a class="news-card" href="/members">
        <div class="news-photo brand-tile">HUFC</div>
        <div class="body">
          <p class="kicker">Club</p>
          <h3>Season membership benefits for 2026/27</h3>
          <p class="meta">2 days ago · Members</p>
        </div>
      </a>
    </div>
  </section>
  <section class="section">
    <h2>Fixtures</h2>
    <div class="fixture-strip">
      <div class="left">
        <span class="pill">Next match</span>
        <div>
          <strong>Harchester United vs %s <span class="badge-sale">On sale</span></strong>
          <span>Sat 15:00 · Dragon's Lair · Premier League</span>
        </div>
      </div>
      <a class="btn small" href="/tickets/hfc-ars">Buy tickets</a>
    </div>
  </section>
</div>`, s.opponent, s.opponent, s.opponent))
}

func (s *server) pageNews(w http.ResponseWriter, r *http.Request) {
	s.clubPage(w, r, "News", fmt.Sprintf(`
<div class="wrap page-copy">
<h1>News</h1>
<article class="story"><p class="kicker">Match preview</p><h2>United prepare for a sold-out Lair</h2><p>The members' sale for Saturday's Premier League fixture against %s is open. Season-ticket holders in Gold and Season Ticket tiers have already been written to.</p></article>
<article class="story"><p class="kicker">First team</p><h2>Squad update ahead of Saturday's clash</h2><p>Okafor: "This group knows what the Lair sounds like on a Saturday." Two academy graduates are named in the 21-man squad.</p></article>
<article class="story"><p class="kicker">Club</p><h2>Season membership benefits for 2026/27</h2><p>Priority access to members' sales remains the core benefit. Hospitality is not part of this Saturday's returned-seat release.</p></article>
</div>`, s.opponent))
}

func (s *server) pageFixtures(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString(`<div class="wrap page-copy"><h1>Fixtures</h1><ul class="fixtures">`)
	for _, ev := range seed.Events(s.opponent) {
		badge := "Scheduled"
		link := ""
		if ev.OnSale {
			badge = `<span class="badge-sale">On sale</span>`
			link = ` <a class="btn small" href="/tickets/hfc-ars">Buy tickets</a>`
		}
		if ev.SoldOut {
			badge = "Sold out"
		}
		fmt.Fprintf(&b, `<li><div><strong>%s</strong><span>%s · %s · %s</span></div><div>%s%s</div></li>`,
			ev.Name, ev.Venue, ev.Kickoff, ev.Competition, badge, link)
	}
	b.WriteString(`</ul></div>`)
	s.clubPage(w, r, "Fixtures", b.String())
}

func (s *server) pageMembers(w http.ResponseWriter, r *http.Request) {
	s.clubPage(w, r, "Members", `
<div class="wrap page-copy">
<h1>Membership</h1>
<p class="lede">A Harchester membership is a seven-digit number, issued on registration. It is how the club knows you at the turnstile and in the ticket office.</p>
<p>Junior · Bronze · Silver · Gold · Season Ticket.</p>
<p><a class="btn" href="/login">Member sign in</a></p>
</div>`)
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
	s.clubPageClass(w, r, "Sign in", "is-login", fmt.Sprintf(`
<div class="login-wrap">
  <div class="login-card">
    <div class="crest">HU</div>
    <h1>Member sign in</h1>
    <p class="lede">Access tickets, membership and club benefits</p>
    %s
    <form method="post" action="/login" class="form">
      <input type="hidden" name="next" value="%s">
      <label>Membership number <input name="membership_number" inputmode="numeric" autocomplete="username" placeholder="e.g. 1001234" required></label>
      <label>Password <input type="password" name="password" autocomplete="current-password" placeholder="Enter your password" required></label>
      <button class="btn" type="submit">Sign in</button>
    </form>
    <p class="login-links"><a href="/members">Forgot password?</a> · <a href="/members">Join as a member</a></p>
  </div>
</div>`, errHTML, next))
}

func (s *server) pageAccount(w http.ResponseWriter, r *http.Request) {
	m, ok := s.current(r)
	if !ok {
		http.Redirect(w, r, "/login?next=/account", http.StatusFound)
		return
	}
	s.clubPage(w, r, "My tickets", fmt.Sprintf(`
<div class="wrap page-copy">
<h1>Hello, %s</h1>
<p>Membership %s · %s · %d loyalty points</p>
<h2>My tickets</h2>
%s
<form method="post" action="/logout"><button class="btn ghost" type="submit">Sign out</button></form>
</div>`,
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
		elig = `<p class="ok">You are eligible for this members' sale</p>`
	} else if logged {
		elig = `<p class="banner">Your membership is not eligible for this members' sale.</p>`
	}
	s.clubPage(w, r, "Tickets", fmt.Sprintf(`
<section class="match-hero">
  <div class="match-hero-inner">
    <p class="kicker">Premier League · Match hub</p>
    <h1>Harchester United vs %s</h1>
  </div>
</section>
<div class="wrap match-grid">
  <section class="card">
    <div class="meta-row">
      <p><span class="lbl">Kick-off</span><strong>Saturday 15:00</strong></p>
      <p><span class="lbl">Venue</span><strong>Dragon's Lair</strong></p>
      <p><span class="lbl">Competition</span><strong>Premier League</strong></p>
    </div>
    %s
    <p><a class="btn" href="/tickets/hfc-ars/buy">Buy tickets</a></p>
    <p class="muted">You will complete your purchase with the club's ticketing partner.</p>
  </section>
  <aside class="card">
    <h2>Match details</h2>
    <table class="details">
      <tr><th>Home</th><td>Harchester United</td></tr>
      <tr><th>Away</th><td>%s</td></tr>
      <tr><th>Sale</th><td class="sale-accent">Members only</td></tr>
      <tr><th>From</th><td>£45</td></tr>
      <tr><th>Capacity</th><td>Limited release</td></tr>
    </table>
  </aside>
</div>`, s.opponent, elig, s.opponent))
}
