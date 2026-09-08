# Make Bruiser authoritative in 30 minutes

Standing analogue: unverified Arsenal-like club (`docs/07-club-profile-arsenal.md`).
Replace paths after discovery. Authority is the requirement; Embedded / Edge /
Proxy are how you *place* the control layer.

## 1. Pick the method that can close bypass

| Situation | Method |
|-----------|--------|
| You own checkout code | **Embedded** — `sdk/go` `Protect` on hold/purchase |
| Platform box office, you control WAF/CDN | **Edge** — `POST /v1/authorize` (NGINX `auth_request`, Envoy `ext_authz`) |
| You can CNAME the allocation hostname | **Proxy** — `BRUISER_PROXY_ADDR` + `BRUISER_ORIGIN_URL` |

Arsenal-like lab: Edge first, Proxy if they can route the box-office name,
Embedded blocked until a platform hook exists.

## 2. Lock the origin

Allocation routes on origin must reject requests that did not come from the
enforcement front (`X-Bruiser-Origin-Secret` or mTLS). Without this, Authority
Check FAIL.

## 3. Run the Authority Check

```bash
bruiser authority-check --front http://edge-or-proxy --origin http://box-office
```

Overall Result PASS is the go-live gate. Lockdown off is the demo FAIL path.

## 4. Watch EAF

```bash
bruiser eaf-demo --front http://edge-or-proxy --n 2000
```

`observed_eaf` should sit near N×; `downstream_eaf` at 1× under `max_active: 1`.
