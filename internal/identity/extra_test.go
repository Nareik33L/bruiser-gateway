package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
)

func TestEdgeSignedIdentity(t *testing.T) {
	raw := SignEdgeIdentity("edge-secret", "48291", time.Hour)
	got, err := fromEdgeSigned(raw, "edge-secret")
	if err != nil || got.CustomerID != "48291" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := fromEdgeSigned(raw, "wrong"); err == nil {
		t.Fatal("want unauthorized")
	}
	if _, err := fromEdgeSigned("v1:48291:1:dead", "edge-secret"); err == nil {
		t.Fatal("want expired/bad mac")
	}
}

func TestIntrospectExtractor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "customer_id": "48291"})
	}))
	t.Cleanup(srv.Close)
	got, err := ExtractInput(context.Background(), Input{
		Identity: merchant.Identity{Extractor: "introspect", IntrospectURL: srv.URL},
		Bearer:   "opaque-session",
		HTTP:     srv.Client(),
	})
	if err != nil || got.CustomerID != "48291" {
		t.Fatalf("%+v %v", got, err)
	}
}
