package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
)

type jwksDoc struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Crv string `json:"crv"`
	N   string `json:"n"`
	E   string `json:"e"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type jwksCache struct {
	mu      sync.Mutex
	keys    map[string]jwksDoc
	fetched map[string]time.Time
}

var globalJWKS = &jwksCache{
	keys:    map[string]jwksDoc{},
	fetched: map[string]time.Time{},
}

func (c *jwksCache) get(ctx context.Context, client *http.Client, url string) (jwksDoc, error) {
	c.mu.Lock()
	if doc, ok := c.keys[url]; ok && time.Since(c.fetched[url]) < 5*time.Minute {
		c.mu.Unlock()
		return doc, nil
	}
	c.mu.Unlock()

	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return jwksDoc{}, auth.ErrUnauthorized
	}
	resp, err := client.Do(req)
	if err != nil {
		return jwksDoc{}, auth.ErrUnauthorized
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return jwksDoc{}, auth.ErrUnauthorized
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return jwksDoc{}, auth.ErrUnauthorized
	}
	var doc jwksDoc
	if json.Unmarshal(raw, &doc) != nil || len(doc.Keys) == 0 {
		return jwksDoc{}, auth.ErrUnauthorized
	}
	c.mu.Lock()
	c.keys[url] = doc
	c.fetched[url] = time.Now()
	c.mu.Unlock()
	return doc, nil
}

func fromOIDC(ctx context.Context, in Input) (Customer, error) {
	tok := in.Bearer
	if tok == "" {
		tok = in.Cookie
	}
	url := in.Identity.JWKSURL
	if url == "" || tok == "" {
		return Customer{}, auth.ErrUnauthorized
	}
	doc, err := globalJWKS.get(ctx, in.HTTP, url)
	if err != nil {
		return Customer{}, err
	}
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(tok, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		key, err := matchJWK(doc, kid, t.Method.Alg())
		if err != nil {
			return nil, err
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"RS256", "EdDSA", "ES256"}))
	if err != nil || parsed == nil || !parsed.Valid {
		return Customer{}, auth.ErrUnauthorized
	}
	if iss := strings.TrimSpace(in.Identity.Issuer); iss != "" {
		got, _ := claims.GetIssuer()
		if got != iss {
			return Customer{}, auth.ErrUnauthorized
		}
	}
	if aud := strings.TrimSpace(in.Identity.Audience); aud != "" {
		ok := false
		if list, err := claims.GetAudience(); err == nil {
			for _, a := range list {
				if a == aud {
					ok = true
					break
				}
			}
		}
		if !ok {
			return Customer{}, auth.ErrUnauthorized
		}
	}
	name := claim(in.Identity)
	sub, _ := claims[name].(string)
	if sub == "" {
		sub, _ = claims["customer_id"].(string)
	}
	if sub == "" {
		sub, _ = claims["sub"].(string)
	}
	if sub == "" {
		return Customer{}, auth.ErrUnauthorized
	}
	jti, _ := claims["jti"].(string)
	return Customer{CustomerID: sub, PrincipalID: jti, Anchors: map[string]string{}}, nil
}

func matchJWK(doc jwksDoc, kid, alg string) (any, error) {
	for _, k := range doc.Keys {
		if kid != "" && k.Kid != "" && k.Kid != kid {
			continue
		}
		if k.Alg != "" && alg != "" && k.Alg != alg {
			continue
		}
		switch {
		case k.Kty == "RSA":
			return rsaFromJWK(k)
		case k.Kty == "OKP" && (k.Crv == "Ed25519" || alg == "EdDSA"):
			return edFromJWK(k)
		case k.Kty == "EC" && k.Crv == "P-256":
			return ecFromJWK(k)
		}
	}
	return nil, fmt.Errorf("%w: no matching jwk", auth.ErrUnauthorized)
}

func rsaFromJWK(k jwk) (*rsa.PublicKey, error) {
	n, err := b64int(k.N)
	if err != nil {
		return nil, auth.ErrUnauthorized
	}
	e, err := b64int(k.E)
	if err != nil || !e.IsInt64() {
		return nil, auth.ErrUnauthorized
	}
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}

func edFromJWK(k jwk) (ed25519.PublicKey, error) {
	b, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, auth.ErrUnauthorized
	}
	return ed25519.PublicKey(b), nil
}

func ecFromJWK(k jwk) (*ecdsa.PublicKey, error) {
	x, err := b64int(k.X)
	if err != nil {
		return nil, auth.ErrUnauthorized
	}
	y, err := b64int(k.Y)
	if err != nil {
		return nil, auth.ErrUnauthorized
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
}

func b64int(s string) (*big.Int, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(b), nil
}
