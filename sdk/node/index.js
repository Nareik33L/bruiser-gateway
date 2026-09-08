"use strict";

const crypto = require("crypto");

class FenceCache {
  constructor() {
    this.last = new Map();
  }
  accept(domain, fence) {
    const prev = this.last.get(domain);
    if (prev !== undefined && fence < prev) {
      const err = new Error("stale fence");
      err.code = "STALE_FENCE";
      throw err;
    }
    if (prev === undefined || fence > prev) {
      this.last.set(domain, fence);
    }
  }
}

function tokenFrom(req, header) {
  const name = header || "x-bruiser-execution";
  const headers = req.headers || {};
  const direct = headers[name] || headers["x-bruiser-execution"];
  if (direct) return String(direct);
  const auth = headers.authorization || "";
  if (auth.toLowerCase().startsWith("bearer ")) {
    return auth.slice(7).trim();
  }
  return "";
}

function b64url(s) {
  return Buffer.from(s, "base64url");
}

function publicFromJWKS(doc) {
  const keys = (doc && doc.keys) || [];
  for (const k of keys) {
    if (k.kty === "OKP" && k.crv === "Ed25519" && k.x) {
      return crypto.createPublicKey({ key: k, format: "jwk" });
    }
  }
  throw new Error("no Ed25519 key in JWKS");
}

function publicFromRaw(raw32) {
  const x = Buffer.isBuffer(raw32) ? raw32 : Buffer.from(raw32, "base64url");
  return crypto.createPublicKey({
    key: { kty: "OKP", crv: "Ed25519", x: x.toString("base64url") },
    format: "jwk",
  });
}

function verify(token, key) {
  const parts = String(token).split(".");
  if (parts.length !== 3) {
    throw new Error("invalid token");
  }
  const [h, p, s] = parts;
  const header = JSON.parse(b64url(h).toString("utf8"));
  if (header.alg !== "EdDSA") {
    throw new Error("unexpected alg");
  }
  const data = Buffer.from(`${h}.${p}`);
  const sig = b64url(s);
  const ok = crypto.verify(null, data, key, sig);
  if (!ok) {
    throw new Error("invalid signature");
  }
  const claims = JSON.parse(b64url(p).toString("utf8"));
  if (claims.exp && Date.now() / 1000 > claims.exp) {
    throw new Error("expired");
  }
  if (!claims.exe || !claims.sub) {
    throw new Error("missing claims");
  }
  return claims;
}

function protect({ key, header, fences } = {}) {
  return function (req, res, next) {
    const raw = tokenFrom(req, header);
    if (!raw) {
      res.statusCode = 401;
      res.setHeader("Content-Type", "application/json");
      res.end(JSON.stringify({ error: "missing execution token" }));
      return;
    }
    let claims;
    try {
      claims = verify(raw, key);
    } catch {
      res.statusCode = 401;
      res.setHeader("Content-Type", "application/json");
      res.end(JSON.stringify({ error: "invalid execution token" }));
      return;
    }
    try {
      if (fences) fences.accept(claims.dom, claims.fnc);
    } catch {
      res.statusCode = 403;
      res.setHeader("Content-Type", "application/json");
      res.end(JSON.stringify({ error: "stale fence" }));
      return;
    }
    req.bruiser = claims;
    if (typeof next === "function") next();
  };
}

module.exports = {
  FenceCache,
  tokenFrom,
  publicFromJWKS,
  publicFromRaw,
  verify,
  protect,
};
