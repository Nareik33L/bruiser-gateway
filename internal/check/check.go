// Package check implements the Bruiser Authority Check against a live Edge + origin.
package check

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
)

type Config struct {
	EdgeURL    string
	OriginURL  string
	ControlURL string
	HMACSecret string
	EdgeSecret string
	Membership string
	EventID    string
	Timeout    time.Duration
}

type Probe struct {
	Name     string `json:"name"`
	Pass     bool   `json:"pass"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
	Blocking bool   `json:"blocking,omitempty"`
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
		mark := p.Status
		if mark == "" {
			if p.Pass {
				mark = "PASS"
			} else {
				mark = "FAIL"
			}
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
	if cfg.EdgeSecret == "" {
		cfg.EdgeSecret = os.Getenv("BRUISER_EDGE_SECRET")
	}
	if cfg.EdgeSecret == "" {
		cfg.EdgeSecret = "edge-secret-dev"
	}
	client := &http.Client{Timeout: cfg.Timeout}
	edge := strings.TrimRight(cfg.EdgeURL, "/")
	origin := strings.TrimRight(cfg.OriginURL, "/")
	control := strings.TrimRight(cfg.ControlURL, "/")
	holdPath := "/api/events/" + cfg.EventID + "/holds"

	rep := Report{
		Covered: []string{
			"GET /api/events (search, uncontrolled)",
			"POST " + holdPath + " (hold, enforcement front)",
			"POST /api/orders (purchase, enforcement front)",
			"direct origin POST " + holdPath + " (bypass)",
			"spoofed identity headers",
			"unlisted / alternate allocation paths",
			"stale and tampered credentials",
		},
	}

	rep.Probes = append(rep.Probes, probe("Browser allocation route protected", func() Probe {
		code, body := postJSON(client, edge+holdPath, nil, map[string]int{"seats": 1})
		if code >= 200 && code < 300 {
			return fail("front allowed unauthenticated hold (%d) %s", code, body)
		}
		return pass("unauthenticated browser hold rejected (%d)", code)
	}))

	rep.Probes = append(rep.Probes, probe("Mobile API protected", func() Probe {
		req, _ := http.NewRequest(http.MethodPost, edge+holdPath, bytes.NewBufferString(`{"seats":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "ArsenalOfficial/4.0 (iPhone; WebView)")
		resp, err := client.Do(req)
		if err != nil {
			return fail("%s", err.Error())
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return fail("mobile hold succeeded without session (%d) %s", resp.StatusCode, b)
		}
		return pass("unauthenticated mobile hold rejected (%d)", resp.StatusCode)
	}))

	rep.Probes = append(rep.Probes, probe("Agent API protected", func() Probe {
		code, body := postJSON(client, edge+holdPath, map[string]string{"X-Principal": "agent"}, map[string]int{"seats": 1})
		if code >= 200 && code < 300 {
			return fail("agent hold succeeded without session (%d) %s", code, body)
		}
		return pass("unauthenticated agent hold rejected (%d)", code)
	}))

	rep.Probes = append(rep.Probes, probe("Spoofed identity headers rejected", func() Probe {
		code, body := postJSON(client, edge+holdPath, map[string]string{
			"X-Customer-Id":      cfg.Membership,
			"X-Bruiser-Customer": cfg.Membership,
			"X-Subject":          cfg.Membership,
		}, map[string]int{"seats": 1})
		if code >= 200 && code < 300 {
			return fail("spoofed identity allocated (%d) %s", code, body)
		}
		return pass("spoofed customer headers did not allocate (%d)", code)
	}))

	rep.Probes = append(rep.Probes, probe("Expired token rejected", func() Probe {
		tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, cfg.Membership, -time.Hour)
		if err != nil {
			return fail("%s", err.Error())
		}
		code, body := postJSON(client, edge+holdPath, cookieHeader(tok), map[string]int{"seats": 1})
		if code == http.StatusUnauthorized {
			return pass("expired boxoffice_session → 401")
		}
		return fail("want 401 got %d %s", code, body)
	}))

	rep.Probes = append(rep.Probes, probe("Tampered token rejected", func() Probe {
		tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, cfg.Membership, time.Hour)
		if err != nil {
			return fail("%s", err.Error())
		}
		if len(tok) < 8 {
			return fail("token too short")
		}
		tampered := tok[:len(tok)-4] + "XXXX"
		code, body := postJSON(client, edge+holdPath, cookieHeader(tampered), map[string]int{"seats": 1})
		if code == http.StatusUnauthorized {
			return pass("tampered boxoffice_session → 401")
		}
		return fail("want 401 got %d %s", code, body)
	}))

	rep.Probes = append(rep.Probes, probe("GET on allocation route does not grant", func() Probe {
		req, _ := http.NewRequest(http.MethodGet, edge+holdPath, nil)
		resp, err := client.Do(req)
		if err != nil {
			return fail("%s", err.Error())
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return fail("GET hold succeeded (%d)", resp.StatusCode)
		}
		return pass("GET hold not allocated (%d)", resp.StatusCode)
	}))

	rep.Probes = append(rep.Probes, probe("Stale execution rejected", func() Probe {
		code, body := postJSON(client, edge+holdPath, map[string]string{
			"X-Bruiser-Execution": "stale.not.a.valid.execution",
		}, map[string]int{"seats": 1})
		if code >= 200 && code < 300 {
			return fail("front allocated with a stale execution token (%d) %s", code, body)
		}
		return pass("stale execution not allocated (%d)", code)
	}))

	if origin == "" {
		rep.Probes = append(rep.Probes, failProbe("Origin lockdown proven", "FAIL — --origin is required. Do not go live without proving the locked origin."))
	} else {
		rep.Probes = append(rep.Probes, probe("Removed proxy headers do not allocate", func() Probe {
			tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, cfg.Membership, time.Hour)
			if err != nil {
				return fail("%s", err.Error())
			}
			hdr := cookieHeader(tok)
			hdr["X-Bruiser-Execution"] = "not-a-token"
			code, body := postJSON(client, origin+holdPath, hdr, map[string]int{"seats": 1})
			if code >= 200 && code < 300 {
				return fail("origin allocated without Bruiser proxy headers (%d) %s", code, body)
			}
			return pass("origin rejected allocation without proxy headers (%d)", code)
		}))

		rep.Probes = append(rep.Probes, probe("Spoofed origin secret rejected", func() Probe {
			code, body := postJSON(client, origin+holdPath, map[string]string{
				"X-Bruiser-Origin-Secret": "wrong-origin-secret",
			}, map[string]int{"seats": 1})
			if code >= 200 && code < 300 {
				return fail("origin accepted a spoofed origin secret (%d) %s", code, body)
			}
			return pass("spoofed origin secret rejected (%d)", code)
		}))

		rep.Probes = append(rep.Probes, probe("Direct allocation bypass blocked", func() Probe {
			tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, cfg.Membership, time.Hour)
			if err != nil {
				return fail("%s", err.Error())
			}
			code, body := postJSON(client, origin+holdPath, cookieHeader(tok), map[string]int{"seats": 1})
			if code == http.StatusForbidden && strings.Contains(body, "origin lockdown") {
				return pass("direct origin hold rejected without Edge secret")
			}
			if code >= 200 && code < 300 {
				return fail("origin accepted a bypass hold (%d); lockdown is off or missing", code)
			}
			return fail("unexpected origin bypass response %d %s", code, body)
		}))

		rep.Probes = append(rep.Probes, probe("Origin rejects missing Bruiser credentials", func() Probe {
			code, body := postJSON(client, origin+holdPath, nil, map[string]int{"seats": 1})
			if code >= 200 && code < 300 {
				return fail("origin allocated with no Bruiser credentials (%d) %s", code, body)
			}
			return pass("origin rejected bare allocation (%d)", code)
		}))

		rep.Probes = append(rep.Probes, probe("Origin rejects spoofed identity", func() Probe {
			code, body := postJSON(client, origin+holdPath, map[string]string{
				"X-Customer-Id":      cfg.Membership,
				"X-Bruiser-Customer": cfg.Membership,
			}, map[string]int{"seats": 1})
			if code >= 200 && code < 300 {
				return fail("origin allocated from spoofed identity (%d) %s", code, body)
			}
			return pass("origin rejected spoofed identity (%d)", code)
		}))

		rep.Probes = append(rep.Probes, probe("Origin rejects stale execution", func() Probe {
			code, body := postJSON(client, origin+holdPath, map[string]string{
				"X-Bruiser-Execution": "stale.not.a.valid.execution",
			}, map[string]int{"seats": 1})
			if code >= 200 && code < 300 {
				return fail("origin allocated from stale execution (%d) %s", code, body)
			}
			return pass("origin rejected stale execution (%d)", code)
		}))

		rep.Probes = append(rep.Probes, probe("Origin rejects tampered session", func() Probe {
			tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, cfg.Membership, time.Hour)
			if err != nil {
				return fail("%s", err.Error())
			}
			if len(tok) < 8 {
				return fail("token too short")
			}
			code, body := postJSON(client, origin+holdPath, cookieHeader(tok[:len(tok)-4]+"XXXX"), map[string]int{"seats": 1})
			if code >= 200 && code < 300 {
				return fail("origin allocated from tampered session (%d) %s", code, body)
			}
			return pass("origin rejected tampered session (%d)", code)
		}))
	}

	if control == "" {
		rep.Probes = append(rep.Probes, failProbe("Authorize requires edge secret", "FAIL — --control is required to prove /v1/authorize rejects a missing edge secret."))
	} else {
		rep.Probes = append(rep.Probes, probe("Authorize requires edge secret", func() Probe {
			code, body := postJSON(client, control+"/v1/authorize", map[string]string{
				"X-Original-Method": "POST",
				"X-Original-URI":    holdPath,
			}, map[string]int{"seats": 1})
			if code >= 200 && code < 300 {
				return fail("authorize succeeded without edge secret (%d) %s", code, body)
			}
			if code == http.StatusUnauthorized {
				return pass("authorize without edge secret → 401")
			}
			return pass("authorize without edge secret rejected (%d)", code)
		}))
	}

	alternates := []string{
		"/api/events/" + cfg.EventID + "/hold",
		"/api/events/" + cfg.EventID + "/holds/",
		strings.ToUpper(holdPath),
	}
	alternates = append(alternates, merchant.CommonAllocationPaths...)
	rep.Probes = append(rep.Probes, probe("Unlisted allocation paths cannot grant", func() Probe {
		tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, cfg.Membership, time.Hour)
		if err != nil {
			return fail("%s", err.Error())
		}
		hdr := cookieHeader(tok)
		allocated := []string{}
		seen := 0
		for _, p := range alternates {
			code, body := postJSON(client, edge+p, hdr, map[string]int{"seats": 1})
			seen++
			if code >= 200 && code < 300 && !looksLikeSearch(body) {
				allocated = append(allocated, fmt.Sprintf("%s → %d", p, code))
			}
		}
		if origin != "" {
			for _, p := range alternates {
				code, body := postJSON(client, origin+p, hdr, map[string]int{"seats": 1})
				seen++
				if code >= 200 && code < 300 && !looksLikeSearch(body) {
					allocated = append(allocated, fmt.Sprintf("origin %s → %d", p, code))
				}
			}
		}
		if len(allocated) > 0 {
			return fail("unlisted path allocated: %s", strings.Join(allocated, "; "))
		}
		return pass("%d alternate host/path combinations did not allocate", seen)
	}))

	rep.Probes = append(rep.Probes, probe("Host/path smuggling does not allocate", func() Probe {
		tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, cfg.Membership, time.Hour)
		if err != nil {
			return fail("%s", err.Error())
		}
		// Hit an uncontrolled path while claiming the allocation URI in hop-by-hop
		// headers. A 2xx hold body here would mean Bruiser honoured a spoofed route.
		req, _ := http.NewRequest(http.MethodPost, edge+"/api/events", bytes.NewBufferString(`{"seats":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", simtix.CookieName+"="+tok)
		req.Host = "origin.internal"
		req.Header.Set("X-Forwarded-Host", "origin.internal")
		req.Header.Set("X-Forwarded-Uri", holdPath)
		req.Header.Set("X-Original-URL", "http://origin.internal"+holdPath)
		req.Header.Set("X-Original-URI", holdPath)
		req.Header.Set("X-Original-Method", "POST")
		resp, err := client.Do(req)
		if err != nil {
			return fail("%s", err.Error())
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		body := string(b)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.Contains(body, `"hold_id"`) {
			return fail("spoofed hop-by-hop headers allocated (%d) %s", resp.StatusCode, body)
		}
		return pass("spoofed host/path headers did not allocate (%d)", resp.StatusCode)
	}))

	rep.Probes = append(rep.Probes, probe("Resource variants share one domain", func() Probe {
		if control == "" {
			return fail("--control is required to prove canonical resource domains")
		}
		tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, fmt.Sprintf("canon-%d", time.Now().UnixNano()), time.Hour)
		if err != nil {
			return fail("%s", err.Error())
		}
		hdr := map[string]string{
			"Cookie":                simtix.CookieName + "=" + tok,
			"X-Bruiser-Edge-Secret": cfg.EdgeSecret,
		}
		code1, body1 := postJSON(client, control+"/v1/authorize", hdr, map[string]string{
			"method": "POST", "path": "/api/events/" + cfg.EventID + "/holds",
		})
		if code1 != http.StatusOK {
			return fail("canonical authorize failed (%d) %s", code1, body1)
		}
		var a1 map[string]any
		_ = json.Unmarshal([]byte(body1), &a1)
		code2, body2 := postJSON(client, control+"/v1/authorize", hdr, map[string]string{
			"method": "POST", "path": "/api/events/" + strings.ToUpper(cfg.EventID) + "/holds/",
		})
		if code2 == http.StatusConflict {
			return fail("path variant created a second domain (%d) %s", code2, body2)
		}
		if code2 != http.StatusOK {
			return fail("variant authorize %d %s", code2, body2)
		}
		var a2 map[string]any
		_ = json.Unmarshal([]byte(body2), &a2)
		if id1, _ := a1["execution_id"].(string); id1 != "" {
			if id2, _ := a2["execution_id"].(string); id2 != "" && id1 != id2 {
				return fail("variants minted two executions %s vs %s", id1, id2)
			}
		}
		return pass("case/slash variants stayed on one execution")
	}))

	rep.Probes = append(rep.Probes, probe("Forged execution rejected at origin", func() Probe {
		if origin == "" {
			return fail("--origin is required")
		}
		code, body := postJSON(client, origin+holdPath, map[string]string{
			"X-Bruiser-Execution": "forged.not.signed",
			"X-Bruiser-Fence":     "1",
		}, map[string]int{"seats": 1})
		if code >= 200 && code < 300 {
			return fail("origin accepted forged execution (%d) %s", code, body)
		}
		return pass("forged execution rejected (%d)", code)
	}))

	rep.Probes = append(rep.Probes, probe("Modified fence rejected", func() Probe {
		if origin == "" {
			return fail("--origin is required")
		}
		code, body := postJSON(client, origin+holdPath, map[string]string{
			"X-Bruiser-Execution": "stale.not.a.valid.execution",
			"X-Bruiser-Fence":     "999999",
		}, map[string]int{"seats": 1})
		if code >= 200 && code < 300 {
			return fail("origin accepted modified fence (%d) %s", code, body)
		}
		return pass("modified fence rejected (%d)", code)
	}))

	rep.Probes = append(rep.Probes, probe("Unsigned identity rejected outside trusted edge", func() Probe {
		code, body := postJSON(client, edge+holdPath, map[string]string{
			"X-Customer-Id": "spoofed-customer",
		}, map[string]int{"seats": 1})
		if code >= 200 && code < 300 {
			return fail("unsigned identity allocated (%d) %s", code, body)
		}
		return pass("unsigned identity rejected (%d)", code)
	}))

	rep.Probes = append(rep.Probes, probe("Legitimate Bruiser-mediated allocation succeeds", func() Probe {
		if edge == "" {
			return fail("front URL not set")
		}
		tok, err := auth.IssueBoxOfficeSession(cfg.HMACSecret, fmt.Sprintf("auth-ok-%d", time.Now().UnixNano()), time.Hour)
		if err != nil {
			return fail("%s", err.Error())
		}
		code, body := postJSON(client, edge+holdPath, cookieHeader(tok), map[string]int{"seats": 1})
		if code >= 200 && code < 300 {
			return pass("front allocated through Bruiser (%d)", code)
		}
		return fail("legitimate hold via Bruiser failed (%d) %s", code, body)
	}))

	finalize(&rep)
	return rep, nil
}

func looksLikeSearch(body string) bool {
	return strings.Contains(body, `"events"`) || strings.Contains(body, `"available"`) && !strings.Contains(body, `"hold_id"`)
}

func probe(name string, fn func() Probe) Probe {
	p := fn()
	p.Name = name
	normalizeProbe(&p)
	return p
}

func normalizeProbe(p *Probe) {
	if p.Status == "" {
		if p.Pass {
			p.Status = "PASS"
		} else {
			p.Status = "FAIL"
		}
	}
	p.Pass = p.Status != "FAIL"
}

func pass(format string, args ...any) Probe {
	return Probe{Status: "PASS", Pass: true, Detail: fmt.Sprintf(format, args...)}
}

func fail(format string, args ...any) Probe {
	return Probe{Status: "FAIL", Pass: false, Detail: fmt.Sprintf(format, args...)}
}

func failProbe(name, detail string) Probe {
	p := Probe{Name: name, Status: "FAIL", Pass: false, Detail: detail}
	normalizeProbe(&p)
	return p
}

func finalize(rep *Report) {
	failN, blockWarn := 0, 0
	for i := range rep.Probes {
		normalizeProbe(&rep.Probes[i])
		switch rep.Probes[i].Status {
		case "FAIL":
			failN++
		case "WARN":
			if rep.Probes[i].Blocking {
				blockWarn++
			}
		}
	}
	switch {
	case failN > 0:
		rep.Overall = "FAIL"
	case blockWarn > 0:
		rep.Overall = "WARN"
	default:
		rep.Overall = "PASS"
	}
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
