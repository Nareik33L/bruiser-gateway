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

func TestExtractHeaderMissing(t *testing.T) {
	id := merchant.Identity{Extractor: "header"}
	if _, err := Extract(id, "", "", "", "", ""); err == nil {
		t.Fatal("want unauthorized")
	}
}
