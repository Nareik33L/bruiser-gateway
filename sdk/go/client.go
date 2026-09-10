package bruiser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is the Bruiser-aware agent SDK. Enforcement never depends on it;
// unaware clients still hit the same rules via Edge/Proxy.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *Client) http() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

type Session struct {
	ID        string    `json:"session_id"`
	Token     string    `json:"session_token"`
	ExpiresAt time.Time `json:"expires_at"`
	Customer  string    `json:"customer_id"`
}

type Execution struct {
	ID               string `json:"execution_id"`
	Status           string `json:"status"`
	Domain           string `json:"domain"`
	Resource         string `json:"resource"`
	Action           string `json:"action"`
	Fence            int64  `json:"fence"`
	Token            string `json:"execution_token"`
	ExpiresAt        string `json:"expires_at"`
	HeartbeatAfterMs int64  `json:"heartbeat_after_ms"`
	Position         int    `json:"position"`
	Watch            string `json:"watch"`
	ActiveID         string `json:"active_execution_id"`
}

func (c *Client) CreateSession(ctx context.Context, assertion, principalType, principalID string) (Session, error) {
	if principalType == "" {
		principalType = "agent"
	}
	body, _ := json.Marshal(map[string]any{
		"principal": map[string]string{"type": principalType, "id": principalID},
	})
	var out Session
	if err := c.do(ctx, http.MethodPost, "/v1/sessions", assertion, body, http.StatusCreated, &out); err != nil {
		return Session{}, err
	}
	return out, nil
}

func (c *Client) Acquire(ctx context.Context, sessionToken, resource, action string) (Execution, error) {
	if action == "" {
		action = "purchase"
	}
	body, _ := json.Marshal(map[string]string{"resource": resource, "action": action})
	var out Execution
	if err := c.do(ctx, http.MethodPost, "/v1/executions/acquire", sessionToken, body, 0, &out); err != nil {
		return Execution{}, err
	}
	return out, nil
}

func (c *Client) Renew(ctx context.Context, sessionToken, executionID string) (Execution, error) {
	var out Execution
	if err := c.do(ctx, http.MethodPost, "/v1/executions/"+executionID+"/renew", sessionToken, nil, http.StatusOK, &out); err != nil {
		return Execution{}, err
	}
	return out, nil
}

func (c *Client) Release(ctx context.Context, sessionToken, executionID string) error {
	return c.do(ctx, http.MethodPost, "/v1/executions/"+executionID+"/release", sessionToken, nil, http.StatusOK, &map[string]any{})
}

func (c *Client) Get(ctx context.Context, sessionToken, executionID string) (Execution, error) {
	var out Execution
	if err := c.do(ctx, http.MethodGet, "/v1/executions/"+executionID, sessionToken, nil, http.StatusOK, &out); err != nil {
		return Execution{}, err
	}
	return out, nil
}

func (c *Client) do(ctx context.Context, method, path, bearer string, body []byte, want int, dst any) error {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, rdr)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if want != 0 && resp.StatusCode != want {
		return fmt.Errorf("bruiser %s %s: %d %s", method, path, resp.StatusCode, raw)
	}
	if want == 0 && resp.StatusCode >= 400 {
		return fmt.Errorf("bruiser %s %s: %d %s", method, path, resp.StatusCode, raw)
	}
	if dst == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}
