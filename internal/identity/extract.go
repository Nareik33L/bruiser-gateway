// Package identity extracts a merchant-owned customer from an existing
// request. Bruiser consumes that identifier; it never computes one.
package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
)

type Customer struct {
	CustomerID  string
	PrincipalID string
	Anchors     map[string]string
}

type Input struct {
	Identity            merchant.Identity
	Secret              string
	EdgeSecret          string
	Cookie              string
	Bearer              string
	Header              string
	Principal           string
	Introspect          string
	HTTP                *http.Client
	MerchantID          string
	AllowDevAssertions  bool
	AllowUnsignedHeader bool
	TrustedEdge         bool
	RequireIssuer       bool
	RequireAudience     bool
}

func Extract(id merchant.Identity, secret, cookie, bearer, headerVal, principalHdr string) (Customer, error) {
	return ExtractInput(context.Background(), Input{
		Identity:            id,
		Secret:              secret,
		Cookie:              cookie,
		Bearer:              bearer,
		Header:              headerVal,
		Principal:           principalHdr,
		AllowDevAssertions:  true,
		AllowUnsignedHeader: true,
	})
}

func ExtractInput(ctx context.Context, in Input) (Customer, error) {
	id := in.Identity
	mode := strings.ToLower(strings.TrimSpace(id.Extractor))
	if mode == "" && id.Source != "" {
		mode = merchantSource(id.Source)
	}
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "cookie-jwt", "cookie":
		return fromJWT(in, in.Cookie)
	case "bearer-jwt", "bearer", "jwt":
		return fromJWT(in, in.Bearer)
	case "header":
		return fromHeader(in)
	case "edge-signed", "signed-header":
		return fromEdgeSigned(in.Header, firstNonEmpty(in.EdgeSecret, in.Secret))
	case "introspect", "introspection":
		return fromIntrospect(ctx, in)
	case "oidc", "jwks":
		return fromOIDC(ctx, in)
	case "auto":
		if in.Cookie != "" {
			if c, err := fromJWT(in, in.Cookie); err == nil {
				return c, nil
			}
		}
		if in.Bearer != "" {
			if id.JWKSURL != "" {
				if c, err := fromOIDC(ctx, in); err == nil {
					return c, nil
				}
			}
			if c, err := fromJWT(in, in.Bearer); err == nil {
				return c, nil
			}
			if c, err := fromIntrospect(ctx, in); err == nil {
				return c, nil
			}
		}
		if in.Header != "" {
			if c, err := fromEdgeSigned(in.Header, firstNonEmpty(in.EdgeSecret, in.Secret)); err == nil {
				return c, nil
			}
			return fromHeader(in)
		}
		return Customer{}, auth.ErrUnauthorized
	default:
		return Customer{}, auth.ErrUnauthorized
	}
}

func claim(id merchant.Identity) string {
	if id.SubjectClaim != "" {
		return id.SubjectClaim
	}
	if id.Claim != "" {
		return id.Claim
	}
	return "sub"
}

func merchantSource(src string) string {
	switch strings.ToLower(src) {
	case "jwt":
		return "jwt"
	case "cookie":
		return "cookie-jwt"
	case "oidc", "jwks":
		return "oidc"
	default:
		return src
	}
}

