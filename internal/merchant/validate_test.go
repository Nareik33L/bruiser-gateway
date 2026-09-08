package merchant

import "testing"

func TestExampleProfileValidates(t *testing.T) {
	p, err := LoadFile("../../configs/example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMissingRoutes(t *testing.T) {
	p := Empty("x")
	if err := p.Validate(); err == nil {
		t.Fatal("expected missing allocation routes to fail")
	}
}
