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
}
