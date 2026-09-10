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
		operatorSecret: shared.Env("BRUISER_OPERATOR_SECRET", "operator-secret-dev"),
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

func (c *console) post(url string, headers map[string]string, body any) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(http.MethodPost, url, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

func (c *console) reset(w http.ResponseWriter, _ *http.Request) {
	h := map[string]string{"X-Demo-Admin-Secret": c.adminSecret}
	c.post(c.simtixOrigin+"/_admin/reset", h, nil)
	c.post(c.simtixEdge+"/_edge/reset-stats", h, nil)
	c.post(c.loadlab+"/stop", h, nil)
	c.post(c.gateway+"/v1/operator/reset-executions", map[string]string{"X-Bruiser-Operator-Secret": c.operatorSecret}, nil)
	shared.WriteJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

func (c *console) enforcement(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode    string `json:"mode"`
		Percent int    `json:"percent"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	c.post(c.simtixEdge+"/_edge/config", map[string]string{"X-Demo-Admin-Secret": c.adminSecret}, body)
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
	cmd := exec.Command(c.bruiserBin, "authority-check",
		"--edge", c.simtixEdge, "--origin", c.simtixOrigin,
		"--membership", seed.AliceMembership, "--event", seed.HeadlineEventID, "--json")
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

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func pageShell(title, body string) string {
	return fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><title>%s · Bruiser</title>
<style>
:root{--bg:#0b0d12;--ink:#f4efe4;--muted:#9a9386;--lime:#d6ff4a;--line:rgba(244,239,228,.12)}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font-family:ui-sans-serif,system-ui,sans-serif}
header{padding:16px 24px;border-bottom:1px solid var(--line);display:flex;justify-content:space-between}
main{width:min(1100px,calc(100%% - 32px));margin:24px auto}
.tiles{display:grid;grid-template-columns:repeat(4,1fr);gap:12px}
.tile{background:rgba(255,255,255,.04);border:1px solid var(--line);border-radius:14px;padding:14px}
.tile b{display:block;font-size:1.6rem}
.tile span{color:var(--muted);font-size:12px}
.row{display:flex;gap:8px;flex-wrap:wrap;margin:16px 0}
button,select,input{background:#151821;color:var(--ink);border:1px solid var(--line);border-radius:8px;padding:8px 12px;font:inherit}
button.primary{background:var(--lime);color:#111;font-weight:800;border:0}
pre{background:#151821;padding:12px;border-radius:10px;overflow:auto;max-height:320px;font-size:12px}
.form label{display:block;margin:12px 0}
h1{font-weight:500}
</style></head><body>
<header><strong>Bruiser</strong><span>Harchester United · operations</span></header>
<main>%s</main></body></html>`, title, body)
}

const dashboardHTML = `
<h1>One customer remains one customer.</h1>
<p>Live view of the authoritative control layer in front of SimTix. Sources named on each tile.</p>
<div class="tiles" id="tiles"></div>
<div class="row">
  <button class="primary" onclick="resetDemo()">Reset demo</button>
  <select id="enf" onchange="setEnf()">
    <option value="off">Off</option>
    <option value="dry-run">Dry Run</option>
    <option value="10">10%</option>
    <option value="25">25%</option>
    <option value="50">50%</option>
    <option value="75">75%</option>
    <option value="100" selected>100%</option>
  </select>
  <button onclick="runCheck()">Run Authority Check</button>
</div>
<h2>Agent Lab</h2>
<div class="row">
  <input id="mem" value="1001234" placeholder="Membership">
  <input id="pw" value="password" placeholder="Password">
  <select id="preset"><option value="single">Single supporter</option><option value="multi">1,000 × 10</option></select>
  <select id="n">
    <option>1</option><option>50</option><option>500</option><option>5000</option><option selected>10000</option>
  </select>
  <button class="primary" onclick="launch()">Launch</button>
  <button onclick="stopSwarm()">Stop</button>
</div>
<pre id="log">waiting for snapshot…</pre>
<script>
const tiles = document.getElementById('tiles');
const log = document.getElementById('log');
const es = new EventSource('/events');
es.addEventListener('snapshot', (e) => {
  const d = JSON.parse(e.data);
  const g = d.gateway || {};
  const eaf = (g.eaf && g.eaf[0]) || {};
  const sim = d.simtix || {};
  const edge = d.edge || {};
  const es = d.edge_stats || {};
  const lab = d.loadlab || {};
  const club = d.club || {};
  const items = [
    ['Seats remaining', sim.available, 'SimTix origin'],
    ['Held / sold', (sim.held||0)+' / '+(sim.sold||0), 'SimTix origin'],
    ['Customers online', club.customers_online, 'Harchester sessions (10 min)'],
    ['Allocation attempts', eaf.attempts, 'gateway operator / EAF'],
    ['Executions forwarded', eaf.forwarded, 'gateway operator / EAF'],
    ['Held back (BUSY)', eaf.busy, 'gateway operator / EAF'],
    ['Observed EAF', (eaf.observed_eaf||0).toFixed ? (eaf.observed_eaf||0).toFixed(1)+'×' : eaf.observed_eaf, 'headline KPI'],
    ['Downstream EAF', eaf.downstream_eaf, 'gateway operator / EAF'],
    ['Active executions', g.active_executions, 'gateway operator'],
    ['Would-have-blocked', es.would_block, 'SimTix edge'],
    ['Enforcement', (edge.mode||'')+' '+(edge.percent||'')+'%', 'SimTix edge'],
    ['Agents finished', (lab.summary&&lab.summary.finished)||0, 'load-lab']
  ];
  tiles.innerHTML = items.map(([k,v,s]) => '<div class="tile"><span>'+k+'</span><b>'+(v??'—')+'</b><span>'+s+'</span></div>').join('');
  log.textContent = JSON.stringify({loadlab: lab, edge, eaf}, null, 2);
});
async function resetDemo(){ await fetch('/reset', {method:'POST'}); }
async function setEnf(){
  const v = document.getElementById('enf').value;
  let mode='enforce', percent=100;
  if(v==='off') mode='off', percent=0;
  else if(v==='dry-run') mode='dry-run', percent=0;
  else { percent=parseInt(v,10); }
  await fetch('/enforcement', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({mode, percent})});
}
async function launch(){
  const n = parseInt(document.getElementById('n').value,10);
  const preset = document.getElementById('preset').value;
  const agents = preset==='multi' ? 10000 : n;
  await fetch('/swarm', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({
    membership_number: document.getElementById('mem').value,
    password: document.getElementById('pw').value,
    agents, spawn_per_s: 1000, retry_window_sec: 8, preset, supporters: 1000
  })});
}
async function stopSwarm(){ await fetch('/swarm/stop', {method:'POST'}); }
async function runCheck(){
  const res = await fetch('/authority-check', {method:'POST'});
  const t = await res.text();
  log.textContent = t;
}
</script>
`
