package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
)

func TestOIDCExtractorRS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"kid": "k1",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	}))
	t.Cleanup(srv.Close)

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "oidc-alice",
		"iss": "https://idp.example",
		"aud": "bruiser",
		"exp": time.Now().Add(time.Hour).Unix(),
		"jti": "jti-1",
	})
	tok.Header["kid"] = "k1"
	raw, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ExtractInput(context.Background(), Input{
		Identity: merchant.Identity{
			Extractor: "oidc",
			JWKSURL:   srv.URL,
			Issuer:    "https://idp.example",
			Audience:  "bruiser",
		},
		Bearer: raw,
		HTTP:   srv.Client(),
	})
	if err != nil || got.CustomerID != "oidc-alice" {
		t.Fatalf("%+v %v", got, err)
	}

	if _, err := ExtractInput(context.Background(), Input{
		Identity: merchant.Identity{Extractor: "oidc", JWKSURL: srv.URL, Issuer: "https://other"},
		Bearer:   raw,
		HTTP:     srv.Client(),
	}); err == nil {
		t.Fatal("want issuer mismatch")
	}
}
