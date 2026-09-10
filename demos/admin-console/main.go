package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Nareik33L/bruiser-gateway/demos/seed"
	"github.com/Nareik33L/bruiser-gateway/demos/shared"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	s := &console{
		log:            log,
		addr:           shared.Env("ADMIN_HTTP_ADDR", ":8110"),
		password:       shared.Env("DEMO_ADMIN_PASSWORD", "harchester"),
		adminSecret:    shared.Env("DEMO_ADMIN_SECRET", "demo-admin-dev"),
		operatorSecret: shared.Env("BRUISER_OPERATOR_SECRET", ""),
		gateway:        shared.Env("BRUISER_URL", "http://127.0.0.1:8080"),
		simtixOrigin:   shared.Env("SIMTIX_ORIGIN_URL", "http://127.0.0.1:8090"),
		simtixEdge:     shared.Env("SIMTIX_EDGE_URL", "http://127.0.0.1:8091"),
		loadlab:        shared.Env("LOADLAB_URL", "http://127.0.0.1:8120"),
		harchester:     shared.Env("HARCHESTER_URL", "http://127.0.0.1:8100"),
		bruiserBin:     shared.Env("BRUISER_BIN", "bin/bruiser"),
	}
	srv := &http.Server{Addr: s.addr, Handler: s.routes(), ReadHeaderTimeout: 5 * time.Second}
	log.Info("admin console listening", "addr", s.addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

type console struct {
	log            *slog.Logger
	addr           string
	password       string
	adminSecret    string
	operatorSecret string
	gateway        string
	simtixOrigin   string
	simtixEdge     string
	loadlab        string
	harchester     string
	bruiserBin     string
}

func (c *console) routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		shared.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "admin"})
	})
	r.Get("/login", c.loginPage)
	r.Post("/login", c.login)
	r.Group(func(r chi.Router) {
		r.Use(c.requireAuth)
		r.Get("/", c.home)
		r.Get("/events", c.sse)
		r.Post("/reset", c.reset)
		r.Post("/enforcement", c.enforcement)
		r.Post("/swarm", c.swarm)
		r.Post("/swarm/stop", c.swarmStop)
		r.Post("/authority-check", c.authority)
		r.Get("/audit", c.audit)
		r.Get("/executions", c.executions)
	})
	return r
}

func (c *console) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie("admin_session")
		if err != nil || subtle.ConstantTimeCompare([]byte(ck.Value), []byte(c.password+"-ok")) != 1 {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (c *console) loginPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, pageShell("Sign in", `<form method="post" class="form"><label>Password <input type="password" name="password" required></label><button type="submit">Enter</button></form>`))
}

func (c *console) login(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if subtle.ConstantTimeCompare([]byte(r.FormValue("password")), []byte(c.password)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "admin_session", Value: c.password + "-ok", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (c *console) home(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, pageShell("Operations", dashboardHTML))
}

func (c *console) sse(w http.ResponseWriter, r *http.Request) {
	shared.SSEHeaders(w)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	send := func() {
		_ = shared.SSEEvent(w, "snapshot", c.snapshot())
	}
	send()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			send()
		}
	}
}

func (c *console) snapshot() map[string]any {
	out := map[string]any{}
	out["gateway"] = c.getJSON(c.gateway+"/v1/operator/stats", map[string]string{"X-Bruiser-Operator-Secret": c.operatorSecret})
	out["simtix"] = c.getJSON(c.simtixOrigin+"/_admin/stats", map[string]string{"X-Demo-Admin-Secret": c.adminSecret})
	out["edge"] = c.getJSON(c.simtixEdge+"/_edge/config", map[string]string{"X-Demo-Admin-Secret": c.adminSecret})
	out["edge_stats"] = c.getJSON(c.simtixEdge+"/_edge/stats", map[string]string{"X-Demo-Admin-Secret": c.adminSecret})
	out["loadlab"] = c.getJSON(c.loadlab+"/status", map[string]string{"X-Demo-Admin-Secret": c.adminSecret})
	out["club"] = c.getJSON(c.harchester+"/_admin/online", map[string]string{"X-Demo-Admin-Secret": c.adminSecret})
	return out
}

