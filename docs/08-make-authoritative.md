# Plug Bruiser in between your stack and scarce inventory

Bruiser is a drop-in control layer. You do not rebuild checkout, wait on a
platform vendor, or invent a new admission path. You declare which existing
routes reserve or purchase scarce inventory, place Bruiser on those routes,
and lock the origin so traffic cannot skip it.

Authority is the requirement. Embedded, Edge and Proxy are how you *place*
the layer in an existing setup.

```
clients (browser / app / agents)
        │
        ▼
  your edge, CNAME, or checkout code
        │
        ▼
     Bruiser          ← lease, policy, fence
        │
        ▼
  your existing origin (box office / inventory)
```

## 1. Write a profile (10 minutes)

Copy `configs/example.yaml` or run `bruiser profile init`. You need three things:

1. **Identity** Bruiser already has — a cookie JWT, a bearer JWT, or a header
   your edge already sets (`X-Customer-Id`). Bruiser consumes that identifier;
   it never mints one.
2. **Allocation routes** — the hold/purchase paths that exist today. Discovery,
   search, login, and static assets are left unmatched and **pass through**.
3. **Policy** — usually one concurrent execution per customer per resource.

```bash
bruiser profile validate configs/example.yaml
export BRUISER_PROFILE=configs/example.yaml
```

`unmatched: allow` (the default) is what makes this a plug-in: only listed
allocation routes fail closed. Everything else is forwarded unchanged.

## 2. Place it in the path you already have

| Your setup today | Placement |
|------------------|-----------|
| You own the hold/purchase code | **Embedded** — one `Protect` call (Go / Node / Python) |
| You already have NGINX, Envoy, Kong, Cloudflare, AWS API GW | **Edge** — `POST /v1/authorize` (`deploy/edge/`) |
| You can CNAME or reverse-proxy the allocation hostname | **Proxy** — `BRUISER_PROXY_ADDR` + `BRUISER_ORIGIN_URL` |

You do not need a new admission API. Unaware clients keep using your session
cookie or customer header; Bruiser acquires the lease transparently.

```bash
export BRUISER_DATABASE_URL=postgres://bruiser:bruiser@127.0.0.1:5432/bruiser?sslmode=disable
export BRUISER_PROXY_ADDR=:8081
export BRUISER_ORIGIN_URL=http://127.0.0.1:8090   # your origin
bruiser serve
```

## 3. Lock the origin

Allocation on origin must reject requests that did not pass Bruiser
(`X-Bruiser-Origin-Secret` or mTLS). Without this, anyone going straight to
origin bypasses the layer and Authority Check FAIL.

## 4. Prove it

```bash
bruiser authority-check --front http://edge-or-proxy --origin http://your-origin
bruiser swarm --front http://edge-or-proxy --n 200
```

PASS plus observed EAF ≈ N× and downstream EAF = 1× is the go-live gate.

Dashboard: `GET /admin`.

## Identity extractors

| `identity.extractor` | What Bruiser reads |
|----------------------|--------------------|
| `auto` (default) | Cookie JWT, then `Authorization: Bearer`, then `identity.header` |
| `cookie-jwt` | Named session cookie (HS256 JWT, `subject_claim`) |
| `bearer-jwt` | Bearer token, same JWT rules |
| `header` | Raw customer id (`X-Customer-Id` by default) |

Optional `principal_header` distinguishes two agents of the same customer
(BUSY). Same cookie `jti` resumes (ALREADY_HELD / heartbeat).

The Arsenal-like file (`configs/arsenal.yaml`) is a lab example of this same
plug-in, not a prerequisite and not a customer claim.
