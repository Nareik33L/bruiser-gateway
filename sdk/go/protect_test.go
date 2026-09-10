package bruiser

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

func TestProtectAndStaleFence(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := auth.Signer{KID: "k", MerchantID: "arsenal", Private: priv, Public: pub}
	okTok, err := s.SignExecution(lease.Execution{
		ID: "exe_1", MerchantID: "arsenal", CustomerID: "1001234",
		DomainKey: "dom", Resource: "event:ars-che", Action: "hold",
		Principal: lease.Principal{Type: "agent", ID: "a1"},
		Fence:     2, ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	staleTok, err := s.SignExecution(lease.Execution{
		ID: "exe_0", MerchantID: "arsenal", CustomerID: "1001234",
		DomainKey: "dom", Resource: "event:ars-che", Action: "hold",
		Principal: lease.Principal{Type: "agent", ID: "a0"},
		Fence:     1, ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	h := Protect(ProtectConfig{Public: pub, Fences: &FenceCache{}})(inner)

	req := httptest.NewRequest(http.MethodPost, "/holds", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/holds", nil)
	req.Header.Set("X-Bruiser-Execution", okTok)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/holds", nil)
	req.Header.Set("X-Bruiser-Execution", staleTok)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stale %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/holds", nil)
	req.Header.Set("X-Bruiser-Execution", "forged.not.signed")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("garbage %d", rec.Code)
	}

	h = Protect(ProtectConfig{Public: pub, Fences: &FenceCache{}, Resource: "event:ars-che."})(inner)
	req = httptest.NewRequest(http.MethodPost, "/holds", nil)
	req.Header.Set("X-Bruiser-Execution", okTok)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("canonical resource %d", rec.Code)
	}
}

func TestProtectPartitionAllowsHeldToken(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := auth.Signer{KID: "k", MerchantID: "arsenal", Private: priv, Public: pub}
	tok, err := s.SignExecution(lease.Execution{
		ID: "exe_1", MerchantID: "arsenal", CustomerID: "1001234",
		DomainKey: "dom", Resource: "event:ars-che", Action: "hold",
		Principal: lease.Principal{Type: "agent", ID: "a1"},
		Fence:     1, ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	h := Protect(ProtectConfig{
		Public: pub,
		Active: func(string, int64) (bool, error) { return false, errors.New("partition") },
	})(inner)
	req := httptest.NewRequest(http.MethodPost, "/holds", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/holds", nil)
	req.Header.Set("X-Bruiser-Execution", tok)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("held token on partition %d", rec.Code)
	}
	denied := Protect(ProtectConfig{
		Public: pub,
		Active: func(string, int64) (bool, error) { return false, nil },
	})(inner)
	req = httptest.NewRequest(http.MethodPost, "/holds", nil)
	req.Header.Set("X-Bruiser-Execution", tok)
	rec = httptest.NewRecorder()
	denied.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("known revoke %d", rec.Code)
	}
}