func fromJWT(in Input, raw string) (Customer, error) {
	if raw == "" {
		return Customer{}, auth.ErrUnauthorized
	}
	mode := strings.ToLower(strings.TrimSpace(in.Identity.Extractor))
	cookieJWT := mode == "cookie-jwt" || mode == "cookie"
	// HMAC box-office cookies are merchant identity when the extractor is
	// cookie-jwt. Bearer HMAC lab assertions still require DevAssertions.
	if !in.AllowDevAssertions && !cookieJWT {
		return Customer{}, auth.ErrUnauthorized
	}
	opts := auth.AssertionOpts{
		Issuer:     in.Identity.Issuer,
		Audience:   in.Identity.Audience,
		MerchantID: in.MerchantID,
		Claim:      claim(in.Identity),
	}
	if in.RequireIssuer && opts.Issuer == "" {
		return Customer{}, auth.ErrUnauthorized
	}
	if in.RequireAudience && opts.Audience == "" {
		return Customer{}, auth.ErrUnauthorized
	}
	a, err := auth.ParseAssertion(raw, in.Secret, opts)
	if err != nil && opts.Claim != "sub" && opts.Claim != "customer_id" {
		return Customer{}, err
	}
	if err != nil {
		opts.Claim = "customer_id"
		a, err = auth.ParseAssertion(raw, in.Secret, opts)
	}
	if err != nil {
		opts.Claim = "sub"
		a, err = auth.ParseAssertion(raw, in.Secret, opts)
	}
	if err != nil {
		return Customer{}, err
	}
	if in.MerchantID != "" && a.MerchantID != "" && a.MerchantID != in.MerchantID {
		return Customer{}, auth.ErrUnauthorized
	}
	return Customer{CustomerID: a.CustomerID, PrincipalID: a.JTI, Anchors: a.Anchors}, nil
}

func fromHeader(in Input) (Customer, error) {
	if !in.TrustedEdge && !in.AllowUnsignedHeader {
		return Customer{}, auth.ErrUnauthorized
	}
	customer := strings.TrimSpace(in.Header)
	if customer == "" {
		return Customer{}, auth.ErrUnauthorized
	}
	return Customer{
		CustomerID:  customer,
		PrincipalID: strings.TrimSpace(in.Principal),
		Anchors:     map[string]string{},
	}, nil
}

// Edge-signed identity: v1:<customer_id>:<exp_unix>:<hmac_hex>
// MAC is HMAC-SHA256(secret, "v1\n"+customer_id+"\n"+exp).
func SignEdgeIdentity(secret, customerID string, ttl time.Duration) string {
	if ttl <= 0 {
		ttl = time.Hour
	}
	exp := time.Now().UTC().Add(ttl).Unix()
	expS := strconv.FormatInt(exp, 10)
	mac := edgeMAC(secret, customerID, expS)
	return "v1:" + customerID + ":" + expS + ":" + mac
}

func fromEdgeSigned(raw, secret string) (Customer, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || secret == "" {
		return Customer{}, auth.ErrUnauthorized
	}
	parts := strings.Split(raw, ":")
	if len(parts) != 4 || parts[0] != "v1" {
		return Customer{}, auth.ErrUnauthorized
	}
	customer, expS, sig := parts[1], parts[2], parts[3]
	if customer == "" {
		return Customer{}, auth.ErrUnauthorized
	}
	exp, err := strconv.ParseInt(expS, 10, 64)
	if err != nil || time.Now().UTC().Unix() > exp {
		return Customer{}, auth.ErrUnauthorized
	}
	want := edgeMAC(secret, customer, expS)
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return Customer{}, auth.ErrUnauthorized
	}
	return Customer{CustomerID: customer, Anchors: map[string]string{}}, nil
}

func edgeMAC(secret, customer, exp string) string {
	m := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(m, "v1\n%s\n%s", customer, exp)
	return hex.EncodeToString(m.Sum(nil))
}

func fromIntrospect(ctx context.Context, in Input) (Customer, error) {
	url := in.Identity.IntrospectURL
	if url == "" {
		url = in.Introspect
	}
	tok := in.Bearer
	if tok == "" {
		tok = in.Cookie
	}
	if url == "" || tok == "" {
		return Customer{}, auth.ErrUnauthorized
	}
	client := in.HTTP
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second}
	}
	form := strings.NewReader("token=" + tok)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, form)
	if err != nil {
		return Customer{}, auth.ErrUnauthorized
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if in.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+in.Secret)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Customer{}, auth.ErrUnauthorized
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return Customer{}, auth.ErrUnauthorized
	}
	if active, ok := doc["active"].(bool); ok && !active {
		return Customer{}, auth.ErrUnauthorized
	}
	for _, k := range []string{"customer_id", "sub", "username"} {
		if s, ok := doc[k].(string); ok && s != "" {
			return Customer{CustomerID: s, Anchors: map[string]string{}}, nil
		}
	}
	return Customer{}, auth.ErrUnauthorized
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
