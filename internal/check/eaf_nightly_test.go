package check_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
)

// TestEAFNightly is the 1×10,000 exit criterion. Regular CI keeps the 200-agent
// swarm; set BRUISER_EAF_N=10000 (make eaf-nightly) to run the full size.
func TestEAFNightly(t *testing.T) {
	raw := os.Getenv("BRUISER_EAF_N")
	if raw == "" {
		t.Skip("BRUISER_EAF_N not set")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 2 {
		t.Fatalf("BRUISER_EAF_N=%q", raw)
	}
	front, origin, _, hmac := startProxyLab(t, "origin-lock-dev")
	var allow, busy, other atomic.Int64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			tok, err := auth.IssueBoxOfficeSession(hmac, "1001234", 0)
			if err != nil {
				other.Add(1)
				return
			}
			req, _ := http.NewRequest(http.MethodPost, front+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: simtix.CookieName, Value: tok})
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				other.Add(1)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusCreated:
				allow.Add(1)
			case http.StatusConflict:
				busy.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	wg.Wait()
	if allow.Load() != 1 || busy.Load() != int64(n-1) || other.Load() != 0 {
		t.Fatalf("allow=%d busy=%d other=%d want 1/%d/0", allow.Load(), busy.Load(), other.Load(), n-1)
	}
	resp, err := http.Get(origin + "/api/events/ars-che")
	if err != nil {
		t.Fatal(err)
	}
	var ev struct {
		Held      int `json:"held"`
		Available int `json:"available"`
		Seats     int `json:"seats"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if ev.Held != 1 {
		t.Fatalf("10k-agent nightly: origin held=%d want 1", ev.Held)
	}
}
