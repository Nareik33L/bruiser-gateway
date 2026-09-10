package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

func TestRoundTripSessionAndExecution(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := Signer{KID: "kid_1", MerchantID: "arsenal", Private: priv, Public: pub}
	tok, err := s.SignSession(SessionClaims{
		MerchantID:    "arsenal",
		CustomerID:    "alice",
		PrincipalType: "agent",
		PrincipalID:   "a1",
		SessionID:     "ses_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseSession(tok, pub)
	if err != nil {
		t.Fatal(err)
	}
	if got.CustomerID != "alice" || got.SessionID != "ses_1" {
		t.Fatalf("%+v", got)
	}

	atok, err := s.SignAccess(RoleOperator, "admin-secret", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ac, err := ParseAccess(atok, pub)
	if err != nil {
		t.Fatal(err)
	}
	if ac.Role != RoleOperator || ac.Actor != "admin-secret" || ac.TokenType != AccessTokenType {
		t.Fatalf("%+v", ac)
	}
	if _, err := ParseAccess(tok, pub); err == nil {
		t.Fatal("session token must not parse as admin access")
	}

	exp := time.Now().UTC().Add(time.Minute)
	etok, err := s.SignExecution(lease.Execution{
		ID: "exe_1", MerchantID: "arsenal", CustomerID: "alice",
		DomainKey: "d", Resource: "event:x", Action: "purchase",
		Principal: lease.Principal{Type: "agent", ID: "a1"},
		Fence:     7, ExpiresAt: exp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if etok == "" {
		t.Fatal("empty execution token")
	}
	parsed, err := ParseExecution(etok, pub)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ExecutionID != "exe_1" || parsed.Fence != 7 {
		t.Fatalf("%+v", parsed)
	}

	assertion, err := IssueDevAssertion("secret", "alice", time.Hour, map[string]string{"membership_no": "1"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := ParseAssertionHS256(assertion, "secret", "bruiser")
	if err != nil {
		t.Fatal(err)
	}
	if a.CustomerID != "alice" || a.Anchors["membership_no"] != "1" {
		t.Fatalf("%+v", a)
	}
	if a.JTI == "" {
		t.Fatal("missing jti")
	}

	c1, err := IssueBoxOfficeSession("secret", "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := IssueBoxOfficeSession("secret", "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}
	b1, err := ParseAssertionHS256(c1, "secret", "bruiser")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := ParseAssertionHS256(c2, "secret", "bruiser")
	if err != nil {
		t.Fatal(err)
	}
	if b1.CustomerID != "1001234" || b1.JTI == b2.JTI {
		t.Fatalf("want distinct jti for two logins: %+v %+v", b1, b2)
	}
}

func TestAssertionIssuerAudienceMerchant(t *testing.T) {
	tok, err := IssueDevAssertion("secret", "alice", time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAssertion(tok, "secret", AssertionOpts{Issuer: "other"}); err == nil {
		t.Fatal("wrong issuer")
	}
	if _, err := ParseAssertion(tok, "secret", AssertionOpts{Issuer: "dev", Audience: "nope"}); err == nil {
		t.Fatal("wrong audience")
	}
	if _, err := ParseAssertion(tok, "secret", AssertionOpts{Issuer: "dev", Audience: "bruiser"}); err != nil {
		t.Fatal(err)
	}
	expired, err := IssueDevAssertion("secret", "alice", -time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAssertionHS256(expired, "secret", "bruiser"); err == nil {
		t.Fatal("expired")
	}
}

func TestExecutionSkewLeeway(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := Signer{KID: "k", MerchantID: "m", Private: priv, Public: pub}
	tok, err := s.SignExecution(lease.Execution{
		ID: "exe", MerchantID: "m", CustomerID: "c",
		DomainKey: "d", Resource: "event:x", Action: "hold",
		Principal: lease.Principal{Type: "agent", ID: "a"},
		Fence:     1, ExpiresAt: time.Now().UTC().Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseExecution(tok, pub); err != nil {
		t.Fatal(err)
	}
}
