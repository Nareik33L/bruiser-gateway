package check

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Evidence is the request/response transcript a PASS must rest on.
// Secrets are marked present rather than echoed.
type Evidence struct {
	Exchanges []Exchange `json:"exchanges,omitempty"`
	Sent      []string   `json:"sent,omitempty"`
}

// Exchange is one HTTP round-trip.
type Exchange struct {
	Method   string            `json:"method,omitempty"`
	URL      string            `json:"url,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Body     string            `json:"body,omitempty"`
	Status   int               `json:"status"`
	Response string            `json:"response,omitempty"`
}

func (e Evidence) headerPresent(name string) bool {
	want := strings.ToLower(name)
	for _, x := range e.Exchanges {
		for k, v := range x.Headers {
			if strings.ToLower(k) == want && v != "" && v != "<empty>" {
				return true
			}
		}
	}
	return false
}

func (e Evidence) anyContains(substr string) bool {
	if substr == "" {
		return false
	}
	for _, s := range e.Sent {
		if strings.Contains(s, substr) {
			return true
		}
	}
	for _, x := range e.Exchanges {
		if strings.Contains(x.URL, substr) || strings.Contains(x.Body, substr) || strings.Contains(x.Response, substr) {
			return true
		}
		for _, v := range x.Headers {
			if strings.Contains(v, substr) {
				return true
			}
		}
	}
	return false
}

func (e Evidence) statuses() []int {
	out := make([]int, 0, len(e.Exchanges))
	for _, x := range e.Exchanges {
		out = append(out, x.Status)
	}
	return out
}

func capturePOST(client *http.Client, rawURL string, headers map[string]string, body any) Exchange {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	ex := Exchange{
		Method:  http.MethodPost,
		URL:     rawURL,
		Headers: redactHeaders(headers),
		Body:    string(raw),
	}
	var buf io.Reader
	if raw != nil {
		buf = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(http.MethodPost, rawURL, buf)
	if err != nil {
		ex.Response = err.Error()
		return ex
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		ex.Response = err.Error()
		return ex
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	ex.Status = resp.StatusCode
	ex.Response = strings.TrimSpace(string(b))
	return ex
}

func captureGET(client *http.Client, rawURL string, headers map[string]string) Exchange {
	ex := Exchange{
		Method:  http.MethodGet,
		URL:     rawURL,
		Headers: redactHeaders(headers),
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		ex.Response = err.Error()
		return ex
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		ex.Response = err.Error()
		return ex
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	ex.Status = resp.StatusCode
	ex.Response = strings.TrimSpace(string(b))
	return ex
}

func redactHeaders(h map[string]string) map[string]string {
	if h == nil {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		lk := strings.ToLower(k)
		switch {
		case strings.Contains(lk, "secret"):
			if strings.TrimSpace(v) == "" {
				out[k] = "<empty>"
			} else {
				out[k] = "<present>"
			}
		case strings.EqualFold(k, "Authorization") && strings.HasPrefix(strings.ToLower(v), "bearer "):
			tok := strings.TrimSpace(v[7:])
			if adversarialToken(tok) {
				out[k] = "Bearer " + tok
			} else {
				out[k] = "Bearer <redacted>"
			}
		case strings.EqualFold(k, "X-Bruiser-Execution"):
			if adversarialToken(v) {
				out[k] = v
			} else {
				out[k] = "<jwt>"
			}
		case strings.EqualFold(k, "Cookie") && !strings.Contains(v, "forged"):
			out[k] = "<cookie>"
		default:
			out[k] = v
		}
	}
	return out
}

func adversarialToken(v string) bool {
	s := strings.ToLower(v)
	return strings.Contains(s, "forged") || strings.Contains(s, "not.signed") || strings.Contains(s, "not-a-jwt") || strings.Contains(s, "garbage")
}
