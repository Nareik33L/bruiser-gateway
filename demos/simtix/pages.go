package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Nareik33L/bruiser-gateway/demos/seed"
)

func (o *origin) css(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(simtixCSS))
}

func simtixPage(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s · SimTix</title><link rel="stylesheet" href="/assets/simtix.css"></head>
<body>
<header class="nav"><a class="brand" href="/"><span class="mark">S</span> SimTix</a>
<nav><a href="/">Events</a><a href="/basket">Basket</a></nav></header>
<main>%s</main>
<footer><p>SimTix Ltd · Independent ticketing platform · Not affiliated with any club.</p>
<p>Fictional demonstration environment. No real payments are taken.</p></footer>
</body></html>`, title, body)
}

func (o *origin) pageHome(w http.ResponseWriter, _ *http.Request) {
	var b strings.Builder
	b.WriteString(`<h1>Find tickets</h1><p class="lede">Official ticketing partner inventory. Select an event to continue.</p><ul class="cards">`)
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, ev := range o.events {
		status := "Scheduled"
		if ev.OnSale {
			status = fmt.Sprintf("%d available", ev.Available())
		}
		if ev.SoldOut {
			status = "Sold out"
		}
		fmt.Fprintf(&b, `<li><a href="/events/%s"><strong>%s</strong><span>%s · %s</span><em>%s</em></a></li>`,
			ev.ID, ev.Name, ev.Venue, ev.Kickoff, status)
	}
	b.WriteString(`</ul>`)
	simtixPage(w, "Events", b.String())
}

func (o *origin) pageEvent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "event")
	o.mu.Lock()
	ev, ok := o.events[id]
	avail := 0
	name := ""
	onSale := false
	venue, kick, comp := "", "", ""
	if ok {
		o.expireLocked(time.Now())
		avail = ev.Available()
		name = ev.Name
		onSale = ev.OnSale
		venue, kick, comp = ev.Venue, ev.Kickoff, ev.Competition
	}
	o.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	notice := ""
	if r.URL.Query().Get("ineligible") == "1" {
		notice = `<p class="banner">This members' sale is not available on your membership.</p>`
	}
	cta := `<p class="muted">Sale not open.</p>`
	if onSale {
		cta = fmt.Sprintf(`<a class="btn" href="/events/%s/seats">Select seats</a>`, id)
	}
	simtixPage(w, name, fmt.Sprintf(`%s
<p class="kicker">SimTix checkout</p>
<h1>%s</h1>
<p class="lede">%s · %s · %s</p>
<p>%d seats remaining in this sale.</p>
%s`, notice, name, venue, kick, comp, avail, cta))
}

func (o *origin) pageSeats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "event")
	o.mu.Lock()
	ev, ok := o.events[id]
	o.mu.Unlock()
	if !ok || !ev.OnSale {
		http.NotFound(w, r)
		return
	}
	var blocks strings.Builder
	for _, b := range ev.Blocks {
		fmt.Fprintf(&blocks, `<label class="block"><input type="radio" name="block" value="%s"> <strong>%s</strong> · %s · £%d</label>`,
			b.ID, b.Name, b.PriceBand, b.PricePence/100)
	}
	simtixPage(w, "Select seats", fmt.Sprintf(`
<h1>Seat allocation</h1>
<p class="lede">Best available will be assigned in the selected stand.</p>
<form id="hold-form">
%s
<button class="btn" type="submit">Best available</button>
<p id="msg" class="muted"></p>
</form>
<script>
const form = document.getElementById('hold-form');
const msg = document.getElementById('msg');
form.addEventListener('submit', async (e) => {
  e.preventDefault();
  const block = (new FormData(form)).get('block') || 'best';
  msg.textContent = 'Reserving…';
  const res = await fetch('/api/events/%s/holds', {
    method: 'POST', credentials: 'same-origin',
    headers: {'Content-Type':'application/json'},
    body: JSON.stringify({seats:1, block_id: block})
  });
  const body = await res.json().catch(() => ({}));
  if (res.status === 409 && (body.status === 'BUSY' || (body.error||'').toLowerCase().includes('busy'))) {
    msg.textContent = 'You already have a reservation in progress on another device.';
    return;
  }
  if (!res.ok) { msg.textContent = body.detail || body.error || ('Could not reserve ('+res.status+')'); return; }
  sessionStorage.setItem('hold_id', body.hold_id);
  sessionStorage.setItem('event_id', body.event_id);
  location.href = '/basket';
});
</script>`, blocks.String(), id))
}

