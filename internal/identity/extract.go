// Package identity extracts a merchant-owned customer from an existing
// request. Bruiser consumes that identifier; it never computes one.
package identity

import (
	"strings"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
)

type Customer struct {
	CustomerID  string
	PrincipalID string
	Anchors     map[string]string
}

func Extract(id merchant.Identity, secret, cookie, bearer, headerVal, principalHdr string) (Customer, error) {
	mode := strings.ToLower(strings.TrimSpace(id.Extractor))
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "cookie-jwt", "cookie":
		return fromJWT(secret, id.SubjectClaim, cookie)
	case "bearer-jwt", "bearer":
		return fromJWT(secret, id.SubjectClaim, bearer)
	case "header":
		return fromHeader(headerVal, principalHdr)
	case "auto":
		if cookie != "" {
			if c, err := fromJWT(secret, id.SubjectClaim, cookie); err == nil {
				return c, nil
			}
		}
		if bearer != "" {
			if c, err := fromJWT(secret, id.SubjectClaim, bearer); err == nil {
				return c, nil
			}
		}
		if headerVal != "" {
			return fromHeader(headerVal, principalHdr)
		}
		return Customer{}, auth.ErrUnauthorized
	default:
		return Customer{}, auth.ErrUnauthorized
	}
}

func fromJWT(secret, claim, raw string) (Customer, error) {
	if raw == "" {
		return Customer{}, auth.ErrUnauthorized
	}
	if claim == "" {
		claim = "sub"
	}
	a, err := auth.ParseAssertionHS256Claim(raw, secret, claim)
	if err != nil {
		return Customer{}, err
	}
	return Customer{CustomerID: a.CustomerID, PrincipalID: a.JTI, Anchors: a.Anchors}, nil
}

func fromHeader(customer, principal string) (Customer, error) {
	customer = strings.TrimSpace(customer)
	if customer == "" {
		return Customer{}, auth.ErrUnauthorized
	}
	return Customer{
		CustomerID:  customer,
		PrincipalID: strings.TrimSpace(principal),
		Anchors:     map[string]string{},
	}, nil
}