func (c *console) getJSON(url string, headers map[string]string) any {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	defer resp.Body.Close()
	var v any
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if json.Unmarshal(b, &v) != nil {
		return map[string]string{"error": string(b)}
	}
	return v
}

func (c *console) post(url string, headers map[string]string, body any) map[string]any {
	return c.call(http.MethodPost, url, headers, body)
}

func (c *console) put(url string, headers map[string]string, body any) map[string]any {
	return c.call(http.MethodPut, url, headers, body)
}

func (c *console) call(method, url string, headers map[string]string, body any) map[string]any {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return map[string]any{"status": resp.Status, "raw": string(raw)}
	}
	return out
}

func (c *console) reset(w http.ResponseWriter, _ *http.Request) {
	h := map[string]string{"X-Demo-Admin-Secret": c.adminSecret}
	sim := c.post(c.simtixOrigin+"/_admin/reset", h, nil)
	c.post(c.simtixEdge+"/_edge/reset-stats", h, nil)
	c.post(c.loadlab+"/reset", h, nil)
	c.post(c.harchester+"/_admin/reset-sessions", h, nil)
	gw := c.post(c.gateway+"/v1/operator/reset-executions", map[string]string{"X-Bruiser-Operator-Secret": c.operatorSecret}, nil)
	seats := seed.HeadlineSeats
	if v, ok := sim["available"].(float64); ok {
		seats = int(v)
	} else if v, ok := sim["seats"].(float64); ok {
		seats = int(v)
	}
	revoked := 0
	if v, ok := gw["revoked"].(float64); ok {
		revoked = int(v)
	}
	msg := fmt.Sprintf("Demo reset: %d seats remaining, executions cleared (%d revoked), customers online cleared.", seats, revoked)
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"status":          "reset",
		"seats_remaining": seats,
		"revoked":         revoked,
		"message":         msg,
	})
}

