(() => {
  const $ = (id) => document.getElementById(id);
  const state = {
    store: null,
    mode: "dry-run",
    swarm: 50,
    dry: null,
    enforce: null,
    es: null,
    sprites: [],
    running: false,
  };

  const fmt = (n) => Number(n || 0).toLocaleString("en-GB");

  function show(id) {
    const el = $(id);
    if (!el) return;
    el.classList.remove("hidden");
    el.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  function mark(step) {
    document.querySelectorAll("#progress .step").forEach((s) => {
      s.classList.toggle("on", s.dataset.step === step || stepOrder(s.dataset.step) <= stepOrder(step));
    });
  }
  function stepOrder(s) {
    return ["create", "observe", "dry", "value", "enforce"].indexOf(s);
  }

  $("launch").addEventListener("click", (e) => {
    e.preventDefault();
    $("create").scrollIntoView({ behavior: "smooth" });
  });

  $("create-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    $("create-err").textContent = "";
    const body = {
      name: $("name").value.trim() || "Bruiser FC",
      product: $("product").value.trim() || "Limited Edition Shirt",
      price: $("price").value.trim() || "£50.00",
    };
    const res = await fetch("/lab/stores", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    const data = await res.json();
    if (!res.ok) {
      $("create-err").textContent = data.error || "Could not create store";
      return;
    }
    state.store = data;
    $("pv-title").textContent = `${data.name} × Special Edition`;
    $("pv-product").textContent = data.product;
    $("pv-price").textContent = data.price;
    $("cfg-name").textContent = data.name;
    $("view-store").href = data.url;
    show("live-store");
    mark("observe");
  });

  $("to-config").addEventListener("click", () => {
    show("config");
  });

  document.querySelectorAll("#modes input").forEach((input) => {
    input.addEventListener("change", async () => {
      state.mode = input.value;
      document.querySelectorAll("#modes .mode").forEach((el) => el.classList.remove("on"));
      input.closest(".mode").classList.add("on");
      const copy = {
        off: "Gateway is idle. Traffic reaches the sandbox store without Bruiser admitting it.",
        "dry-run": "Dry Run watches every request and makes decisions without blocking anyone.",
        enforce: "Bruiser will now actively enforce its decisions.",
      };
      $("mode-copy").textContent = copy[state.mode];
      if (!state.store) return;
      await fetch(`/lab/stores/${state.store.id}/mode`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mode: state.mode }),
      });
    });
  });

  $("to-agents").addEventListener("click", async () => {
    if (state.store) {
      await fetch(`/lab/stores/${state.store.id}/mode`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mode: state.mode }),
      });
    }
    show("agents");
  });

  $("swarm").addEventListener("input", () => {
    state.swarm = Number($("swarm").value);
    $("swarm-val").textContent = `${state.swarm} agents`;
  });

  $("attack").addEventListener("click", () => startAttack(false));
  $("enable-enforce").addEventListener("click", async () => {
    state.mode = "enforce";
    document.querySelectorAll("#modes input").forEach((i) => {
      i.checked = i.value === "enforce";
      i.closest(".mode").classList.toggle("on", i.value === "enforce");
    });
    $("mode-copy").textContent = "Bruiser will now actively enforce its decisions.";
    await fetch(`/lab/stores/${state.store.id}/mode`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ mode: "enforce" }),
    });
    show("enforce-live");
    mark("enforce");
    $("mood").textContent = "Okay. Now we’re fighting back.";
    $("block-label").textContent = "BLOCKED";
    await startAttack(true);
  });

  async function startAttack(replay) {
    if (!state.store || state.running) return;
    state.running = true;
    $("feed").innerHTML = "";
    state.sprites = [];
    show("live");
    mark(replay ? "enforce" : "dry");
    $("mood").textContent = replay ? "Okay. Now we’re fighting back." : "They’re getting angry.";
    const res = await fetch(`/lab/stores/${state.store.id}/attacks`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ swarm_size: state.swarm, replay }),
    });
    const data = await res.json();
    if (!res.ok) {
      state.running = false;
      $("mood").textContent = data.error || "Could not start attack";
      return;
    }
    if (state.es) state.es.close();
    const es = new EventSource(data.stream);
    state.es = es;
    es.addEventListener("agent", (ev) => onAgent(JSON.parse(ev.data)));
    es.addEventListener("metrics", (ev) => onMetrics(JSON.parse(ev.data).metrics));
    es.addEventListener("done", (ev) => {
      const payload = JSON.parse(ev.data);
      onMetrics(payload.metrics || payload.summary);
      onDone(payload.summary, replay);
      es.close();
      state.running = false;
    });
    es.onerror = () => {
      /* stream ends after done */
    };
  }

  function onAgent(e) {
    spawn(e);
    const feed = $("feed");
    const row = document.createElement("div");
    row.className = "feed-item";
    const icon = e.icon === "human" ? "👤" : "🤖";
    const cls = e.verdict === "allowed" ? "ok" : e.verdict === "blocked" ? "bad" : "warn";
    row.innerHTML = `<div>${icon}</div>
      <div><div class="who">${esc(e.title)}</div><div class="det">${esc(e.subtitle || "")}${e.detail ? " · " + esc(e.detail) : ""}</div></div>
      <div class="tag ${cls}">${e.verdict === "allowed" ? "✓" : e.verdict === "blocked" ? "✕" : "⚠"} ${esc(e.label || "")}<div class="det">${esc(e.note || "")}</div></div>`;
    feed.prepend(row);
    while (feed.children.length > 40) feed.removeChild(feed.lastChild);
  }

  function onMetrics(m) {
    if (!m) return;
    const map = {
      total: m.total,
      allowed: m.allowed,
      suspicious: m.suspicious,
      would_be_blocked: state.mode === "enforce" ? m.blocked : m.would_be_blocked,
    };
    Object.entries(map).forEach(([k, v]) => {
      const el = document.querySelector(`[data-k="${k}"]`);
      if (el) el.textContent = fmt(v);
    });
  }

  function onDone(sum, replay) {
    if (!sum) return;
    if (!replay) {
      state.dry = sum;
      $("dry-headline").textContent = `${fmt(sum.would_be_blocked)} would have been stopped`;
      $("dry-total").textContent = fmt(sum.total);
      $("dry-sus").textContent = fmt(sum.suspicious);
      $("dry-wbb").textContent = fmt(sum.would_be_blocked);
      $("dry-ok").textContent = fmt(sum.allowed);
      $("mood").textContent = "Attack complete.";
      show("dry-results");
      mark("value");
      return;
    }
    state.enforce = sum;
    $("fin-total").textContent = fmt(sum.total);
    $("fin-ok").textContent = fmt(sum.allowed);
    $("fin-block").textContent = fmt(sum.blocked);
    const without = state.dry ? state.dry.would_be_blocked : sum.blocked;
    $("without").textContent = `${fmt(without)} suspicious agents reach checkout`;
    $("with").textContent = `${fmt(sum.blocked)} suspicious agents stopped`;
    $("mood").textContent = "Bruiser won.";
    show("final");
    show("convert");
  }

  function esc(s) {
    return String(s || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  const canvas = $("swarm-canvas");
  const ctx = canvas.getContext("2d");
  function resize() {
    const wrap = canvas.parentElement;
    canvas.width = wrap.clientWidth * devicePixelRatio;
    canvas.height = wrap.clientHeight * devicePixelRatio;
    ctx.setTransform(devicePixelRatio, 0, 0, devicePixelRatio, 0, 0);
  }
  resize();
  window.addEventListener("resize", resize);

  function spawn(e) {
    const w = canvas.width / devicePixelRatio;
    const h = canvas.height / devicePixelRatio;
    const side = Math.floor(Math.random() * 4);
    let x, y;
    if (side === 0) { x = Math.random() * w; y = -12; }
    else if (side === 1) { x = w + 12; y = Math.random() * h; }
    else if (side === 2) { x = Math.random() * w; y = h + 12; }
    else { x = -12; y = Math.random() * h; }
    const color = e.verdict === "allowed" ? "#3dff8a" : e.verdict === "blocked" ? "#ff4d5a" : "#ffb020";
    state.sprites.push({
      x, y,
      tx: w / 2, ty: h / 2,
      t: 0,
      color,
      icon: e.icon === "human" ? "👤" : "🤖",
      mark: e.verdict === "allowed" ? "✓" : e.verdict === "blocked" ? "✕" : "⚠",
      done: false,
    });
    if (state.sprites.length > 80) state.sprites.shift();
  }

  function tick() {
    const w = canvas.width / devicePixelRatio;
    const h = canvas.height / devicePixelRatio;
    ctx.clearRect(0, 0, w, h);
    ctx.beginPath();
    ctx.arc(w / 2, h / 2, 46, 0, Math.PI * 2);
    ctx.strokeStyle = "rgba(214,255,74,0.45)";
    ctx.lineWidth = 2;
    ctx.stroke();
    ctx.fillStyle = "rgba(214,255,74,0.08)";
    ctx.fill();
    state.sprites.forEach((s) => {
      s.t += 0.018;
      if (s.t < 1) {
        s.x += (s.tx - s.x) * 0.08;
        s.y += (s.ty - s.y) * 0.08;
      } else {
        s.x += (s.tx + (s.mark === "✓" ? 90 : s.mark === "✕" ? -10 : 40) - s.x) * 0.06;
        s.y += (s.ty + (s.mark === "✓" ? 0 : -70) - s.y) * 0.06;
        s.done = true;
      }
      ctx.font = "16px sans-serif";
      ctx.fillText(s.icon, s.x - 8, s.y + 6);
      if (s.t > 0.85) {
        ctx.fillStyle = s.color;
        ctx.font = "700 12px sans-serif";
        ctx.fillText(s.mark, s.x + 12, s.y);
        ctx.fillStyle = "#f4efe4";
      }
    });
    requestAnimationFrame(tick);
  }
  tick();
})();
