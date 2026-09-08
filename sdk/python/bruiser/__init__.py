"""Bruiser Embedded SDK for Python — verify execution tokens at admission."""

from __future__ import annotations

import base64
import json
from dataclasses import dataclass, field
from typing import Any, Callable, Mapping


class BruiserError(Exception):
    pass


class MissingToken(BruiserError):
    pass


class InvalidToken(BruiserError):
    pass


class StaleFence(BruiserError):
    pass


def _b64url_decode(s: str) -> bytes:
    pad = "=" * (-len(s) % 4)
    return base64.urlsafe_b64decode(s + pad)


class FenceCache:
    def __init__(self) -> None:
        self._last: dict[str, int] = {}

    def accept(self, domain: str, fence: int) -> None:
        prev = self._last.get(domain)
        if prev is not None and fence < prev:
            raise StaleFence("stale fence")
        if prev is None or fence > prev:
            self._last[domain] = fence


def public_from_jwks(doc: Mapping[str, Any]) -> bytes:
    for k in doc.get("keys", []):
        if k.get("kty") == "OKP" and k.get("crv") == "Ed25519" and k.get("x"):
            return _b64url_decode(k["x"])
    raise InvalidToken("no Ed25519 key in JWKS")


def _verify_eddsa(token: str, public: bytes) -> dict[str, Any]:
    try:
        from cryptography.exceptions import InvalidSignature
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
    except ImportError as e:  # pragma: no cover
        raise InvalidToken("cryptography package required for Ed25519 verify") from e

    parts = token.split(".")
    if len(parts) != 3:
        raise InvalidToken("invalid token")
    header_b, payload_b, sig_b = parts
    header = json.loads(_b64url_decode(header_b))
    if header.get("alg") != "EdDSA":
        raise InvalidToken("unexpected alg")
    data = f"{header_b}.{payload_b}".encode("ascii")
    sig = _b64url_decode(sig_b)
    key = Ed25519PublicKey.from_public_bytes(public)
    try:
        key.verify(sig, data)
    except InvalidSignature as e:
        raise InvalidToken("invalid signature") from e
    claims = json.loads(_b64url_decode(payload_b))
    if not claims.get("exe") or not claims.get("sub"):
        raise InvalidToken("missing claims")
    return claims


def verify(token: str, public: bytes) -> dict[str, Any]:
    return _verify_eddsa(token, public)


def token_from(headers: Mapping[str, str], header: str = "X-Bruiser-Execution") -> str:
    lower = {str(k).lower(): v for k, v in headers.items()}
    raw = lower.get(header.lower()) or lower.get("x-bruiser-execution") or ""
    if raw:
        return str(raw)
    auth = lower.get("authorization", "")
    if auth.lower().startswith("bearer "):
        return auth[7:].strip()
    return ""


@dataclass
class Protect:
    public: bytes
    header: str = "X-Bruiser-Execution"
    fences: FenceCache = field(default_factory=FenceCache)

    def __call__(self, headers: Mapping[str, str]) -> dict[str, Any]:
        raw = token_from(headers, self.header)
        if not raw:
            raise MissingToken("missing execution token")
        claims = verify(raw, self.public)
        self.fences.accept(str(claims.get("dom", "")), int(claims.get("fnc") or 0))
        return claims


def asgi_protect(cfg: Protect) -> Callable:
    """Minimal ASGI middleware factory. 401/403 JSON on failure."""

    async def middleware(scope, receive, send, app):
        if scope["type"] != "http":
            await app(scope, receive, send)
            return
        headers = {k.decode(): v.decode() for k, v in scope.get("headers", [])}
        try:
            scope = dict(scope)
            scope["bruiser"] = cfg(headers)
        except MissingToken:
            await _send_json(send, 401, {"error": "missing execution token"})
            return
        except StaleFence:
            await _send_json(send, 403, {"error": "stale fence"})
            return
        except BruiserError:
            await _send_json(send, 401, {"error": "invalid execution token"})
            return
        await app(scope, receive, send)

    def wrapper(app):
        async def inner(scope, receive, send):
            await middleware(scope, receive, send, app)

        return inner

    return wrapper


async def _send_json(send, status: int, body: dict[str, Any]) -> None:
    raw = json.dumps(body).encode()
    await send(
        {
            "type": "http.response.start",
            "status": status,
            "headers": [(b"content-type", b"application/json")],
        }
    )
    await send({"type": "http.response.body", "body": raw})
