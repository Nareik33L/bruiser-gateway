# Bruiser Embedded SDKs

These libraries verify Bruiser execution tokens at a merchant admission
point. Bruiser is not sold as middleware; this is how a club that owns
checkout *places* the control layer.

| Language | Path | Verify | Fence |
|----------|------|--------|-------|
| Go | `sdk/go` | EdDSA JWT + JWKS | `FenceCache` |
| Node | `sdk/node` | Node `crypto` JWK | `FenceCache` |
| Python | `sdk/python` | `cryptography` Ed25519 | `FenceCache` |

Protocol and SDKs are intended Apache-2.0 after legal review. No licence
file is committed yet.

```bash
# Node
cd sdk/node && node --test test.js

# Python (needs: pip install cryptography)
cd sdk/python && python -m unittest tests/test_protect.py
```

Samples: `sdk/node/sample.js`, `sdk/python/sample/app.py`. Point
`BRUISER_JWKS_URL` at `/.well-known/bruiser/jwks.json`.
