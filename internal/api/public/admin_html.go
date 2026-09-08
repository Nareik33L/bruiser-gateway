package publicapi

const adminLoginHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Bruiser admin</title>
<style>
body{font-family:ui-sans-serif,system-ui,sans-serif;background:#0f1419;color:#e8eef4;margin:0;display:grid;place-items:center;min-height:100vh}
form{background:#1a222c;padding:2rem;border-radius:12px;width:min(28rem,92vw);box-shadow:0 12px 40px #0006}
label{display:block;margin:.5rem 0 .25rem;color:#9db0c3}
input,button{width:100%;padding:.7rem;border-radius:8px;border:1px solid #334}
input{background:#0f1419;color:#e8eef4}
button{margin-top:1rem;background:#d4a017;border:0;font-weight:700;cursor:pointer}
p{color:#9db0c3;font-size:.9rem}
</style></head>
<body>
<form method="get" action="/admin">
<h1>Bruiser operator</h1>
<p>Enter the admin secret (same as the edge secret in the lab).</p>
<label>Admin secret</label>
<input name="secret" type="password" autocomplete="current-password" required>
<button type="submit">Open dashboard</button>
</form>
</body></html>`

const adminHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Bruiser — EAF</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
:root{--bg:#0f1419;--card:#1a222c;--ink:#e8eef4;--muted:#9db0c3;--gold:#d4a017;--pass:#3ecf8e;--fail:#f07178}
*{box-sizing:border-box}
body{margin:0;font-family:ui-sans-serif,system-ui,sans-serif;background:var(--bg);color:var(--ink)}
header{padding:1.2rem 1.5rem;border-bottom:1px solid #243}
h1{margin:0;font-size:1.1rem;letter-spacing:.04em;text-transform:uppercase;color:var(--muted)}
.eaf{font-size:clamp(2.4rem,8vw,5rem);font-weight:800;margin:.2rem 0;color:var(--gold)}
.sub{color:var(--muted)}
main{display:grid;gap:1rem;padding:1rem 1.5rem;grid-template-columns:repeat(auto-fit,minmax(18rem,1fr))}
section{background:var(--card);border-radius:12px;padding:1rem 1.1rem}
h2{margin:.2rem 0 .8rem;font-size:.95rem;color:var(--muted);text-transform:uppercase;letter-spacing:.06em}
table{width:100%;border-collapse:collapse;font-size:.9rem}
td,th{text-align:left;padding:.35rem 0;border-bottom:1px solid #243}
.pass{color:var(--pass)} .fail{color:var(--fail)}
button{background:var(--gold);border:0;border-radius:8px;padding:.45rem .8rem;font-weight:700;cursor:pointer}
input{background:#0f1419;color:var(--ink);border:1px solid #334;border-radius:8px;padding:.4rem .6rem}
.row{display:flex;gap:.5rem;flex-wrap:wrap;align-items:center}
pre{white-space:pre-wrap;font-size:.8rem;color:var(--muted)}
a{color:var(--gold)}
</style></head>
<body>
<header>
  <h1>Observed EAF</h1>
  <div class="eaf" id="eaf">—</div>
  <div class="sub">Downstream <strong id="down">—</strong> · attempts <span id="att">0</span> · forwarded <span id="fwd">0</span> · policy v<span id="pol">0</span> · ramp <strong id="mode">—</strong></div>
</header>
<main>
  <section>
    <h2>Enforcement ramp</h2>
    <p class="sub">Don’t trust us. Start at 10%. Observation stays at 100%; only active enforcement changes. Assignment is per customer.</p>
    <table>
      <tr><td>Active enforcement</td><td id="ctl-pct">—</td></tr>
      <tr><td>Mode</td><td id="ctl-mode">—</td></tr>
      <tr><td>Master switch</td><td id="ctl-enf">—</td></tr>
      <tr><td>Queue</td><td id="ctl-q">—</td></tr>
      <tr><td>Fail closed</td><td id="ctl-fc">—</td></tr>
    </table>
    <div class="row" style="margin-top:.6rem">
      <button data-pct="0">0% dry run</button>
      <button data-pct="10">10%</button>
      <button data-pct="25">25%</button>
      <button data-pct="50">50%</button>
      <button data-pct="75">75%</button>
      <button data-pct="100">100%</button>
    </div>
    <div class="row" style="margin-top:.4rem">
      <button id="enf-off">Emergency 0%</button>
      <button id="q-off">Queue off</button>
      <button id="drain">Drain waiters</button>
      <button id="rev-all">Revoke all</button>
    </div>
  </section>
  <section>
    <h2>What Bruiser would have stopped</h2>
    <p class="sub">Last 24h — 100% evaluated. Enforced vs hypothetical on the same policies.</p>
    <table>
      <tr><td>Requests observed</td><td id="dr-obs">0</td></tr>
      <tr><td>Requests evaluated</td><td id="dr-eval">0</td></tr>
      <tr><td>Actually enforced</td><td id="dr-enf">0</td></tr>
      <tr><td>Not actively enforced</td><td id="dr-obs2">0</td></tr>
      <tr><td>Would allow</td><td id="dr-allow">0</td></tr>
      <tr><td>Would queue</td><td id="dr-queue">0</td></tr>
      <tr><td>Would reject</td><td id="dr-rej">0</td></tr>
      <tr><td>Actual allow / queue / reject</td><td id="dr-act">0 / 0 / 0</td></tr>
      <tr><td>Customers affected</td><td id="dr-aff">0</td></tr>
      <tr><td>Requests absorbed</td><td id="dr-abs">0</td></tr>
      <tr><td>Origin-load reduction</td><td id="dr-olr">—</td></tr>
      <tr><td>Execution Amplification Factor</td><td id="dr-eaf">—</td></tr>
      <tr><td>Errors / policy violations</td><td id="dr-err">0 / 0</td></tr>
    </table>
    <pre id="dr-recent"></pre>
  </section>
  <section>
    <h2>Authority Check</h2>
    <div id="check-overall" class="sub">No run recorded</div>
    <ul id="probes"></ul>
    <div class="row">
      <button hx-post="/v1/admin/authority-check" id="run-check">Run check</button>
      <span class="sub" id="check-at"></span>
    </div>
  </section>
  <section>
    <h2>Usage (fair-use, informational)</h2>
    <table>
      <tr><td>Active executions</td><td id="u-active">0</td></tr>
      <tr><td>Queue depth</td><td id="u-queue">0</td></tr>
      <tr><td>Executions / 30d</td><td id="u-30">0</td></tr>
      <tr><td>Resources protected</td><td id="u-res">0</td></tr>
    </table>
  </section>
  <section style="grid-column:1/-1">
    <h2>Active executions</h2>
    <table><thead><tr><th>Customer</th><th>Principal</th><th>Resource</th><th>Fence</th><th></th></tr></thead>
    <tbody id="execs"></tbody></table>
  </section>
  <section style="grid-column:1/-1">
    <h2>Same-customer waiters</h2>
    <p class="sub">Not a waiting room. Extra agents of one customer, waiting for that customer’s next execution.</p>
    <table><thead><tr><th>Customer</th><th>Principal</th><th>Resource</th><th>Expires</th></tr></thead>
    <tbody id="waiters"></tbody></table>
  </section>
  <section style="grid-column:1/-1">
    <h2>Audit (why?)</h2>
    <div class="row">
      <input id="cust" placeholder="customer id" />
      <button id="search">Search</button>
      <a id="export" href="/v1/admin/export">Export JSONL</a>
    </div>
    <table><thead><tr><th>When</th><th>Type</th><th>Customer</th><th>Rule</th><th>Reason</th></tr></thead>
    <tbody id="audit"></tbody></table>
  </section>
</main>
<script>
const secret = new URLSearchParams(location.search).get('secret') || '';
function headers(){const h={}; if(secret) h['X-Bruiser-Admin-Secret']=secret; return h;}
function qs(u){if(!secret) return u; const j=u.includes('?')?'&':'?'; return u+j+'secret='+encodeURIComponent(secret);}
function paint(d){
  const e=d.eaf||{};
  document.getElementById('eaf').textContent = (e.observed||0).toFixed(0)+'×';
  document.getElementById('down').textContent = (e.downstream||0).toFixed(0)+'×';
  document.getElementById('att').textContent = e.attempts||0;
  document.getElementById('fwd').textContent = e.forwarded||0;
  document.getElementById('pol').textContent = d.policy_version||0;
  const ctl=d.controls||{};
  const pct = (ctl.enforcement===false || ctl.mode==='dry-run') ? 0 : (ctl.enforce_percent||0);
  document.getElementById('mode').textContent = pct+'%';
  document.getElementById('ctl-pct').textContent = pct+'%';
  document.getElementById('ctl-mode').textContent = ctl.mode||'enforce';
  document.getElementById('ctl-enf').textContent = ctl.enforcement===false?'OFF':'ON';
  document.getElementById('ctl-q').textContent = ctl.queue_enabled===false?'OFF':'ON';
  document.getElementById('ctl-fc').textContent = ctl.fail_closed===false?'open':'closed';
  const dr=d.dry_run||{};
  document.getElementById('dr-obs').textContent = dr.requests_observed||0;
  document.getElementById('dr-eval').textContent = dr.requests_evaluated||dr.requests_observed||0;
  document.getElementById('dr-enf').textContent = dr.traffic_enforced||0;
  document.getElementById('dr-obs2').textContent = dr.traffic_not_enforced||0;
  document.getElementById('dr-allow').textContent = dr.would_allow||0;
  document.getElementById('dr-queue').textContent = dr.would_queue||0;
  document.getElementById('dr-rej').textContent = dr.would_reject||0;
  document.getElementById('dr-act').textContent = (dr.actual_allow||0)+' / '+(dr.actual_queue||0)+' / '+(dr.actual_reject||0);
  document.getElementById('dr-aff').textContent = dr.customers_affected||0;
  document.getElementById('dr-abs').textContent = dr.requests_absorbed||dr.requests_potentially_absorbed||0;
  document.getElementById('dr-olr').textContent = ((dr.estimated_origin_load_reduction||0)*100).toFixed(1)+'%';
  document.getElementById('dr-eaf').textContent = (dr.execution_amplification_factor||0).toFixed(2)+'×';
  document.getElementById('dr-err').textContent = (dr.errors||0)+' / '+(dr.policy_violations||0);
  const rec=(dr.recent||[]).slice(0,8).map(x=>(x.kind||'hypothetical')+'  '+x.would+'  customer '+x.customer_id+'  '+x.reason).join('\n');
  document.getElementById('dr-recent').textContent = rec;
  const u=d.usage||{};
  document.getElementById('u-active').textContent = u.active_executions||0;
  document.getElementById('u-queue').textContent = u.queue_depth||0;
  document.getElementById('u-30').textContent = u.executions_30d||0;
  document.getElementById('u-res').textContent = u.resources_protected||0;
  const tb=document.getElementById('execs'); tb.innerHTML='';
  (d.executions||[]).forEach(x=>{
    const tr=document.createElement('tr');
    const p=x.principal||{};
    tr.innerHTML='<td>'+x.customer_id+'</td><td>'+p.type+':'+p.id+'</td><td>'+x.resource+'</td><td>'+x.fence+'</td><td><button data-rev="'+x.execution_id+'">Revoke</button></td>';
    tb.appendChild(tr);
  });
  const wtb=document.getElementById('waiters'); if(wtb){ wtb.innerHTML='';
    (d.waiters||[]).forEach(x=>{
      const tr=document.createElement('tr');
      const p=x.principal||{};
      tr.innerHTML='<td>'+x.customer_id+'</td><td>'+p.type+':'+p.id+'</td><td>'+x.resource+'</td><td>'+(x.expires_at||'')+'</td>';
      wtb.appendChild(tr);
    });
  }
  const last=d.last_authority_check;
  if(last){
    const ok=last.reason==='PASS';
    document.getElementById('check-overall').innerHTML='<span class="'+(ok?'pass':'fail')+'">Overall Result: '+(last.reason||'')+'</span>';
    document.getElementById('check-at').textContent=last.at||'';
    const ul=document.getElementById('probes'); ul.innerHTML='';
    const probes=(last.report&&last.report.probes)||[];
    probes.forEach(p=>{
      const li=document.createElement('li');
      li.className=p.pass?'pass':'fail';
      li.textContent=(p.pass?'PASS':'FAIL')+' — '+p.name;
      ul.appendChild(li);
    });
  }
}
async function refresh(){
  const r=await fetch(qs('/v1/admin/status'),{headers:headers()});
  if(r.ok) paint(await r.json());
}
document.getElementById('run-check').onclick=async()=>{
  const r=await fetch(qs('/v1/admin/authority-check'),{method:'POST',headers:headers()});
  const j=await r.json();
  await refresh();
  if(j.overall) document.getElementById('check-overall').textContent='Overall Result: '+j.overall;
};
document.getElementById('search').onclick=async()=>{
  const c=document.getElementById('cust').value;
  const r=await fetch(qs('/v1/admin/audit?customer='+encodeURIComponent(c)),{headers:headers()});
  const j=await r.json();
  const tb=document.getElementById('audit'); tb.innerHTML='';
  (j.events||[]).forEach(ev=>{
    const tr=document.createElement('tr');
    tr.innerHTML='<td>'+ev.at+'</td><td>'+ev.type+'</td><td>'+ev.customer_id+'</td><td>'+ev.rule_name+'</td><td>'+ev.reason+'</td>';
    tb.appendChild(tr);
  });
};
document.body.addEventListener('click',async e=>{
  const id=e.target.getAttribute&&e.target.getAttribute('data-rev');
  if(!id) return;
  await fetch(qs('/v1/admin/executions/'+id+'/revoke'),{method:'POST',headers:Object.assign({'Content-Type':'application/json'},headers()),body:JSON.stringify({reason:'admin'})});
  refresh();
});
async function putCtl(body){
  await fetch(qs('/v1/admin/controls'),{method:'PUT',headers:Object.assign({'Content-Type':'application/json'},headers()),body:JSON.stringify(body)});
  refresh();
}
document.body.addEventListener('click',e=>{
  const pct=e.target.getAttribute&&e.target.getAttribute('data-pct');
  if(pct==null) return;
  putCtl({enforce_percent:parseInt(pct,10),enforcement:true,updated_by:'admin-ui'});
});
document.getElementById('enf-off').onclick=()=>putCtl({enforce_percent:0,updated_by:'admin-ui'});
document.getElementById('q-off').onclick=()=>putCtl({queue_enabled:false,updated_by:'admin-ui'});
document.getElementById('drain').onclick=async()=>{
  await fetch(qs('/v1/admin/controls/drain'),{method:'POST',headers:headers()});
  refresh();
};
document.getElementById('rev-all').onclick=async()=>{
  await fetch(qs('/v1/admin/controls/revoke-all'),{method:'POST',headers:headers()});
  refresh();
};
document.getElementById('export').href=qs('/v1/admin/export');
refresh();
try{
  const es=new EventSource(qs('/v1/admin/stream'));
  es.addEventListener('status',ev=>{ try{ paint(JSON.parse(ev.data)); }catch(e){} });
}catch(e){ setInterval(refresh,2000); }
</script>
</body></html>`
