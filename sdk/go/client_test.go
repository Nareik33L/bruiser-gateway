package bruiser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentClientAcquireRenewRelease(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer assert" {
			http.Error(w, "unauth", 401)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id": "ses_1", "session_token": "sess", "customer_id": "alice",
		})
	})
	mux.HandleFunc("POST /v1/executions/acquire", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"execution_id": "exe_1", "status": "ACTIVE", "resource": "event:x",
		})
	})
	mux.HandleFunc("POST /v1/executions/exe_1/renew", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"execution_id": "exe_1", "status": "ACTIVE", "renew_count": 1})
	})
	mux.HandleFunc("POST /v1/executions/exe_1/release", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"execution_id": "exe_1", "state": "RELEASED"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()
	sess, err := c.CreateSession(ctx, "assert", "agent", "a1")
	if err != nil || sess.Token != "sess" {
		t.Fatalf("%+v %v", sess, err)
	}
	exe, err := c.Acquire(ctx, sess.Token, "event:x", "purchase")
	if err != nil || exe.ID != "exe_1" {
		t.Fatalf("%+v %v", exe, err)
	}
	if _, err := c.Renew(ctx, sess.Token, exe.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.Release(ctx, sess.Token, exe.ID); err != nil {
		t.Fatal(err)
	}
}
