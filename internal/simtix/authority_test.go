package simtix

import (
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

func testSigner(t *testing.T, merchant string) auth.Signer {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return auth.Signer{KID: "k1", MerchantID: merchant, Private: priv, Public: pub}
}

func signExe(t *testing.T, s auth.Signer, resource string, fence int64, ttl time.Duration) string {
	t.Helper()
	tok, err := s.SignExecution(lease.Execution{
		ID:         "exe_test",
		MerchantID: s.MerchantID,
		DomainKey:  resource + "/" + strconv.FormatInt(fence, 10) + "/" + strconv.FormatInt(ttl.Nanoseconds(), 10),
		CustomerID: "alice",
		Principal:  lease.Principal{Type: "agent", ID: "a1"},
		Resource:   resource,
		Action:     "hold",
		Fence:      fence,
		ExpiresAt:  time.Now().UTC().Add(ttl),
	})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func postHold(t *testing.T, url string, hdr map[string]string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url+"/api/events/ars-che/holds", strings.NewReader(`{"seats":1}`))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestOriginSecretIsPathTrustNotAuthority(t *testing.T) {
	signer := testSigner(t, "arsenal")
	s := New(Lab("dev-secret-change-me", "lock", "arsenal", signer.Public, "", 4))
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	if code := postHold(t, srv.URL, nil); code == http.StatusCreated {
		t.Fatalf("bare hold allocated %d", code)
	}
	if code := postHold(t, srv.URL, map[string]string{"X-Bruiser-Origin-Secret": "lock"}); code != http.StatusUnauthorized {
		t.Fatalf("secret alone must not allocate %d", code)
	}
	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     "forged.not.signed",
		"X-Bruiser-Fence":         "1",
	}); code < 400 {
		t.Fatalf("forged token + valid secret allocated %d", code)
	}
}

func TestOriginRejectsModifiedFenceWrongMerchantResourceExpired(t *testing.T) {
	signer := testSigner(t, "arsenal")
	s := New(Lab("dev-secret-change-me", "lock", "arsenal", signer.Public, "", 4))
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	ok := signExe(t, signer, "event:ars-che", 3, time.Hour)
	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     ok,
		"X-Bruiser-Fence":         "3",
	}); code != http.StatusCreated {
		t.Fatalf("valid authority %d", code)
	}

	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     ok,
		"X-Bruiser-Fence":         "999999",
	}); code != http.StatusForbidden {
		t.Fatalf("modified fence %d", code)
	}

	other := testSigner(t, "other-club")
	wrongMid := signExe(t, other, "event:ars-che", 1, time.Hour)
	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     wrongMid,
		"X-Bruiser-Fence":         "1",
	}); code < 400 {
		t.Fatalf("wrong merchant allocated %d", code)
	}

	wrongRes := signExe(t, signer, "event:other", 1, time.Hour)
	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     wrongRes,
		"X-Bruiser-Fence":         "1",
	}); code != http.StatusForbidden {
		t.Fatalf("wrong resource %d", code)
	}

	expired := signExe(t, signer, "event:ars-che", 1, -time.Hour)
	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     expired,
		"X-Bruiser-Fence":         "1",
	}); code < 400 {
		t.Fatalf("expired execution allocated %d", code)
	}
}

