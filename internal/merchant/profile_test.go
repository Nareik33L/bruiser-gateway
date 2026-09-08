package merchant

import "testing"

func TestMatchRouteArsenalHolds(t *testing.T) {
	p, err := Parse([]byte(`
merchant_id: arsenal
routes:
  - match: { method: GET, path: "/api/events" }
    action: search
  - match: { method: POST, path: "/api/events/{event}/holds" }
    resource: "event:{event}"
    action: hold
  - match: { method: POST, path: "/api/orders" }
    resource_from: event_id
    resource_prefix: "event:"
    action: purchase
`))
	if err != nil {
		t.Fatal(err)
	}
	r, params, ok := p.MatchRoute("POST", "/api/events/ars-che/holds")
	if !ok || r.Action != "hold" {
		t.Fatalf("hold route: ok=%v action=%s", ok, r.Action)
	}
	if params["event"] != "ars-che" {
		t.Fatalf("params=%v", params)
	}
	if got := r.ResourceFor(params, ""); got != "event:ars-che" {
		t.Fatalf("resource=%s", got)
	}
	r, _, ok = p.MatchRoute("GET", "/api/events")
	if !ok || r.Controlled() {
		t.Fatal("search should be uncontrolled")
	}
	r, _, ok = p.MatchRoute("POST", "/api/orders")
	if !ok || r.Action != "purchase" {
		t.Fatal("orders")
	}
	if got := r.ResourceFor(nil, "ars-che"); got != "event:ars-che" {
		t.Fatalf("order resource=%s", got)
	}
}
