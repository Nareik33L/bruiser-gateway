package identity

import (
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
)

func TestExtractAutoPrefersCookieThenHeader(t *testing.T) {
	tok, err := auth.IssueDevAssertion("s", "alice", time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := merchant.Identity{Extractor: "auto", Header: "X-Customer-Id", SubjectClaim: "sub"}
	got, err := Extract(id, "s", tok, "", "ignored", "")
	if err != nil || got.CustomerID != "alice" {
		t.Fatalf("cookie jwt: %+v %v", got, err)
	}
	got, err = Extract(id, "s", "", "", "header-cust", "p1")
	if err != nil || got.CustomerID != "header-cust" || got.PrincipalID != "p1" {
		t.Fatalf("header: %+v %v", got, err)
	}
}

func TestExtractCookieJWTWithoutDevAssertions(t *testing.T) {
	tok, err := auth.IssueBoxOfficeSession("s", "1001234", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	id := merchant.Identity{Extractor: "cookie-jwt", Cookie: "boxoffice_session", SubjectClaim: "sub"}
	got, err := ExtractInput(t.Context(), Input{
		Identity:           id,
		Secret:             "s",
		Cookie:             tok,
		AllowDevAssertions: false,
	})
	if err != nil || got.CustomerID != "1001234" {
		t.Fatalf("cookie-jwt must work with DevAssertions off: %+v %v", got, err)
	}
	if _, err := ExtractInput(t.Context(), Input{
		Identity:           merchant.Identity{Extractor: "auto"},
		Secret:             "s",
		Bearer:             tok,
		AllowDevAssertions: false,
	}); err == nil {
		t.Fatal("auto bearer HMAC must still require DevAssertions")
	}
}

func TestExtractHeaderMissing(t *testing.T) {
	id := merchant.Identity{Extractor: "header"}
	if _, err := Extract(id, "", "", "", "", ""); err == nil {
		t.Fatal("want unauthorized")
	}
}
