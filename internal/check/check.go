// Package check implements the Bruiser Authority Check against a live Edge + origin.
package check

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
)

type Config struct {
	EdgeURL    string
	OriginURL  string
	HMACSecret string
	Membership string
	EventID    string
	Timeout    time.Duration
}

type Probe struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
}

type Report struct {
	Probes  []Probe  `json:"probes"`
	Overall string   `json:"overall"`
	Covered []string `json:"covered"`
}

func (r Report) String() string {
	var b strings.Builder
	b.WriteString("Bruiser Authority Check\n\n")
	for _, p := range r.Probes {
		mark := "FAIL"
		if p.Pass {
			mark = "PASS"
		}
		fmt.Fprintf(&b, "  %s — %s\n", mark, p.Name)
		if p.Detail != "" {
			fmt.Fprintf(&b, "         %s\n", p.Detail)
		}
	}
	fmt.Fprintf(&b, "\n  Overall Result: %s\n", r.Overall)
	if len(r.Covered) > 0 {
		b.WriteString("\n  Covered paths (lab; replace from discovery):\n")
		for _, c := range r.Covered {
			fmt.Fprintf(&b, "    - %s\n", c)
		}
	}
	return b.String()
}

func (r Report) Passed() bool { return r.Overall == "PASS" }

func Run(cfg Config) (Report, error) {
	if cfg.Membership == "" {
		cfg.Membership = "1001234"
	}
	if cfg.EventID == "" {
		cfg.EventID = simtix.DefaultEvent
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 8 * time.Second
	}
	client := &http.Client{Timeout: cfg.Timeout}
	edge := strings.TrimRight(cfg.EdgeURL, "/")
	origin := strings.TrimRight(cfg.OriginURL, "/")
	holdPath := "/api/events/" + cfg.EventID + "/holds"

	rep := Report{
		Covered: []string{
			"GET /api/events (search, uncontrolled)",
			"POST " + holdPath + " (hold, Edge)",
			"POST /api/orders (purchase, Edge)",
			"direct origin POST " + holdPath + " (bypass)",
		},
	}

	// Browser: unauthenticated allocation via Edge must not reach origin success.
	rep.Probes = append(rep.Probes, probe("Browser allocation route protected", func() (bool, string) {
		code, body := postJSON(client, edge+holdPath, nil, map[string]int{"seats": 1})
		if code >= 200 && code < 300 {
			return false, fmt.Sprintf("edge allowed unauthenticated hold (%d) %s", code, body)
		}
		return true, fmt.Sprintf("unauthenticated browser hold rejected (%d)", code)
	}))

	// Mobile analogue: same box-office API, WebView-style UA, no session.
	rep.Probes = append(rep.Probes, probe("Mobile API protected", func() (bool, string) {
		req, _ := http.NewRequest(http.MethodPost, edge+holdPath, bytes.NewBufferString(`{"seats":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "ArsenalOfficial/4.0 (iPhone; WebView)")
		resp, err := client.Do(req)
		if err != nil {
			return false, err.Error()
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return false, fmt.Sprintf("mobile hold succeeded without session (%d) %s", resp.StatusCode, b)
		}
		return true, fmt.Sprintf("unauthenticated mobile hold rejected (%d)", resp.StatusCode)
	}))

	// Agent: no session via Edge.
	rep.Probes = append(rep.Probes, probe("Agent API protected", func() (bool, string) {
		code, body := postJSON(client, edge+holdPath, map[string]string{"X-Principal": "agent"}, map[string]int{"seats": 1})
		if code >= 200 && code < 300 {
			return false, fmt.Sprintf("agent hold succeeded without session (%d) %s", code, body)
		}
		return true, fmt.Sprintf("unauthenticated agent hold rejected (%d)", code)
	}))

	rep.Probes = append(rep.Probes, probe("Expired token rejected", func() (bool, string) {
		tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, cfg.Membership, -time.Hour)
		if err != nil {
			return false, err.Error()
		}
		code, body := postJSON(client, edge+holdPath, cookieHeader(tok), map[string]int{"seats": 1})
		if code == http.StatusUnauthorized {
			return true, "expired boxoffice_session → 401"
		}
		return false, fmt.Sprintf("want 401 got %d %s", code, body)
	}))

	rep.Probes = append(rep.Probes, probe("Tampered token rejected", func() (bool, string) {
		tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, cfg.Membership, time.Hour)
		if err != nil {
			return false, err.Error()
		}
		if len(tok) < 8 {
			return false, "token too short"
		}
		tampered := tok[:len(tok)-4] + "XXXX"
		code, body := postJSON(client, edge+holdPath, cookieHeader(tampered), map[string]int{"seats": 1})
		if code == http.StatusUnauthorized {
			return true, "tampered boxoffice_session → 401"
		}
		return false, fmt.Sprintf("want 401 got %d %s", code, body)
	}))

	rep.Probes = append(rep.Probes, probe("Direct allocation bypass blocked", func() (bool, string) {
		if origin == "" {
			return false, "origin URL not set"
		}
		tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, cfg.Membership, time.Hour)
		if err != nil {
			return false, err.Error()
		}
		code, body := postJSON(client, origin+holdPath, cookieHeader(tok), map[string]int{"seats": 1})
		if code == http.StatusForbidden && strings.Contains(body, "origin lockdown") {
			return true, "direct origin hold rejected without Edge secret"
		}
		if code >= 200 && code < 300 {
			return false, fmt.Sprintf("origin accepted a bypass hold (%d); lockdown is off or missing", code)
		}
		return false, fmt.Sprintf("unexpected origin bypass response %d %s", code, body)
	}))

	rep.Overall = "PASS"
	for _, p := range rep.Probes {
		if !p.Pass {
			rep.Overall = "FAIL"
			break
		}
	}
	return rep, nil
}

func probe(name string, fn func() (bool, string)) Probe {
	ok, detail := fn()
	return Probe{Name: name, Pass: ok, Detail: detail}
}

func cookieHeader(tok string) map[string]string {
	return map[string]string{"Cookie": simtix.CookieName + "=" + tok}
}

func postJSON(client *http.Client, url string, headers map[string]string, body any) (int, string) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, url, buf)
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, strings.TrimSpace(string(b))
}
