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

const Skew = 5 * time.Second

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

type CustomerAssertion struct {
	CustomerID string
	MerchantID string
	Anchors    map[string]string
	ExpiresAt  time.Time
	JTI        string
}

type SessionClaims struct {
	MerchantID    string            `json:"mid"`
	CustomerID    string            `json:"sub"`
	PrincipalType string            `json:"pty"`
	PrincipalID   string            `json:"pid"`
	SessionID     string            `json:"sid"`
	Version       int               `json:"ver,omitempty"`
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

const (
	AccessTokenType = "bruiser_admin"
	RoleAdmin       = "admin"
	RoleOperator    = "operator"
)

type AccessClaims struct {
	MerchantID string `json:"mid"`
	Role       string `json:"role"`
	Actor      string `json:"act"`
	TokenType  string `json:"typ"`
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

func ParseExecution(token string, pub ed25519.PublicKey) (ExecutionClaims, error) {
	var claims ExecutionClaims
	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodEdDSA.Alg() {
			return nil, fmt.Errorf("%w: unexpected alg", ErrUnauthorized)
		}
		return pub, nil
	}, jwt.WithLeeway(Skew), jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}))
	if err != nil || !parsed.Valid {
		return ExecutionClaims{}, ErrUnauthorized
	}
	if claims.ExpiresAt != nil && time.Now().UTC().After(claims.ExpiresAt.Time.Add(Skew)) {
		return ExecutionClaims{}, ErrUnauthorized
	}
	if claims.ExecutionID == "" || claims.CustomerID == "" || claims.MerchantID == "" {
		return ExecutionClaims{}, ErrUnauthorized
	}
	return claims, nil
}

func (s Signer) SignAccess(role, actor string, ttl time.Duration) (string, error) {
	if role != RoleAdmin && role != RoleOperator {
		return "", fmt.Errorf("%w: unknown role", ErrForbidden)
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if ttl > time.Hour {
		ttl = time.Hour
	}
	now := time.Now().UTC()
	claims := AccessClaims{
		MerchantID: s.MerchantID,
		Role:       role,
		Actor:      actor,
		TokenType:  AccessTokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "bruiser/" + s.MerchantID,
			Subject:   actor,
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        id.New("jti"),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tok.Header["kid"] = s.KID
	return tok.SignedString(s.Private)
}

func ParseAccess(token string, pub ed25519.PublicKey) (AccessClaims, error) {
	var claims AccessClaims
	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodEdDSA.Alg() {
			return nil, fmt.Errorf("%w: unexpected alg", ErrUnauthorized)
		}
		return pub, nil
	}, jwt.WithLeeway(Skew), jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}))
	if err != nil || !parsed.Valid {
		return AccessClaims{}, ErrUnauthorized
	}
	if claims.TokenType != AccessTokenType {
		return AccessClaims{}, ErrUnauthorized
	}
	if claims.Role != RoleAdmin && claims.Role != RoleOperator {
		return AccessClaims{}, ErrUnauthorized
	}
	if claims.ExpiresAt != nil && time.Now().UTC().After(claims.ExpiresAt.Time.Add(Skew)) {
		return AccessClaims{}, ErrUnauthorized
	}
	return claims, nil
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
	return ParseAssertionHS256Claim(token, secret, "sub")
}

func ParseAssertionHS256Claim(token, secret, subjectClaim string) (CustomerAssertion, error) {
	var claims jwt.MapClaims
	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("%w: unexpected alg", ErrUnauthorized)
		}
		return []byte(secret), nil
	}, jwt.WithLeeway(Skew), jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !parsed.Valid {
		return CustomerAssertion{}, ErrUnauthorized
	}
	if subjectClaim == "" {
		subjectClaim = "sub"
	}
	sub, _ := claims[subjectClaim].(string)
	if sub == "" {
		if subjectClaim == "sub" {
			sub, _ = claims.GetSubject()
		}
	}
	if sub == "" {
		return CustomerAssertion{}, ErrUnauthorized
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return CustomerAssertion{}, ErrUnauthorized
	}
	if time.Now().UTC().After(exp.Time.Add(Skew)) {
		return CustomerAssertion{}, ErrUnauthorized
	}
	out := CustomerAssertion{
		CustomerID: sub,
		Anchors:    map[string]string{},
		ExpiresAt:  exp.Time,
	}
	if v, ok := claims["jti"].(string); ok {
		out.JTI = v
	}
	if v, ok := claims["mid"].(string); ok {
		out.MerchantID = v
	} else if v, ok := claims["merchant_id"].(string); ok {
		out.MerchantID = v
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

type AssertionOpts struct {
	Issuer     string
	Audience   string
	MerchantID string
	Claim      string
}

func ParseAssertion(token, secret string, opts AssertionOpts) (CustomerAssertion, error) {
	claim := opts.Claim
	if claim == "" {
		claim = "sub"
	}
	a, err := ParseAssertionHS256Claim(token, secret, claim)
	if err != nil {
		return CustomerAssertion{}, err
	}
	if opts.Issuer != "" {
		var claims jwt.MapClaims
		parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
			return []byte(secret), nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithLeeway(Skew))
		if err != nil || parsed == nil {
			return CustomerAssertion{}, ErrUnauthorized
		}
		iss, _ := claims.GetIssuer()
		if iss != opts.Issuer {
			return CustomerAssertion{}, ErrUnauthorized
		}
		if opts.Audience != "" {
			ok := false
			if list, err := claims.GetAudience(); err == nil {
				for _, a := range list {
					if a == opts.Audience {
						ok = true
						break
					}
				}
			}
			if !ok {
				return CustomerAssertion{}, ErrUnauthorized
			}
		}
	} else if opts.Audience != "" {
		var claims jwt.MapClaims
		parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
			return []byte(secret), nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithLeeway(Skew))
		if err != nil || parsed == nil {
			return CustomerAssertion{}, ErrUnauthorized
		}
		ok := false
		if list, err := claims.GetAudience(); err == nil {
			for _, a := range list {
				if a == opts.Audience {
					ok = true
					break
				}
			}
		}
		if !ok {
			return CustomerAssertion{}, ErrUnauthorized
		}
	}
	if opts.MerchantID != "" && a.MerchantID != "" && a.MerchantID != opts.MerchantID {
		return CustomerAssertion{}, ErrUnauthorized
	}
	if opts.MerchantID != "" && a.MerchantID == "" {
		a.MerchantID = opts.MerchantID
	}
	return a, nil
}

func IssueDevAssertion(secret, customerID string, ttl time.Duration, anchors map[string]string) (string, error) {
	claims := jwt.MapClaims{
		"sub": customerID,
		"iss": "dev",
		"aud": "bruiser",
		"iat": time.Now().UTC().Unix(),
		"exp": time.Now().UTC().Add(ttl).Unix(),
		"jti": id.New("jti"),
	}
	if len(anchors) > 0 {
		claims["bruiser_anchors"] = anchors
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(secret))
}

// IssueBoxOfficeSession mints the Arsenal-like box-office cookie: sub is the
// 7-digit membership number. Each call gets a distinct jti so two logins of
// the same member are two unaware principals (BUSY), while a copied cookie
// is one principal (ALREADY_HELD).
func IssueBoxOfficeSession(secret, membershipNo string, ttl time.Duration) (string, error) {
	if ttl == 0 {
		ttl = time.Hour
	}
	return IssueDevAssertion(secret, membershipNo, ttl, map[string]string{
		"membership_no": membershipNo,
	})
}
