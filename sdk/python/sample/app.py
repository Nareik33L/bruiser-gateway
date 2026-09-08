"""Sample ASGI admission point using the Bruiser Python SDK."""

from __future__ import annotations

import json
import os
import urllib.request

from bruiser import Protect, asgi_protect, public_from_jwks


def load_key():
    url = os.environ.get("BRUISER_JWKS_URL", "http://127.0.0.1:8080/.well-known/bruiser/jwks.json")
    with urllib.request.urlopen(url, timeout=5) as resp:
        return public_from_jwks(json.load(resp))


protect = Protect(public=load_key()) if os.environ.get("BRUISER_JWKS_URL") else None


async def app(scope, receive, send):
    if scope["path"] != "/holds":
        await send({"type": "http.response.start", "status": 404, "headers": []})
        await send({"type": "http.response.body", "body": b"not found"})
        return
    await send(
        {
            "type": "http.response.start",
            "status": 201,
            "headers": [(b"content-type", b"application/json")],
        }
    )
    await send({"type": "http.response.body", "body": b'{"ok":true}'})


if protect is not None:
    app = asgi_protect(protect)(app)
