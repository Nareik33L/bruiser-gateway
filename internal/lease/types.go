package lease

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/resource"
)

const (
	StateActive    = "ACTIVE"
	StateReleased  = "RELEASED"
	StateExpired   = "EXPIRED"
	StateRevoked   = "REVOKED"
	StateHandedOff = "HANDED_OFF"

	StatusGranted     = "GRANTED"
	StatusAlreadyHeld = "ALREADY_HELD"
	StatusBusy        = "BUSY"
	StatusQueued      = "QUEUED"
	StatusDenied      = "DENIED"

	StateQueued = "QUEUED"

	ReasonExpired   = "EXPIRED"
	ReasonRevoked   = "REVOKED"
	ReasonHandedOff = "HANDED_OFF"
	ReasonReleased  = "RELEASED"

	ModePreempt     = "preempt"
	ModeCooperative = "cooperative"
)

var (
	ErrNotFound     = errors.New("execution not found")
	ErrNotHolder    = errors.New("caller is not the holder")
	ErrUnavailable  = errors.New("store unavailable")
	ErrGone         = errors.New("execution is no longer active")
	ErrInvalidInput = errors.New("invalid input")
	ErrPrecedence   = errors.New("caller cannot preempt holder")
	ErrForbidden    = errors.New("not allowed")
)

type GoneError struct {
	Reason      string
	SuccessorID string
	ExecutionID string
}

func (e *GoneError) Error() string {
	return fmt.Sprintf("execution %s is %s", e.ExecutionID, e.Reason)
}

func (e *GoneError) Unwrap() error { return ErrGone }

type Principal struct {
	Type string
	ID   string
}

type Execution struct {
	ID            string
	MerchantID    string
	DomainKey     string
	CustomerID    string
	Principal     Principal
	SessionID     string
	Resource      string
	Action        string
	RuleName      string
	Fence         int64
	State         string
	GrantedAt     time.Time
	ExpiresAt     time.Time
	MaxLifetimeAt time.Time
	RenewCount    int
	EndedAt       *time.Time
	EndReason     string
	SuccessorID   string
	QueuePosition int
	OpsUsed       int
	MaxOps        int
}

func (e Execution) IsActive(now time.Time) bool {
	return e.State == StateActive && e.ExpiresAt.After(now)
}

type AcquireRequest struct {
	MerchantID  string
	DomainKey   string
	CustomerID  string
	Principal   Principal
	SessionID   string
	Resource    string
	Action      string
	RuleName    string
	MaxActive   int
	TTL         time.Duration
	MaxLifetime time.Duration
	Precedence  []string
	MaxWaiters  int
	MaxOps      int
	RequestID   string
}

type QueueInfo struct {
	WaiterID          string
	Position          int
	ActiveExecutionID string
	ExpiresAt         time.Time
}

type BusyInfo struct {
	ActiveExecutionID string
	Holder            Principal
	ExpiresAt         time.Time
	CanPreempt        bool
}

type AcquireResult struct {
	Status    string
	Execution *Execution
	Busy      *BusyInfo
	Queue     *QueueInfo
	Reason    string
}

type HandoffRequest struct {
	MerchantID  string
	ExecutionID string
	SessionID   string
	To          Principal
	Mode        string // "preempt", "cooperative", or empty (inferred)
	TTL         time.Duration
	Precedence  []string
	RequestID   string
}

type HandoffResult struct {
	Predecessor Execution
	Successor   Execution
	Mode        string
}

type Store interface {
	Acquire(ctx context.Context, req AcquireRequest) (AcquireResult, error)
	Renew(ctx context.Context, merchantID, executionID, sessionID, requestID string, ttl time.Duration) (Execution, error)
	Release(ctx context.Context, merchantID, executionID, sessionID, requestID string) (Execution, error)
	Revoke(ctx context.Context, merchantID, executionID, sessionID, requestID, reason string) (Execution, error)
	Handoff(ctx context.Context, req HandoffRequest) (HandoffResult, error)
	Get(ctx context.Context, merchantID, executionID string) (Execution, error)
	ExpireDue(ctx context.Context, limit int) (int, error)
	Ping(ctx context.Context) error
}

func DomainKey(merchantID, ruleName, customerID, res string) string {
	if canon, err := resource.Canonical(res); err == nil {
		res = canon
	}
	return merchantID + "/" + ruleName + "/customer=" + customerID + "/resource=" + res
}

func DefaultRuleName() string { return "purchase-per-event" }
