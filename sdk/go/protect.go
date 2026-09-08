// Package bruiser is the Go Embedded SDK: verify execution tokens at the
// merchant's admission point. Bruiser is not sold as middleware; this is how a
// club that owns checkout *places* the control layer.
package bruiser

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
)

var (
	ErrMissingToken = errors.New("missing execution token")
	ErrStaleFence   = errors.New("stale fence")
)

type FenceCache struct {
	mu   sync.Mutex
	last map[string]int64
}

func (c *FenceCache) Accept(domain string, fence int64) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil {
		c.last = map[string]int64{}
	}
	if prev, ok := c.last[domain]; ok && fence < prev {
		return ErrStaleFence
	}
	if fence > c.last[domain] {
		c.last[domain] = fence
	}
	return nil
}

type ProtectConfig struct {
	Public ed25519.PublicKey
	Header string // default X-Bruiser-Execution
	Fences *FenceCache
}

func TokenFrom(r *http.Request, header string) string {
	if header == "" {
		header = "X-Bruiser-Execution"
	}
	if v := r.Header.Get(header); v != "" {
		return v
	}
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func Verify(token string, pub ed25519.PublicKey) (auth.ExecutionClaims, error) {
	return auth.ParseExecution(token, pub)
}

func Protect(cfg ProtectConfig) func(http.Handler) http.Handler {
	if cfg.Header == "" {
		cfg.Header = "X-Bruiser-Execution"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := TokenFrom(r, cfg.Header)
			if raw == "" {
				http.Error(w, `{"error":"missing execution token"}`, http.StatusUnauthorized)
				return
			}
			claims, err := Verify(raw, cfg.Public)
			if err != nil {
				http.Error(w, `{"error":"invalid execution token"}`, http.StatusUnauthorized)
				return
			}
			if err := cfg.Fences.Accept(claims.Domain, claims.Fence); err != nil {
				http.Error(w, `{"error":"stale fence"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// PublicFromJWKS extracts the first OKP/Ed25519 key. Lab helper; production
// should cache by kid.
func PublicFromJWKS(raw []byte) (ed25519.PublicKey, error) {
	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Crv string `json:"crv"`
			X   string `json:"x"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	for _, k := range doc.Keys {
		if k.Kty == "OKP" && k.Crv == "Ed25519" && k.X != "" {
			b, err := base64.RawURLEncoding.DecodeString(k.X)
			if err != nil {
				return nil, err
			}
			return ed25519.PublicKey(b), nil
		}
	}
	return nil, errors.New("no Ed25519 key in JWKS")
}
