package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Nareik33L/bruiser-gateway/demos/seed"
)

func simtixPage(w http.ResponseWriter, title, body string) {
	simtixPageClass(w, title, "", body)
}

func simtixPageClass(w http.ResponseWriter, title, bodyClass, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s · SimTix</title><link rel="stylesheet" href="/assets/simtix.css"></head>
<body class="%s">
<header class="nav"><a class="brand" href="/">SimTix</a>
<nav><a href="/">Help</a><a href="/basket">My tickets</a><a href="/">Sign in</a></nav></header>
%s
<footer><p>SimTix Ltd · Independent ticketing platform · Not affiliated with any club.</p>
<p>Fictional demonstration environment. No real payments are taken.</p></footer>
</body></html>`, title, bodyClass, body)
}

func (o *origin) pageHome(w http.ResponseWriter, _ *http.Request) {
	var b strings.Builder
	b.WriteString(`<div class="wrap page-copy"><h1>Find tickets</h1><p class="lede">Official ticketing partner inventory. Select an event to continue.</p><ul class="cards">`)
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
	b.WriteString(`</ul></div>`)
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
	left := "Limited seats remaining"
	if avail == 0 {
		left = "No seats remaining"
	}
	cta := `<p class="muted">Sale not open.</p>`
	if onSale {
		cta = fmt.Sprintf(`<a class="btn" href="/events/%s/seats">Find tickets</a>`, id)
	}
	simtixPage(w, name, fmt.Sprintf(`
<p class="crumb"><a href="/">Football</a> / %s / %s</p>
%s
<div class="wrap event-layout">
  <div class="event-shell">
    <div class="event-hero">
      <div class="event-hero-inner">
        <p class="kicker">%s · Home fixture</p>
        <h1>%s</h1>
        <p>%s · Saturday · Kick-off 15:00</p>
      </div>
    </div>
    <div class="event-meta">
      <p><span class="lbl">Venue</span><strong>%s</strong></p>
      <p><span class="lbl">Date</span><strong>%s</strong></p>
      <p><span class="lbl">Category</span><strong>Members' sale</strong></p>
    </div>
    <div class="event-actions">%s</div>
  </div>
  <aside class="tickets-card">
    <h2>Tickets</h2>
    <div class="price-box"><span class="lbl">Prices from</span><strong>£45</strong></div>
    <p class="seats-left">%s</p>
    %s
    <p class="fine">Official ticketing partner for this fixture. All sales subject to club allocation rules.</p>
  </aside>
</div>`, comp, name, notice, comp, name, venue, venue, kick, cta, left, cta))
}

func westSeatDots() (string, string) {
	var b strings.Builder
	selected := "W-L-14-82"
	for row := 0; row < 12; row++ {
		for col := 0; col < 3; col++ {
			x := 56 + col*14
			y := 168 + row*15
			id := fmt.Sprintf("W-L-%d-%d", 10+row, 80+col)
			fill := "#93c5fd"
			cls := "seat"
			if id == selected {
				fill = "#4ade80"
				cls = "seat picked"
			}
			fmt.Fprintf(&b, `<circle class="%s" data-seat="%s" data-block="east-family" data-stand="West Lower" cx="%d" cy="%d" r="5" fill="%s"/>`,
				cls, id, x, y, fill)
		}
	}
	return b.String(), selected
}

func standDots(x0, y0, cols, rows int, fill string) string {
	var b strings.Builder
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			fmt.Fprintf(&b, `<circle class="seat taken" cx="%d" cy="%d" r="4.5" fill="%s"/>`, x0+col*14, y0+row*12, fill)
		}
	}
	return b.String()
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
	west, selected := westSeatDots()
	simtixPageClass(w, "Select seats", "is-seats", fmt.Sprintf(`
<div class="seats-page">
  <div class="seats-main">
    <p class="crumb" style="padding:0 0 8px;width:auto;margin:0"><a href="/events/%s">Event</a> / Select seats</p>
    <div style="display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap">
      <h1>Dragon's Lair — seat map</h1>
      <div class="legend">
        <span><i style="background:#5b2a8c"></i>Unavailable</span>
        <span><i style="background:#3b82f6"></i>Available</span>
        <span><i style="background:#22c55e"></i>Selected</span>
      </div>
    </div>
    <div class="bowl-wrap">
      <svg class="bowl" viewBox="0 0 640 540" role="img" aria-label="Dragon's Lair seat map">
        <rect x="24" y="16" width="592" height="508" rx="86" fill="#e8eef7"/>
        <rect x="168" y="158" width="304" height="224" rx="10" fill="#3a9d4e"/>
        <rect x="178" y="168" width="284" height="204" fill="none" stroke="#fff" stroke-width="2"/>
        <circle cx="320" cy="270" r="34" fill="none" stroke="#fff" stroke-width="2"/>
        <line x1="168" y1="270" x2="472" y2="270" stroke="#fff" stroke-width="2"/>
        <circle cx="320" cy="270" r="3.5" fill="#fff"/>
        <text x="320" y="274" text-anchor="middle" fill="#fff" font-size="11" font-family="system-ui" font-weight="700">PITCH</text>
        <rect x="186" y="36" width="268" height="96" rx="18" fill="#5b2a8c"/>
        <text x="320" y="90" text-anchor="middle" fill="#fff" font-size="13" font-family="system-ui" font-weight="800">NORTH</text>
        %s
        <rect x="40" y="148" width="108" height="244" rx="18" fill="#026CDF"/>
        <text x="94" y="278" text-anchor="middle" fill="#fff" font-size="12" font-family="system-ui" font-weight="800" transform="rotate(-90 94 278)">WEST LOWER</text>
        %s
        <rect x="492" y="162" width="108" height="216" rx="18" fill="#5b2a8c"/>
        <text x="546" y="278" text-anchor="middle" fill="#fff" font-size="13" font-family="system-ui" font-weight="800" transform="rotate(90 546 278)">EAST</text>
        %s
        <rect x="186" y="408" width="268" height="92" rx="18" fill="#0B1B3D"/>
        <text x="320" y="462" text-anchor="middle" fill="#fff" font-size="13" font-family="system-ui" font-weight="800">SOUTH</text>
        %s
      </svg>
    </div>
  </div>
  <aside class="rail">
    <div class="hold-pill" id="hold-timer">Hold 02:00</div>
    <h2>Your selection</h2>
    <p class="sub">Harchester United vs %s · Sat 15:00</p>
    <div class="fact"><span class="lbl">Stand</span><strong id="stand-label">West Lower</strong></div>
    <div class="fact"><span class="lbl">Seats</span><strong id="qty-label">1 ticket</strong><div class="chip" id="seat-chip">%s</div></div>
    <div class="total-row"><span>Total</span><span id="price-label">£45</span></div>
    <p id="msg" class="muted"></p>
    <button class="btn wide" type="button" id="continue-btn">Continue to checkout</button>
    <p class="fine" style="text-align:center">Seats held for a limited time</p>
  </aside>
</div>
<script>
(function () {
  var selected = '%s';
  var blockId = 'east-family';
  var remain = 120;
  var timer = document.getElementById('hold-timer');
  setInterval(function () {
    remain = Math.max(0, remain - 1);
    var m = String(Math.floor(remain / 60)).padStart(2, '0');
    var s = String(remain %% 60).padStart(2, '0');
    timer.textContent = 'Hold ' + m + ':' + s;
  }, 1000);
  document.querySelectorAll('.seat:not(.taken)').forEach(function (el) {
    el.addEventListener('click', function () {
      document.querySelectorAll('.seat.picked').forEach(function (p) {
        p.classList.remove('picked');
        p.setAttribute('fill', '#93c5fd');
      });
      el.classList.add('picked');
      el.setAttribute('fill', '#4ade80');
      selected = el.getAttribute('data-seat');
      blockId = el.getAttribute('data-block') || 'east-family';
      document.getElementById('seat-chip').textContent = selected;
      document.getElementById('stand-label').textContent = el.getAttribute('data-stand') || 'West Lower';
    });
  });
  document.getElementById('continue-btn').addEventListener('click', async function () {
    var msg = document.getElementById('msg');
    msg.textContent = 'Reserving…';
    var res = await fetch('/api/events/%s/holds', {
      method: 'POST', credentials: 'same-origin',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({seats:1, block_id: blockId})
    });
    var body = await res.json().catch(function () { return {}; });
    if (res.status === 409 && (body.status === 'BUSY' || (body.error||'').toLowerCase().includes('busy'))) {
      msg.textContent = 'You already have a reservation in progress on another device.';
      return;
    }
    if (!res.ok) { msg.textContent = body.detail || body.error || ('Could not reserve ('+res.status+')'); return; }
    sessionStorage.setItem('hold_id', body.hold_id);
    sessionStorage.setItem('event_id', body.event_id);
    sessionStorage.setItem('seat_label', selected);
    sessionStorage.setItem('stand_label', document.getElementById('stand-label').textContent);
    location.href = '/checkout';
  });
})();
</script>`, id, standDots(208, 48, 16, 5, "#c4b5fd"), west, standDots(514, 178, 5, 14, "#c4b5fd"), standDots(208, 422, 16, 5, "#334155"), o.cfg.Opponent, selected, selected, id))
}

func (o *origin) pageBasket(w http.ResponseWriter, _ *http.Request) {
	simtixPage(w, "Basket", `
<div class="wrap page-copy">
  <p class="crumb"><a href="/">Event</a> / Seats / Basket</p>
  <h1>Your tickets</h1>
  <p class="lede">1 × members' sale ticket. Hold expires in two minutes.</p>
  <p><a class="btn" href="/checkout">Continue to checkout</a></p>
</div>`)
}

func (o *origin) pageCheckout(w http.ResponseWriter, r *http.Request) {
	name := ""
	if c, err := r.Cookie("simtix_name"); err == nil {
		name = c.Value
	}
	if name == "" {
		name = "Alex Morgan"
	}
	simtixPage(w, "Checkout", fmt.Sprintf(`
<p class="crumb wrap"><a href="/">Event</a> / <a href="/basket">Seats</a> / Checkout</p>
<div class="wrap checkout-grid">
  <section class="pay-card">
    <h1>Payment</h1>
    <p class="lede">Complete your order securely with SimTix</p>
    <form id="pay" class="form">
      <label>Name on card <input name="name" value="%s" required></label>
      <label>Card number <input name="card" value="4242 4242 4242 4242" required></label>
      <div class="row-2">
        <label>Expiry <input name="exp" value="09 / 28" required></label>
        <label>CVC <input name="cvc" value="123" required></label>
      </div>
      <label>Email for tickets <input name="email" type="email" value="you@example.com" required></label>
      <p class="lock">Secured checkout · fictional demo card details only</p>
      <button class="btn wide" type="submit">Pay and confirm</button>
      <p id="msg" class="muted"></p>
    </form>
  </section>
  <aside class="sum-card">
    <h2>Order summary</h2>
    <div class="summary-row"><span>Event</span><strong>HUFC vs %s</strong></div>
    <div class="summary-row"><span>Venue</span><strong>Dragon's Lair</strong></div>
    <div class="summary-row"><span>Date</span><strong>Sat 15:00</strong></div>
    <div class="summary-row"><span>1 ticket <em class="seat-chip" id="sum-seat">W-L-14-82 · West Lower</em></span><strong>£45.00</strong></div>
    <div class="summary-row"><span>Booking fee</span><strong>£0.00</strong></div>
    <div class="summary-row"><span>Total</span><strong style="font-size:1.35rem">£45</strong></div>
    <p class="fine">By confirming you agree to SimTix terms and the club's ground regulations. This is a design mockup — no payment is processed.</p>
  </aside>
</div>
<script>
(function () {
  var seat = sessionStorage.getItem('seat_label');
  var stand = sessionStorage.getItem('stand_label') || 'West Lower';
  if (seat) {
    document.getElementById('sum-seat').textContent = seat + ' · ' + stand;
  }
  document.getElementById('pay').addEventListener('submit', async function (e) {
    e.preventDefault();
    var hold = sessionStorage.getItem('hold_id');
    var eventId = sessionStorage.getItem('event_id') || '%s';
    var msg = document.getElementById('msg');
    if (!hold) { msg.textContent = 'Your reservation expired.'; return; }
    var res = await fetch('/api/orders', {
      method: 'POST', credentials: 'same-origin',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({event_id: eventId, hold_id: hold})
    });
    var body = await res.json().catch(function () { return {}; });
    if (!res.ok) { msg.textContent = body.error || 'Could not complete order'; return; }
    location.href = '/confirmation/' + body.order_id;
  });
})();
</script>`, name, o.cfg.Opponent, seed.HeadlineEventID))
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
<div class="wrap page-copy">
<p class="kicker" style="color:#026CDF;font-weight:800;letter-spacing:.14em;text-transform:uppercase;font-size:11px">Order confirmed</p>
<h1>You're going.</h1>
<p>Reference <strong>%s</strong> · %d ticket · Membership %s</p>
<p class="ticket">Harchester United vs %s<br>Dragon's Lair · Saturday 15:00<br>Print this page. No app required.</p>
</div>`,
		ord.ID, ord.Seats, ord.MembershipNo, o.cfg.Opponent))
}
