"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const { spawnSync } = require("child_process");
const path = require("path");
const { publicFromJWKS, verify, FenceCache, protect } = require("./index");

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

