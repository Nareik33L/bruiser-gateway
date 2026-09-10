package shared_test

import (
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/demos/shared"
)

func TestIssueParseBoxOffice(t *testing.T) {
	tok, err := shared.IssueBoxOffice("secret", "1001234", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c, err := shared.Parse(tok, "secret", "bruiser")
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "1001234" || c.JTI == "" {
		t.Fatalf("claims %+v", c)
	}
	tok2, _ := shared.IssueBoxOffice("secret", "1001234", time.Hour)
	c2, _ := shared.Parse(tok2, "secret", "bruiser")
	if c.JTI == c2.JTI {
		t.Fatal("each session must have a unique jti")
	}
}

func TestHandoffExpiryAndAudience(t *testing.T) {
	tok, err := shared.IssueHandoff("sso", "1001234", "Alice Okafor", "alice@example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	c, err := shared.Parse(tok, "sso", "simtix")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Eligible || c.Name != "Alice Okafor" {
		t.Fatalf("%+v", c)
	}
	if _, err := shared.Parse(tok, "sso", "bruiser"); err == nil {
		t.Fatal("wrong audience must fail")
	}
}

func TestEnforcedDeterministic(t *testing.T) {
	id := "1001234"
	a := shared.Enforced(id, 50)
	b := shared.Enforced(id, 50)
	if a != b {
		t.Fatal("must be deterministic")
	}
	if shared.Enforced(id, 0) {
		t.Fatal("0% never")
	}
	if !shared.Enforced(id, 100) {
		t.Fatal("100% always")
	}
	n := 0
	for i := 0; i < 1000; i++ {
		if shared.Enforced(fmtMembership(1000001+i), 10) {
			n++
		}
	}
	if n < 50 || n > 150 {
		t.Fatalf("10%% of 1000 should be ~100, got %d", n)
	}
}

func fmtMembership(n int) string {
	s := "0000000"
	v := itoa(n)
	return s[len(v):] + v
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
