package shared

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const BoxOfficeCookie = "boxoffice_session"

// Claims is a small HS256 assertion used for club→SimTix SSO and SimTix→Bruiser
// box-office cookies. Shape of the box-office cookie matches the product's
// IssueBoxOfficeSession so /v1/authorize can extract customer = sub.
type Claims struct {
	Issuer    string
	Audience  string
	Subject   string
	JTI       string
	Name      string
	Email     string
	Eligible  bool
	ExpiresAt time.Time
}

func NewJTI() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "jti_" + hex.EncodeToString(b[:])
}

func Issue(secret string, c Claims, ttl time.Duration) (string, error) {
	if ttl == 0 {
		ttl = time.Hour
	}
	if c.JTI == "" {
		c.JTI = NewJTI()
	}
	if c.Issuer == "" {
		c.Issuer = "dev"
	}
	if c.Audience == "" {
		c.Audience = "bruiser"
	}
	claims := jwt.MapClaims{
		"sub": c.Subject,
		"iss": c.Issuer,
		"aud": c.Audience,
		"iat": time.Now().UTC().Unix(),
		"exp": time.Now().UTC().Add(ttl).Unix(),
		"jti": c.JTI,
	}
	if c.Name != "" {
		claims["name"] = c.Name
	}
	if c.Email != "" {
		claims["email"] = c.Email
	}
	if c.Eligible {
		claims["eligible"] = true
	}
	if c.Audience == "bruiser" {
		claims["bruiser_anchors"] = map[string]string{"membership_no": c.Subject}
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(secret))
}

func Parse(token, secret, audience string) (Claims, error) {
	var raw jwt.MapClaims
	parsed, err := jwt.ParseWithClaims(token, &raw, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected alg")
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !parsed.Valid {
		return Claims{}, fmt.Errorf("invalid token")
	}
	sub, _ := raw.GetSubject()
	if sub == "" {
		return Claims{}, fmt.Errorf("missing sub")
	}
	exp, err := raw.GetExpirationTime()
	if err != nil || exp == nil {
		return Claims{}, fmt.Errorf("missing exp")
	}
	iss, _ := raw.GetIssuer()
	auds, _ := raw.GetAudience()
	if audience != "" {
		ok := false
		for _, a := range auds {
			if a == audience {
				ok = true
				break
			}
		}
		if !ok {
			return Claims{}, fmt.Errorf("unexpected audience")
		}
	}
	out := Claims{
		Issuer:    iss,
		Subject:   sub,
		ExpiresAt: exp.Time,
	}
	if len(auds) > 0 {
		out.Audience = auds[0]
	}
	if v, ok := raw["jti"].(string); ok {
		out.JTI = v
	}
	if v, ok := raw["name"].(string); ok {
		out.Name = v
	}
	if v, ok := raw["email"].(string); ok {
		out.Email = v
	}
	if v, ok := raw["eligible"].(bool); ok {
		out.Eligible = v
	}
	return out, nil
}

func IssueBoxOffice(secret, membershipNo string, ttl time.Duration) (string, error) {
	return Issue(secret, Claims{
		Issuer:   "simtix",
		Audience: "bruiser",
		Subject:  membershipNo,
	}, ttl)
}

func IssueHandoff(secret string, membershipNo, name, email string, eligible bool) (string, error) {
	return Issue(secret, Claims{
		Issuer:   "harchester",
		Audience: "simtix",
		Subject:  membershipNo,
		Name:     name,
		Email:    email,
		Eligible: eligible,
	}, 60*time.Second)
}
