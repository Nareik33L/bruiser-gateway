"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const { spawnSync } = require("child_process");
const path = require("path");
const { publicFromJWKS, verify, FenceCache, protect, Client } = require("./index");

function mint() {
  const r = spawnSync("go", ["run", "./sdk/mint"], {
    cwd: path.join(__dirname, "..", ".."),
    encoding: "utf8",
  });
  if (r.status !== 0) {
    throw new Error(r.stderr || r.stdout || "mint failed");
  }
  return JSON.parse(r.stdout);
}

test("verify + stale fence", () => {
  let minted;
  try {
    minted = mint();
  } catch (e) {
    if (!process.env.CI) {
      console.log("skip: go mint unavailable", e.message);
      return;
    }
    throw e;
  }
  const key = publicFromJWKS(minted.jwks);
  const claims = verify(minted.ok, key);
  assert.equal(claims.exe, "exe_1");
  assert.equal(claims.fnc, 2);
  const fences = new FenceCache();
  fences.accept(claims.dom, claims.fnc);
  assert.throws(() => {
    const stale = verify(minted.stale, key);
    fences.accept(stale.dom, stale.fnc);
  });

  const inner = (req, res) => {
    res.statusCode = 201;
    res.end("ok");
  };
  const mw = protect({ key, fences: new FenceCache() });
  const missing = mockRes();
  mw({ headers: {} }, missing, inner);
  assert.equal(missing.statusCode, 401);

  const okRes = mockRes();
  mw({ headers: { "x-bruiser-execution": minted.ok } }, okRes, () => {
    okRes.statusCode = 201;
  });
  assert.equal(okRes.statusCode, 201);

  const staleRes = mockRes();
  const fences2 = new FenceCache();
  const mw2 = protect({ key, fences: fences2 });
  mw2({ headers: { "x-bruiser-execution": minted.ok } }, mockRes(), () => {});
  mw2({ headers: { "x-bruiser-execution": minted.stale } }, staleRes, () => {});
  assert.equal(staleRes.statusCode, 403);
});

test("agent client acquire renew release", async () => {
  const { createServer } = require("http");
  const srv = createServer((req, res) => {
    const url = req.url || "";
    res.setHeader("Content-Type", "application/json");
    if (req.method === "POST" && url === "/v1/sessions") {
      res.statusCode = 201;
      res.end(JSON.stringify({ session_id: "ses_1", session_token: "sess", customer_id: "alice" }));
      return;
    }
    if (req.method === "POST" && url === "/v1/executions/acquire") {
      res.statusCode = 201;
      res.end(JSON.stringify({ execution_id: "exe_1", status: "ACTIVE" }));
      return;
    }
    if (req.method === "POST" && url === "/v1/executions/exe_1/renew") {
      res.end(JSON.stringify({ execution_id: "exe_1", status: "ACTIVE" }));
      return;
    }
    if (req.method === "POST" && url === "/v1/executions/exe_1/release") {
      res.end(JSON.stringify({ execution_id: "exe_1", state: "RELEASED" }));
      return;
    }
    res.statusCode = 404;
    res.end("{}");
  });
  await new Promise((resolve) => srv.listen(0, resolve));
  const { port } = srv.address();
  try {
    const c = new Client(`http://127.0.0.1:${port}`);
    const sess = await c.createSession("assert", "agent", "a1");
    assert.equal(sess.session_token, "sess");
    const exe = await c.acquire(sess.session_token, "event:x", "purchase");
    assert.equal(exe.execution_id, "exe_1");
    await c.renew(sess.session_token, exe.execution_id);
    await c.release(sess.session_token, exe.execution_id);
  } finally {
    srv.close();
  }
});

function mockRes() {
  return {
    statusCode: 200,
    headers: {},
    setHeader(k, v) {
      this.headers[k] = v;
    },
    end() {},
  };
}

