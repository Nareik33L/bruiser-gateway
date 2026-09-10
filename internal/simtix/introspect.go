package simtix

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

func (s *Server) introspectLive(token string) error {
	if s.cfg.IntrospectURL == "" {
		return nil
	}
	raw, _ := json.Marshal(map[string]string{"token": token})
	req, err := http.NewRequest(http.MethodPost, s.cfg.IntrospectURL, bytes.NewReader(raw))
	if err != nil {
		return ErrNoVerifier
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Partition: local signature/expiry already passed. A held token
		// is enough; do not fail-open for *new* allocations (no token).
		return nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return authDenied(b)
	}
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var out struct {
		Active bool   `json:"active"`
		Reason string `json:"reason"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return ErrNoVerifier
	}
	if !out.Active {
		if out.Reason == "stale_fence" {
			return ErrStaleFence
		}
		return ErrRevoked
	}
	return nil
}

func authDenied(body []byte) error {
	if bytes.Contains(body, []byte("stale_fence")) {
		return ErrStaleFence
	}
	return ErrRevoked
}