func (o *origin) pageBasket(w http.ResponseWriter, _ *http.Request) {
	simtixPage(w, "Basket", `
<h1>Basket</h1>
<p>1 × members' sale ticket. Hold expires in two minutes.</p>
<a class="btn" href="/checkout">Continue to checkout</a>`)
}

func (o *origin) pageCheckout(w http.ResponseWriter, r *http.Request) {
	name := ""
	if c, err := r.Cookie("simtix_name"); err == nil {
		name = c.Value
	}
	simtixPage(w, "Checkout", fmt.Sprintf(`
<h1>Checkout</h1>
<form id="pay" class="form">
<label>Name <input name="name" value="%s" required></label>
<label>Card number <input name="card" value="4242424242424242" required></label>
<label>Expiry <input name="exp" value="12/28" required></label>
<label>CVC <input name="cvc" value="123" required></label>
<p class="muted">Fictional payment form. No processor is contacted.</p>
<button class="btn" type="submit">Pay and confirm</button>
<p id="msg" class="muted"></p>
</form>
<script>
document.getElementById('pay').addEventListener('submit', async (e) => {
  e.preventDefault();
  const hold = sessionStorage.getItem('hold_id');
  const eventId = sessionStorage.getItem('event_id') || '%s';
  const msg = document.getElementById('msg');
  if (!hold) { msg.textContent = 'Your reservation expired.'; return; }
  const res = await fetch('/api/orders', {
    method: 'POST', credentials: 'same-origin',
    headers: {'Content-Type':'application/json'},
    body: JSON.stringify({event_id: eventId, hold_id: hold})
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) { msg.textContent = body.error || 'Could not complete order'; return; }
  location.href = '/confirmation/' + body.order_id;
});
</script>`, name, seed.HeadlineEventID))
}

func (o *origin) postCheckout(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/checkout", http.StatusSeeOther)
}

func (o *origin) pageConfirm(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "order")
	o.mu.Lock()
	ord, ok := o.orders[id]
	o.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	simtixPage(w, "Confirmation", fmt.Sprintf(`
<p class="kicker">Order confirmed</p>
<h1>You're going.</h1>
<p>Reference <strong>%s</strong> · %d ticket · Membership %s</p>
<p class="ticket">Harchester United vs %s<br>Dragon's Lair · Saturday 15:00<br>Print this page. No app required.</p>`,
		ord.ID, ord.Seats, ord.MembershipNo, o.cfg.Opponent))
}

const simtixCSS = `
:root{--bg:#0f172a;--ink:#e2e8f0;--muted:#94a3b8;--card:#1e293b;--accent:#14b8a6;--line:#334155}
*{box-sizing:border-box}html,body{margin:0;background:var(--bg);color:var(--ink);font-family:ui-sans-serif,system-ui,sans-serif}
a{color:var(--accent);text-decoration:none}
.nav{display:flex;justify-content:space-between;align-items:center;padding:16px 24px;border-bottom:1px solid var(--line);background:#0b1222}
.brand{display:flex;gap:10px;align-items:center;color:var(--ink);font-weight:800;letter-spacing:.12em;text-transform:uppercase;font-size:13px}
.mark{width:28px;height:28px;border-radius:6px;background:var(--accent);color:#042f2e;display:grid;place-items:center;font-weight:900}
nav a{margin-left:18px;color:var(--muted)}
main{width:min(820px,calc(100% - 40px));margin:36px auto}
h1{font-size:2.1rem;margin:0 0 8px}
.lede,.muted{color:var(--muted)}
.kicker{letter-spacing:.2em;text-transform:uppercase;font-size:11px;color:var(--accent);font-weight:700}
.btn{display:inline-block;background:var(--accent);color:#042f2e;font-weight:800;padding:12px 18px;border-radius:8px;border:0;cursor:pointer;font:inherit}
.cards{list-style:none;padding:0;display:grid;gap:12px}
.cards a{display:block;background:var(--card);border:1px solid var(--line);border-radius:12px;padding:16px;color:var(--ink)}
.cards span,.cards em{display:block;color:var(--muted);font-size:14px;font-style:normal;margin-top:4px}
.block{display:block;background:var(--card);border:1px solid var(--line);border-radius:10px;padding:12px;margin:8px 0}
.form label{display:block;margin:12px 0;color:var(--muted)}
.form input{width:100%;padding:10px;border-radius:8px;border:1px solid var(--line);background:#0b1222;color:var(--ink)}
.banner{background:#7f1d1d;color:#fecaca;padding:12px 14px;border-radius:8px}
.ticket{margin-top:24px;border:2px dashed var(--accent);padding:20px;border-radius:12px;line-height:1.6}
footer{padding:24px;color:var(--muted);font-size:12px;border-top:1px solid var(--line);margin-top:48px}
`
