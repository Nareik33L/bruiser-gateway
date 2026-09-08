package adapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPHold(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/holds" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"hold_id":"h1"}`))
	}))
	t.Cleanup(srv.Close)
	h := HTTP{Origin: srv.URL}
	got, err := h.Hold(context.Background(), HoldRequest{CustomerID: "48291", Resource: "event:1", Seats: 1})
	if err != nil || !got.OK || got.HoldID != "h1" {
		t.Fatalf("%+v %v", got, err)
	}
}