func TestOriginExecutionVerificationMatrix(t *testing.T) {
	signer := testSigner(t, "arsenal")
	s := New(Lab("dev-secret-change-me", "lock", "arsenal", signer.Public, "", 4))
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
	}); code != http.StatusUnauthorized && code != http.StatusForbidden {
		t.Fatalf("absent token %d", code)
	}
	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     "forged.not.signed",
		"X-Bruiser-Fence":         "1",
	}); code != http.StatusForbidden {
		t.Fatalf("garbage token want 403 got %d", code)
	}

	tok := signExe(t, signer, "event:ars-che", 7, time.Hour)
	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     tok,
		"X-Bruiser-Fence":         "6",
	}); code != http.StatusForbidden {
		t.Fatalf("stale fence header %d", code)
	}
	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     tok,
		"X-Bruiser-Fence":         "7",
	}); code != http.StatusCreated {
		t.Fatalf("valid current token %d", code)
	}

	lower := signExeDomain(t, signer, "event:ars-che", "same-domain", 3, time.Hour)
	higher := signExeDomain(t, signer, "event:ars-che", "same-domain", 9, time.Hour)
	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     higher,
		"X-Bruiser-Fence":         "9",
	}); code != http.StatusCreated {
		t.Fatalf("higher fence %d", code)
	}
	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     lower,
		"X-Bruiser-Fence":         "3",
	}); code != http.StatusForbidden {
		t.Fatalf("last-seen fence %d", code)
	}
}

func signExeDomain(t *testing.T, s auth.Signer, resource, domain string, fence int64, ttl time.Duration) string {
	t.Helper()
	tok, err := s.SignExecution(lease.Execution{
		ID:         "exe_" + strconv.FormatInt(fence, 10),
		MerchantID: s.MerchantID,
		DomainKey:  domain,
		CustomerID: "alice",
		Principal:  lease.Principal{Type: "agent", ID: "a1"},
		Resource:   resource,
		Action:     "hold",
		Fence:      fence,
		ExpiresAt:  time.Now().UTC().Add(ttl),
	})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestAllowOriginSecretOnlyLabOnly(t *testing.T) {
	lab := New(Config{
		HMACSecret:            "dev-secret-change-me",
		OriginSecret:          "lock",
		Seats:                 4,
		AllowOriginSecretOnly: true,
		Environment:           "lab",
	})
	labSrv := httptest.NewServer(lab.Handler())
	t.Cleanup(labSrv.Close)
	if code := postHold(t, labSrv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     "garbage",
	}); code != http.StatusCreated {
		t.Fatalf("lab escape hatch %d", code)
	}

	prod := New(Config{
		HMACSecret:            "dev-secret-change-me",
		OriginSecret:          "lock",
		Seats:                 4,
		AllowOriginSecretOnly: true,
		Environment:           "production",
	})
	prodSrv := httptest.NewServer(prod.Handler())
	t.Cleanup(prodSrv.Close)
	if code := postHold(t, prodSrv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     "garbage",
	}); code != http.StatusForbidden {
		t.Fatalf("production must refuse secret-only hatch %d", code)
	}
}

func TestOriginIntrospectRejectsRevoked(t *testing.T) {
	signer := testSigner(t, "arsenal")
	intro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":false,"status":"DENIED","reason":"REVOKED","state":"REVOKED"}`))
	}))
	t.Cleanup(intro.Close)
	s := New(Lab("dev-secret-change-me", "lock", "arsenal", signer.Public, intro.URL, 4))
	s.cfg.IntrospectURL = intro.URL
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	tok := signExe(t, signer, "event:ars-che", 1, time.Hour)
	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     tok,
		"X-Bruiser-Fence":         "1",
	}); code != http.StatusForbidden {
		t.Fatalf("revoked %d", code)
	}
}

func TestOriginPartitionAllowsHeldToken(t *testing.T) {
	signer := testSigner(t, "arsenal")
	intro := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	intro.Close()
	s := New(Lab("dev-secret-change-me", "lock", "arsenal", signer.Public, intro.URL, 4))
	s.cfg.IntrospectURL = intro.URL
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	tok := signExe(t, signer, "event:ars-che", 1, time.Hour)
	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
		"X-Bruiser-Execution":     tok,
		"X-Bruiser-Fence":         "1",
	}); code != http.StatusCreated {
		t.Fatalf("held token while gateway partitioned want allow, got %d", code)
	}
	if code := postHold(t, srv.URL, map[string]string{
		"X-Bruiser-Origin-Secret": "lock",
	}); code != http.StatusUnauthorized {
		t.Fatalf("no token on partition must deny, got %d", code)
	}
}
