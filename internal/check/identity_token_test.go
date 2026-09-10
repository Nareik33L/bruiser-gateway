package check

import (
	"strings"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
)

func TestCustomerHeadersPreferIdentityToken(t *testing.T) {
	c := Config{IdentityToken: "eyJstaging.oidc.jwt", CookieName: "idp_session", HMACSecret: "unused"}
	hdr, err := c.customerHeaders("ignored")
	if err != nil {
		t.Fatal(err)
	}
	if hdr["Authorization"] != "Bearer eyJstaging.oidc.jwt" {
		t.Fatalf("bearer: %v", hdr)
	}
	if hdr["Cookie"] != "idp_session=eyJstaging.oidc.jwt" {
		t.Fatalf("cookie: %v", hdr)
	}
}

func TestCustomerHeadersLabHMACCookie(t *testing.T) {
	c := Config{HMACSecret: "lab-hmac-not-a-placeholder", Membership: "1001234"}
	hdr, err := c.customerHeaders("1001234")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hdr["Authorization"]; ok {
		t.Fatal("lab HMAC path must not set Authorization")
	}
	if !strings.HasPrefix(hdr["Cookie"], simtix.CookieName+"=") {
		t.Fatalf("cookie: %v", hdr)
	}
}
