// Cloudflare Worker placement: call Bruiser /v1/authorize, then origin.
// Secrets: BRUISER_URL, BRUISER_EDGE_SECRET, ORIGIN_URL
export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const auth = await fetch(`${env.BRUISER_URL}/v1/authorize`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "X-Bruiser-Edge-Secret": env.BRUISER_EDGE_SECRET,
        cookie: request.headers.get("cookie") || "",
        authorization: request.headers.get("authorization") || "",
        "X-Customer-Id": request.headers.get("X-Customer-Id") || "",
        "X-Bruiser-Identity": request.headers.get("X-Bruiser-Identity") || "",
      },
      body: JSON.stringify({ method: request.method, path: url.pathname }),
    });
    if (!auth.ok) {
      return new Response(await auth.text(), {
        status: auth.status,
        headers: { "content-type": auth.headers.get("content-type") || "application/json" },
      });
    }
    const dest = new URL(url.pathname + url.search, env.ORIGIN_URL);
    const headers = new Headers(request.headers);
    for (const name of [
      "X-Bruiser-Execution",
      "X-Bruiser-Fence",
      "X-Bruiser-Customer",
      "X-Bruiser-Origin-Secret",
    ]) {
      const v = auth.headers.get(name);
      if (v) headers.set(name, v);
    }
    return fetch(dest, { method: request.method, headers, body: request.body });
  },
};