func (c *console) enforcement(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode    string `json:"mode"`
		Percent int    `json:"percent"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	h := map[string]string{"X-Demo-Admin-Secret": c.adminSecret}
	c.post(c.simtixEdge+"/_edge/config", h, body)
	cap := seed.PerAccountLimit
	if body.Mode == "off" {
		cap = 0
	}
	c.put(c.simtixOrigin+"/_admin/per-account-cap", h, map[string]int{"limit": cap})
	shared.WriteJSON(w, http.StatusOK, body)
}

func (c *console) swarm(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	c.post(c.loadlab+"/run", map[string]string{"X-Demo-Admin-Secret": c.adminSecret}, body)
	shared.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (c *console) swarmStop(w http.ResponseWriter, _ *http.Request) {
	c.post(c.loadlab+"/stop", map[string]string{"X-Demo-Admin-Secret": c.adminSecret}, nil)
	shared.WriteJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
}

func (c *console) authority(w http.ResponseWriter, _ *http.Request) {
	hmac := shared.Env("BRUISER_DEV_HMAC_SECRET", "")
	cmd := exec.Command(c.bruiserBin, "authority-check",
		"--edge", c.simtixEdge, "--origin", c.simtixOrigin,
		"--control", c.gateway, "--admin", shared.Env("BRUISER_ADMIN_URL", "http://127.0.0.1:8082"),
		"--membership", seed.AliceMembership, "--event", seed.HeadlineEventID,
		"--hmac-secret", hmac, "--json")
	out, err := cmd.CombinedOutput()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusOK)
	}
	if len(out) == 0 {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": errString(err), "output": string(out)})
		return
	}
	_, _ = w.Write(out)
}

func (c *console) audit(w http.ResponseWriter, _ *http.Request) {
	v := c.getJSON(c.gateway+"/v1/operator/audit?limit=40", map[string]string{"X-Bruiser-Operator-Secret": c.operatorSecret})
	shared.WriteJSON(w, http.StatusOK, v)
}

func (c *console) executions(w http.ResponseWriter, _ *http.Request) {
	v := c.getJSON(c.gateway+"/v1/operator/executions?limit=20", map[string]string{"X-Bruiser-Operator-Secret": c.operatorSecret})
	shared.WriteJSON(w, http.StatusOK, v)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func pageShell(title, body string) string {
	return fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><title>%s · Bruiser</title>
<style>
:root{--bg:#f3eee4;--panel:#fffdf8;--ink:#1c1917;--muted:#6b645c;--rust:#b44a32;--line:rgba(28,25,23,.12);--ok:#2f5d45;--busy:#a15c12;--deny:#9f2d20}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font-family:"Segoe UI",ui-sans-serif,system-ui,sans-serif}
header{padding:16px 24px;border-bottom:1px solid var(--line);display:flex;justify-content:space-between;align-items:center;background:var(--bg)}
.brand{display:flex;align-items:center;gap:10px;font-weight:800;letter-spacing:.08em;font-size:13px;text-transform:uppercase}
.mark{width:28px;height:28px;border-radius:8px;background:var(--rust);color:var(--bg);display:grid;place-items:center;font-size:14px}
header>span{color:var(--muted);font-size:13px}
main{width:min(1100px,calc(100%% - 32px));margin:24px auto}
.tiles{display:grid;grid-template-columns:repeat(4,1fr);gap:12px}
@media (max-width:900px){.tiles{grid-template-columns:repeat(2,1fr)}}
.tile{background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:14px}
.tile b{display:block;font-size:1.6rem}
.tile span{color:var(--muted);font-size:12px}
.row{display:flex;gap:8px;flex-wrap:wrap;margin:16px 0;align-items:center}
button,select,input{background:var(--panel);color:var(--ink);border:1px solid var(--line);border-radius:8px;padding:8px 12px;font:inherit}
button.primary,.form button{background:var(--rust);color:var(--bg);font-weight:800;border:0}
pre{background:var(--panel);padding:12px;border-radius:10px;overflow:auto;max-height:320px;font-size:12px;border:1px solid var(--line)}
.cert{background:var(--panel);padding:16px;border-radius:12px;border:1px solid var(--line);margin:12px 0}
.cert.pass{border-color:var(--ok)}
.cert.fail{border-color:var(--deny)}
.cert ul{margin:8px 0 0;padding-left:18px}
.form label{display:block;margin:12px 0}
h1{font-family:Palatino,Georgia,serif;font-weight:500;letter-spacing:-.02em}
.hint{color:var(--muted);font-size:13px;margin:0 0 12px}
.tile.hot{border-color:var(--rust)}
table.feed{width:100%%;border-collapse:collapse;font-size:13px}
table.feed th,table.feed td{text-align:left;padding:6px 8px;border-bottom:1px solid var(--line);font-variant-numeric:tabular-nums}
table.feed th{color:var(--muted);font-weight:600}
.st-order{color:var(--ok);font-weight:700}
.st-allow{color:#215a8c}
.st-busy{color:var(--busy)}
.st-denied{color:var(--deny)}
#toast{position:fixed;right:20px;bottom:20px;background:var(--ink);color:var(--bg);font-weight:700;padding:12px 16px;border-radius:10px;display:none;max-width:420px;z-index:9;border-left:4px solid var(--rust)}
#toast.show{display:block}
#resetStatus:not(:empty){color:var(--ink);font-weight:700;margin:0}
.mem-field.hidden{display:none}
.limit-block{display:flex;align-items:center;gap:10px;flex-wrap:wrap}
.limit-label{font-weight:700}
.seg{display:inline-flex;border:1px solid var(--line);border-radius:10px;overflow:hidden;background:var(--panel)}
.seg button{border:0;border-radius:0;margin:0;background:transparent}
.seg button.on{background:var(--rust);color:var(--bg);font-weight:800}
details.advanced{margin:0 0 16px;max-width:640px}
details.advanced summary{cursor:pointer;color:var(--muted);font-size:13px}
</style></head><body>
<header><span class="brand"><span class="mark">B</span> Bruiser</span><span>Harchester United · operations</span></header>
<main>%s</main><div id="toast"></div></body></html>`, title, body)
}

