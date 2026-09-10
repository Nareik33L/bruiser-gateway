package attacklab

import (
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
)

func TestRoutesMatchSandboxDrop(t *testing.T) {
	p := merchant.Empty("lab")
	p.Routes = append(p.Routes, Routes()...)
	r, _, ok := p.MatchRoute("GET", "/s/str-abc")
	if !ok || r.Controlled() {
		t.Fatalf("product page should be uncontrolled: ok=%v controlled=%v", ok, r.Controlled())
	}
	r, params, ok := p.MatchRoute("POST", "/s/str-abc/drops/wav1/checkout")
	if !ok || r.Action != "purchase" {
		t.Fatalf("checkout route ok=%v action=%s", ok, r.Action)
	}
	if got := r.ResourceFor(params, ""); got != "drop:str-abc-wav1" {
		t.Fatalf("resource %s", got)
	}
}
