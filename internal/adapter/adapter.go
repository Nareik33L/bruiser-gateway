// Package adapter is the generic commerce integration surface.
// Bruiser's execution engine does not change when a merchant is added.
package adapter

import "context"

type SearchQuery struct {
	Query string
}

type SearchResult struct {
	Items []map[string]any
}

type HoldRequest struct {
	CustomerID string
	Resource   string
	Seats      int
}

type HoldResult struct {
	HoldID string `json:"hold_id"`
	OK     bool   `json:"ok"`
}

type PurchaseRequest struct {
	CustomerID string
	HoldID     string
	Resource   string
}

type PurchaseResult struct {
	OrderID string `json:"order_id"`
	OK      bool   `json:"ok"`
}

// Commerce is optional. Not every merchant implements every operation.
// The core lease engine does not depend on this interface.
type Commerce interface {
	Search(ctx context.Context, q SearchQuery) (SearchResult, error)
	Hold(ctx context.Context, h HoldRequest) (HoldResult, error)
	Release(ctx context.Context, holdID string) error
	Purchase(ctx context.Context, p PurchaseRequest) (PurchaseResult, error)
	Cancel(ctx context.Context, orderID string) error
}
