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
