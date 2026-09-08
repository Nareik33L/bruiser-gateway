// Package swarm drives demo and EAF load profiles against an Edge or Proxy front.
package swarm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
)

type Profile string

const (
	Profile1xN      Profile = "1xN"
	ProfileNxK      Profile = "NxK"
	ProfileHandoff  Profile = "handoff"
	ProfileBypass   Profile = "bypass"
	ProfileUnaware  Profile = "unaware"
)

type Config struct {
	Front      string
	Origin     string
	HMACSecret string
	EventID    string
	N          int // agents for 1xN / unaware
	Customers  int // for NxK
	PerCust    int // for NxK
	Bypass     bool
}

type Result struct {
	Profile      string `json:"profile"`
	Allow        int64  `json:"allow"`
	Busy         int64  `json:"busy"`
	Other        int64  `json:"other"`
	ObservedEAF  float64 `json:"observed_eaf"`
	DownstreamEAF float64 `json:"downstream_eaf"`
	ElapsedMS    int64  `json:"elapsed_ms"`
	Detail       string `json:"detail,omitempty"`
	OK           bool   `json:"ok"`
}

func Run(cfg Config, profile Profile) (Result, error) {
	if cfg.EventID == "" {
		cfg.EventID = simtix.DefaultEvent
	}
	if cfg.HMACSecret == "" {
		cfg.HMACSecret = "dev-secret-change-me"
	}
	if cfg.N < 1 {
		cfg.N = 50
	}
	switch profile {
	case ProfileNxK:
		return runNxK(cfg)
	case ProfileHandoff:
		return runHandoff(cfg)
	case ProfileBypass:
		return runBypass(cfg)
	case Profile1xN, ProfileUnaware, "":
		return run1xN(cfg)
	default:
		return Result{}, fmt.Errorf("unknown profile %s", profile)
	}
}

func run1xN(cfg Config) (Result, error) {
	target := cfg.Front
	if cfg.Bypass && cfg.Origin != "" {
		target = cfg.Origin
	}
	var allow, busy, other atomic.Int64
	var wg sync.WaitGroup
	wg.Add(cfg.N)
	client := &http.Client{Timeout: 20 * time.Second}
	start := time.Now()
	url := target + "/api/events/" + cfg.EventID + "/holds"
	for i := 0; i < cfg.N; i++ {
		go func() {
			defer wg.Done()
			tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, "1001234", time.Hour)
			if err != nil {
				other.Add(1)
				return
			}
			code := postHold(client, url, tok)
			switch code {
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
	ok := a == 1 && b == int64(cfg.N-1) && o == 0
	if cfg.Bypass {
		ok = a == int64(cfg.N) || (a > 1 && b == 0)
	}
	return Result{
		Profile:       string(Profile1xN),
		Allow:         a,
		Busy:          b,
		Other:         o,
		ObservedEAF:   float64(cfg.N) / float64(fwd),
		DownstreamEAF: float64(fwd),
		ElapsedMS:     time.Since(start).Milliseconds(),
		OK:            ok,
	}, nil
}

func runNxK(cfg Config) (Result, error) {
	if cfg.Customers < 1 {
		cfg.Customers = 10
	}
	if cfg.PerCust < 1 {
		cfg.PerCust = 2
	}
	total := cfg.Customers * cfg.PerCust
	var allow, busy, other atomic.Int64
	var wg sync.WaitGroup
	wg.Add(total)
	client := &http.Client{Timeout: 20 * time.Second}
	start := time.Now()
	url := cfg.Front + "/api/events/" + cfg.EventID + "/holds"
	for c := 0; c < cfg.Customers; c++ {
		cust := fmt.Sprintf("%07d", 1000000+c)
		for k := 0; k < cfg.PerCust; k++ {
			go func(membership string) {
				defer wg.Done()
				tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, membership, time.Hour)
				if err != nil {
					other.Add(1)
					return
				}
				switch postHold(client, url, tok) {
				case http.StatusCreated, http.StatusOK:
					allow.Add(1)
				case http.StatusConflict:
					busy.Add(1)
				default:
					other.Add(1)
				}
			}(cust)
		}
	}
	wg.Wait()
	a, b, o := allow.Load(), busy.Load(), other.Load()
	ok := a == int64(cfg.Customers) && o == 0
	return Result{
		Profile:       string(ProfileNxK),
		Allow:         a,
		Busy:          b,
		Other:         o,
		ObservedEAF:   float64(total) / float64(max64(a, 1)),
		DownstreamEAF: float64(a) / float64(cfg.Customers),
		ElapsedMS:     time.Since(start).Milliseconds(),
		OK:            ok,
	}, nil
}

func runHandoff(cfg Config) (Result, error) {
	// Handoff is exercised against the control plane; this profile documents
	// the scripted agent→browser take-control path for the demo CLI.
	return Result{
		Profile: string(ProfileHandoff),
		OK:      true,
		Detail:  "run make demo-handoff / CI TestTakeControlHandoffScenario",
	}, nil
}

func runBypass(cfg Config) (Result, error) {
	cfg.Bypass = true
	if cfg.Origin == "" {
		return Result{}, fmt.Errorf("origin URL required for bypass profile")
	}
	res, err := run1xN(cfg)
	res.Profile = string(ProfileBypass)
	return res, err
}

func postHold(client *http.Client, url, cookie string) int {
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(`{"seats":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: simtix.CookieName, Value: cookie})
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (r Result) JSON() []byte {
	b, _ := json.MarshalIndent(r, "", "  ")
	return b
}
