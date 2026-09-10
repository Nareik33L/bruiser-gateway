"use strict";

const http = require("http");
const { protect, publicFromJWKS, FenceCache } = require("./index");

const jwksURL = process.env.BRUISER_JWKS_URL || "http://127.0.0.1:8080/.well-known/bruiser/jwks.json";

async function main() {
  const raw = await fetch(jwksURL).then((r) => r.json());
  const key = publicFromJWKS(raw);
  const mw = protect({ key, fences: new FenceCache() });
  const server = http.createServer((req, res) => {
    mw(req, res, () => {
      res.statusCode = 201;
      res.setHeader("Content-Type", "application/json");
      res.end(JSON.stringify({ ok: true, exe: req.bruiser.exe }));
    });
  });
  const port = process.env.PORT || 3000;
  server.listen(port, () => console.log("sample listening", port));
}

if (require.main === module) {
  main().catch((err) => {
    console.error(err);
    process.exit(1);
  });
}
