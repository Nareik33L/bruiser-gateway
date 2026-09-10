package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
)

func cmdEAFDemo() error {
	fs := flag.NewFlagSet("eaf-demo", flag.ContinueOnError)
	front := fs.String("front", env("SIMTIX_EDGE_URL", "http://127.0.0.1:8091"), "Edge or Proxy URL")
	secret := fs.String("hmac-secret", env("BRUISER_DEV_HMAC_SECRET", ""), "HMAC for box-office cookies")
	membership := fs.String("membership", "1001234", "membership number")
	eventID := fs.String("event", "ars-che", "event id")
	n := fs.Int("n", 2000, "unaware agents (distinct logins)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	cookies := make([]string, *n)
	for i := 0; i < *n; i++ {
		tok, err := auth.IssueBoxOfficeSession(*secret, *membership, time.Hour)
		if err != nil {
			return err
		}
		cookies[i] = tok
	}
	var allow, busy, other atomic.Int64
	var wg sync.WaitGroup
	wg.Add(*n)
	client := &http.Client{Timeout: 15 * time.Second}
	start := time.Now()
	url := *front + "/api/events/" + *eventID + "/holds"
	for i := 0; i < *n; i++ {
		i := i
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(`{"seats":1}`))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: simtix.CookieName, Value: cookies[i]})
			resp, err := client.Do(req)
			if err != nil {
				other.Add(1)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusCreated, http.StatusOK:
				allow.Add(1)
			case http.StatusConflict:
				busy.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	wg.Wait()
	a, b, o := allow.Load(), busy.Load(), other.Load()
	fwd := a
	if fwd < 1 {
		fwd = 1
	}
	fmt.Printf("unaware agents=%d allow=%d busy=%d other=%d elapsed=%s\n", *n, a, b, o, time.Since(start).Truncate(time.Millisecond))
	fmt.Printf("observed_eaf=%.0f×  downstream_eaf=%.0f×  (want ~%d× and 1×)\n", float64(*n)/float64(fwd), float64(fwd), *n)
	if a != 1 || b != int64(*n-1) || o != 0 {
		return fmt.Errorf("want allow=1 busy=%d other=0", *n-1)
	}
	return nil
}
