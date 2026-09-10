package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/demos/seed"
)

func TestAccountCapExceeded(t *testing.T) {
	cases := []struct {
		limit, held, seats int
		want               bool
	}{
		{4, 4, 1, true},
		{4, 3, 1, false},
		{4, 0, 4, false},
		{4, 0, 5, true},
		{0, 100, 1, false},
		{-1, 10, 1, false},
	}
	for _, tc := range cases {
		if got := accountCapExceeded(tc.limit, tc.held, tc.seats); got != tc.want {
			t.Fatalf("accountCapExceeded(%d,%d,%d)=%v want %v", tc.limit, tc.held, tc.seats, got, tc.want)
		}
	}
}

func TestPerAccountCapAdmin(t *testing.T) {
	o := newOrigin(originConfig{AdminSecret: "demo-admin-dev"})
	if o.perAccountLimit != seed.PerAccountLimit {
		t.Fatalf("default cap %d want %d", o.perAccountLimit, seed.PerAccountLimit)
	}
	srv := httptest.NewServer(o.handler())
	defer srv.Close()

	get := func() int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/_admin/per-account-cap", nil)
		req.Header.Set("X-Demo-Admin-Secret", "demo-admin-dev")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %d", resp.StatusCode)
		}
		var body struct {
			Limit int `json:"limit"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body.Limit
	}
	put := func(n int) {
		t.Helper()
		b, _ := json.Marshal(map[string]int{"limit": n})
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/_admin/per-account-cap", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Demo-Admin-Secret", "demo-admin-dev")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PUT %d", resp.StatusCode)
		}
	}

	if got := get(); got != 4 {
		t.Fatalf("GET default %d want 4", got)
	}
	put(0)
	if got := get(); got != 0 {
		t.Fatalf("after Off %d want 0", got)
	}
	put(4)
	if got := get(); got != 4 {
		t.Fatalf("after On %d want 4", got)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/_admin/per-account-cap", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing secret %d want 401", resp.StatusCode)
	}
}
