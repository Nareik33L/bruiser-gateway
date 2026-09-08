package adapter

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

// HTTP talks to an existing commerce API using the merchant's own paths.
type HTTP struct {
	Origin     string
	HoldPath   string
	ReleaseFmt string
	OrderPath  string
	SearchPath string
	Secret     string
	Client     *http.Client
}

func (h HTTP) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return &http.Client{Timeout: 8 * time.Second}
}

func (h HTTP) Search(ctx context.Context, q SearchQuery) (SearchResult, error) {
	path := h.SearchPath
	if path == "" {
		path = "/api/events"
	}
	code, body, err := h.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return SearchResult{}, err
	}
	if code >= 300 {
		return SearchResult{}, fmt.Errorf("search %d", code)
	}
	var items []map[string]any
	_ = json.Unmarshal(body, &items)
	return SearchResult{Items: items}, nil
}

func (h HTTP) Hold(ctx context.Context, req HoldRequest) (HoldResult, error) {
	path := h.HoldPath
	if path == "" {
		path = "/api/holds"
	}
	code, body, err := h.do(ctx, http.MethodPost, path, map[string]any{
		"customer_id": req.CustomerID,
		"resource":    req.Resource,
		"seats":       req.Seats,
	})
	if err != nil {
		return HoldResult{}, err
	}
	if code >= 300 {
		return HoldResult{OK: false}, fmt.Errorf("hold %d", code)
	}
	var out HoldResult
	_ = json.Unmarshal(body, &out)
	out.OK = true
	return out, nil
}

func (h HTTP) Release(ctx context.Context, holdID string) error {
	path := h.ReleaseFmt
	if path == "" {
		path = "/api/holds/" + holdID
	} else {
		path = strings.ReplaceAll(path, "{id}", holdID)
	}
	_, _, err := h.do(ctx, http.MethodDelete, path, nil)
	return err
}

func (h HTTP) Purchase(ctx context.Context, req PurchaseRequest) (PurchaseResult, error) {
	path := h.OrderPath
	if path == "" {
		path = "/api/orders"
	}
	code, body, err := h.do(ctx, http.MethodPost, path, map[string]any{
		"customer_id": req.CustomerID,
		"hold_id":     req.HoldID,
		"resource":    req.Resource,
	})
	if err != nil {
		return PurchaseResult{}, err
	}
	if code >= 300 {
		return PurchaseResult{}, fmt.Errorf("purchase %d", code)
	}
	var out PurchaseResult
	_ = json.Unmarshal(body, &out)
	out.OK = true
	return out, nil
}

func (h HTTP) Cancel(ctx context.Context, orderID string) error {
	_, _, err := h.do(ctx, http.MethodPost, "/api/orders/"+orderID+"/cancel", nil)
	return err
}

func (h HTTP) do(ctx context.Context, method, path string, payload any) (int, []byte, error) {
	var rdr io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(h.Origin, "/")+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if h.Secret != "" {
		req.Header.Set("X-Bruiser-Origin-Secret", h.Secret)
	}
	resp, err := h.client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, nil
}

var _ Commerce = HTTP{}
