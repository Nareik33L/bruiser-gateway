package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

type CustomerAssertion struct {
	CustomerID string
	MerchantID string
	Anchors    map[string]string
	ExpiresAt  time.Time
}

type SessionClaims struct {
	MerchantID    string            `json:"mid"`
	CustomerID    string            `json:"sub"`
	PrincipalType string            `json:"pty"`
	PrincipalID   string            `json:"pid"`
	SessionID     string            `json:"sid"`
	Anchors       map[string]string `json:"anc,omitempty"`
	jwt.RegisteredClaims
}

type ExecutionClaims struct {
	MerchantID  string `json:"mid"`
	CustomerID  string `json:"sub"`
	ExecutionID string `json:"exe"`
	Domain      string `json:"dom"`
	Resource    string `json:"res"`
	Action      string `json:"act"`
	Principal   string `json:"prn"`
	Fence       int64  `json:"fnc"`
	jwt.RegisteredClaims
}

type Signer struct {
	KID        string
	MerchantID string
	Private    ed25519.PrivateKey
	Public     ed25519.PublicKey
}

func (s Signer) SignSession(claims SessionClaims) (string, error) {
	claims.RegisteredClaims.Issuer = "bruiser/" + s.MerchantID
	claims.RegisteredClaims.ID = id.New("jti")
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tok.Header["kid"] = s.KID
	return tok.SignedString(s.Private)
}

func (s Signer) SignExecution(e lease.Execution) (string, error) {
	claims := ExecutionClaims{
		MerchantID:  e.MerchantID,
		CustomerID:  e.CustomerID,
		ExecutionID: e.ID,
		Domain:      e.DomainKey,
		Resource:    e.Resource,
		Action:      e.Action,
		Principal:   e.Principal.Type + ":" + e.Principal.ID,
		Fence:       e.Fence,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "bruiser/" + e.MerchantID,
			Subject:   e.CustomerID,
			ExpiresAt: jwt.NewNumericDate(e.ExpiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ID:        id.New("jti"),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tok.Header["kid"] = s.KID
	return tok.SignedString(s.Private)
}

func ParseSession(token string, pub ed25519.PublicKey) (SessionClaims, error) {
	var claims SessionClaims
	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodEdDSA.Alg() {
			return nil, fmt.Errorf("%w: unexpected alg", ErrUnauthorized)
		}
		return pub, nil
	})
	if err != nil || !parsed.Valid {
		return SessionClaims{}, ErrUnauthorized
	}
	if claims.ExpiresAt != nil && time.Now().UTC().After(claims.ExpiresAt.Time) {
		return SessionClaims{}, ErrUnauthorized
	}
	return claims, nil
}

func ParseAssertionHS256(token, secret, audience string) (CustomerAssertion, error) {
	var claims jwt.MapClaims
	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("%w: unexpected alg", ErrUnauthorized)
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !parsed.Valid {
		return CustomerAssertion{}, ErrUnauthorized
	}
	sub, _ := claims.GetSubject()
	if sub == "" {
		return CustomerAssertion{}, ErrUnauthorized
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return CustomerAssertion{}, ErrUnauthorized
	}
	out := CustomerAssertion{
		CustomerID: sub,
		Anchors:    map[string]string{},
		ExpiresAt:  exp.Time,
	}
	if aud, err := claims.GetAudience(); err == nil && len(aud) > 0 {
		_ = audience
	}
	if raw, ok := claims["bruiser_anchors"].(map[string]any); ok {
		for k, v := range raw {
			if s, ok := v.(string); ok {
				out.Anchors[k] = s
			}
		}
	}
	return out, nil
}

func JWKS(kid string, pub ed25519.PublicKey) map[string]any {
	return map[string]any{
		"keys": []map[string]any{{
			"kty": "OKP",
			"crv": "Ed25519",
			"alg": "EdDSA",
			"use": "sig",
			"kid": kid,
			"x":   base64.RawURLEncoding.EncodeToString(pub),
		}},
	}
}

func IssueDevAssertion(secret, customerID string, ttl time.Duration, anchors map[string]string) (string, error) {
	claims := jwt.MapClaims{
		"sub": customerID,
		"iss": "dev",
		"aud": "bruiser",
		"iat": time.Now().UTC().Unix(),
		"exp": time.Now().UTC().Add(ttl).Unix(),
	}
	if len(anchors) > 0 {
		claims["bruiser_anchors"] = anchors
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(secret))
}