const dashboardHTML = `
<h1>One customer remains one customer.</h1>
<p>Live view of the authoritative control layer in front of SimTix. Sources named on each tile. Never a queue: held-back agents are BUSY.</p>
<div class="tiles" id="tiles"></div>
<div class="row">
  <button class="primary" onclick="resetDemo()">Reset demo</button>
  <span class="hint" id="resetStatus"></span>
  <div class="limit-block">
    <span class="limit-label">Per-customer limit</span>
    <div class="seg" id="limitSeg">
      <button type="button" data-limit="off" onclick="setLimit('off')">Off</button>
      <button type="button" data-limit="dry-run" onclick="setLimit('dry-run')">Dry run</button>
      <button type="button" data-limit="on" class="on" onclick="setLimit('on')">On</button>
    </div>
  </div>
  <button onclick="runCheck()">Run Authority Check</button>
  <button onclick="loadAudit()">Refresh audit</button>
</div>
<p class="hint" id="enfHint">On = one customer, one execution. Off = no limit.</p>
<details class="advanced">
  <summary>Rollout % (advanced)</summary>
  <select id="enf" onchange="setRollout()" title="Per-customer rollout">
    <option value="10">10%</option>
    <option value="25">25%</option>
    <option value="50">50%</option>
    <option value="75">75%</option>
    <option value="100" selected>100%</option>
  </select>
  <p class="hint">Percent is <strong>per-customer rollout</strong> (some customers fully enforced, others dry-run) — not “let through X% of an agent swarm.”</p>
</details>
<h2>Agent Lab</h2>
<div class="row">
  <label class="mem-field" id="memWrap">Membership <input id="mem" value="1001234" placeholder="Membership"></label>
  <input id="pw" value="password" placeholder="Password">
  <select id="preset" onchange="syncPreset()">
    <option value="single">Single supporter</option>
    <option value="ten">10 × N</option>
    <option value="multi">1,000 × 10</option>
  </select>
  <label>N <select id="n">
    <option>1</option><option>50</option><option selected>200</option><option>500</option><option>5000</option><option>10000</option>
  </select></label>
  <button class="primary" onclick="launch()">Launch</button>
  <button onclick="stopSwarm()">Stop</button>
</div>
<p class="hint" id="presetHint">Membership IDs come from the preset (seeded eligible supporters).</p>
<h2>Live feed</h2>
<p class="hint">Agents reach SimTix through club → Edge hold/order. Status: <span class="st-order">order</span> purchased, <span class="st-allow">allow</span> forwarded, <span class="st-busy">busy</span> held back, <span class="st-denied">denied</span> origin/login.</p>
<table class="feed"><thead><tr><th>Agent</th><th>Membership</th><th>Status</th><th>Path</th><th>Detail</th></tr></thead><tbody id="feed"><tr><td colspan="5">Waiting for a run…</td></tr></tbody></table>
<h2>Authority Check certificate</h2>
<div id="cert" class="cert idle">Run Authority Check to mint a certificate against this Edge.</div>
<h2>Audit (append-only; reset does not delete)</h2>
<pre id="audit">—</pre>
<h2>Load-lab / edge</h2>
<pre id="log">waiting for snapshot…</pre>
<script>
const tiles = document.getElementById('tiles');
const log = document.getElementById('log');
const cert = document.getElementById('cert');
const auditEl = document.getElementById('audit');
const feed = document.getElementById('feed');
let holdFeed = false;
function toast(msg){
  const rs = document.getElementById('resetStatus');
  if (rs) rs.textContent = msg;
  const el = document.getElementById('toast');
  if (!el) return;
  el.textContent = msg;
  el.className = 'show';
  clearTimeout(el._t);
  el._t = setTimeout(() => { el.className = ''; }, 8000);
}
function syncPreset(){
  const preset = document.getElementById('preset').value;
  const single = preset === 'single';
  document.getElementById('memWrap').classList.toggle('hidden', !single);
  document.getElementById('mem').disabled = !single;
  document.getElementById('presetHint').textContent = single
    ? 'Single supporter: every agent uses the membership box (Alice 1001234 by default).'
    : (preset === 'ten'
      ? '10 × N: ten seeded eligible memberships (Alice + nine others). N is agents per customer. Total agents = 10 × N. The membership box is not used.'
      : '1,000 × 10: one thousand sequential seeded memberships × 10 agents (10,000 total). The membership box is not used.');
  if (preset === 'ten') {
    const n = document.getElementById('n');
    if (parseInt(n.value,10) > 500) n.value = '50';
  }
}
syncPreset();
const es = new EventSource('/events');
es.addEventListener('snapshot', (e) => {
  const d = JSON.parse(e.data);
  const g = d.gateway || {};
  const eaf = (g.eaf && g.eaf[0]) || {};
  const sim = d.simtix || {};
  const edge = d.edge || {};
  const est = d.edge_stats || {};
  const lab = d.loadlab || {};
  const club = d.club || {};
  const sum = lab.summary || {};
  const items = [
    ['Seats remaining', sim.available, 'SimTix origin', true],
    ['Orders', sim.orders ?? sum.orders ?? 0, 'SimTix origin (sold checkouts)', true],
    ['Held / sold', (sim.held||0)+' / '+(sim.sold||0), 'SimTix origin', false],
    ['Swarm orders', sum.orders||0, 'load-lab completed checkouts', true],
    ['Customers online', club.customers_online, 'Harchester sessions (10 min)', false],
    ['Allocation attempts', eaf.attempts, 'gateway operator / EAF', false],
    ['Executions forwarded', eaf.forwarded, 'gateway operator / EAF', false],
    ['Held back (BUSY)', eaf.busy ?? sum.busy, 'gateway operator / EAF', false],
    ['Observed EAF', (eaf.observed_eaf||0).toFixed ? (eaf.observed_eaf||0).toFixed(1)+'×' : eaf.observed_eaf, 'headline KPI', false],
    ['Downstream EAF', eaf.downstream_eaf, 'gateway operator / EAF', false],
    ['Active executions', g.active_executions, 'gateway operator', false],
    ['Would-have-blocked', est.would_block, 'SimTix edge', false],
    ['Per-customer limit', limitLabel(edge), 'SimTix edge', false],
    ['Agents finished', sum.finished||0, 'load-lab', false]
  ];
  tiles.innerHTML = items.map(([k,v,s,hot]) => '<div class="tile'+(hot?' hot':'')+'"><span>'+k+'</span><b>'+(v??'—')+'</b><span>'+s+'</span></div>').join('');
  const events = lab.events || [];
  const rows = events.filter(x => x.type === 'agent').slice(-40).reverse();
  if (holdFeed && !lab.running) {
    feed.innerHTML = '<tr><td colspan="5">Waiting for a run…</td></tr>';
  } else if (!rows.length) {
    feed.innerHTML = '<tr><td colspan="5">'+(lab.running ? 'Swarm running…' : 'Waiting for a run…')+'</td></tr>';
  } else {
    if (lab.running) holdFeed = false;
    feed.innerHTML = rows.map(x => {
      const st = (x.status||'').toLowerCase();
      return '<tr><td>'+(x.id||'')+'</td><td>'+(x.membership||'—')+'</td><td class="st-'+st+'">'+(x.status||'—')+'</td><td>'+(x.path||'club → Edge hold/order')+'</td><td>'+(x.detail||'')+'</td></tr>';
    }).join('');
  }
  log.textContent = JSON.stringify({loadlab: {running: lab.running, done: lab.done, preset: lab.preset, summary: sum}, edge, eaf}, null, 2);
  paintLimit(limitKey(edge.mode));
});
function limitKey(mode){
  mode = (mode||'').toLowerCase();
  if (mode === 'off') return 'off';
  if (mode === 'dry-run') return 'dry-run';
  return 'on';
}
function limitLabel(edge){
  const mode = (edge.mode||'').toLowerCase();
  if (mode === 'off') return 'Off';
  if (mode === 'dry-run') return 'Dry run';
  const p = edge.percent;
  if (p && p !== 100) return 'On ('+p+'%)';
  return 'On';
}
function paintLimit(which){
  document.querySelectorAll('#limitSeg button').forEach(b => b.classList.toggle('on', b.getAttribute('data-limit')===which));
}
async function applyEnf(mode, percent){
  await fetch('/enforcement', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({mode, percent})});
}
async function setLimit(which){
  paintLimit(which);
  if (which === 'off') return applyEnf('off', 0);
  if (which === 'dry-run') return applyEnf('dry-run', 0);
  document.getElementById('enf').value = '100';
  return applyEnf('enforce', 100);
}
async function setRollout(){
  paintLimit('on');
  const percent = parseInt(document.getElementById('enf').value, 10) || 100;
  return applyEnf('enforce', percent);
}
async function resetDemo(){
  toast('Resetting…');
  try {
    const res = await fetch('/reset', {method:'POST'});
    const d = await res.json().catch(() => ({}));
    toast(d.message || ('Reset: '+(d.seats_remaining??'—')+' seats remaining, executions cleared.'));
    holdFeed = true;
    feed.innerHTML = '<tr><td colspan="5">Waiting for a run…</td></tr>';
  } catch (err) {
    toast('Reset failed: '+(err && err.message ? err.message : err));
  }
}
async function launch(){
  const n = parseInt(document.getElementById('n').value,10);
  const preset = document.getElementById('preset').value;
  let agents = n;
  if (preset === 'multi') agents = 10000;
  if (preset === 'ten') agents = 10 * n;
  const body = { password: document.getElementById('pw').value, agents, spawn_per_s: 1000, retry_window_sec: 8, preset };
  if (preset === 'single') body.membership_number = document.getElementById('mem').value;
  if (preset === 'multi') { body.supporters = 1000; body.start_membership = 1000001; }
  holdFeed = false;
  await fetch('/swarm', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(body)});
}
async function stopSwarm(){ await fetch('/swarm/stop', {method:'POST'}); }
async function runCheck(){
  cert.className = 'cert idle';
  cert.textContent = 'Running…';
  const res = await fetch('/authority-check', {method:'POST'});
  const t = await res.text();
  let d;
  try { d = JSON.parse(t); } catch(e) { cert.className='cert fail'; cert.textContent = t; return; }
  const ok = d.overall === 'PASS';
  cert.className = 'cert ' + (ok ? 'pass' : 'fail');
  const probes = (d.probes||[]).map(p => '<li><strong>'+(p.pass?'PASS':'FAIL')+'</strong> '+p.name+(p.detail? ' — '+p.detail:'')+'</li>').join('');
  cert.innerHTML = '<div><b>Overall Result: '+(d.overall||'?')+'</b></div><ul>'+probes+'</ul>';
}
async function loadAudit(){
  const res = await fetch('/audit');
  const d = await res.json();
  auditEl.textContent = JSON.stringify(d, null, 2);
}
loadAudit();
</script>
`
