package attacklab

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/demostore"
	"github.com/Nareik33L/bruiser-gateway/internal/edge"
	"github.com/Nareik33L/bruiser-gateway/internal/id"
)

const (
	MaxSwarm              = 100
	MinSwarm              = 10
	DefaultSwarm          = 50
	MaxStores             = 40
	StoresPerIP           = 8
	MaxConcurrentAttacks  = 2
	MaxCheckoutsPerAttack = 700
	StoreTTL              = 20 * time.Minute
	attackCooldown        = 2 * time.Second
)

type Config struct {
	HMACSecret   string
	EdgeSecret   string
	OriginSecret string
	Gateway      http.Handler
	BruiserURL   string
	Log          *slog.Logger
	StoreTTL     time.Duration
}

type Lab struct {
	cfg    Config
	origin *demostore.Registry
	edge   http.Handler
	log    *slog.Logger

	mu       sync.Mutex
	sessions map[string]*session
	attacks  map[string]*Attack
	creates  map[string][]time.Time
	running  int
}

type session struct {
	Store     demostore.Store
	Mode      string
	IP        string
	Agents    []Agent
	LastSize  int
	lastAt    time.Time
	attacking bool
}

type Metrics struct {
	Total          int `json:"total"`
	Allowed        int `json:"allowed"`
	Suspicious     int `json:"suspicious"`
	Blocked        int `json:"blocked"`
	WouldBeBlocked int `json:"would_be_blocked"`
	OriginOrders   int `json:"origin_orders"`
}

type Event struct {
	Type      string   `json:"type"`
	At        int64    `json:"at"`
	ID        int      `json:"id,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	Icon      string   `json:"icon,omitempty"`
	Title     string   `json:"title,omitempty"`
	Subtitle  string   `json:"subtitle,omitempty"`
	Detail    string   `json:"detail,omitempty"`
	Verdict   string   `json:"verdict,omitempty"`
	Label     string   `json:"label,omitempty"`
	Note      string   `json:"note,omitempty"`
	Bruiser   string   `json:"bruiser,omitempty"`
	Forwarded bool     `json:"forwarded,omitempty"`
	Metrics   *Metrics `json:"metrics,omitempty"`
	Summary   *Summary `json:"summary,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type Summary struct {
	Mode           string `json:"mode"`
	SwarmSize      int    `json:"swarm_size"`
	Total          int    `json:"total"`
	Allowed        int    `json:"allowed"`
	Suspicious     int    `json:"suspicious"`
	Blocked        int    `json:"blocked"`
	WouldBeBlocked int    `json:"would_be_blocked"`
	OriginOrders   int    `json:"origin_orders"`
	Headline       string `json:"headline"`
}

type Attack struct {
	ID        string    `json:"id"`
	StoreID   string    `json:"store_id"`
	Wave      string    `json:"wave"`
	Mode      string    `json:"mode"`
	SwarmSize int       `json:"swarm_size"`
	StartedAt time.Time `json:"started_at"`

	mu      sync.Mutex
	events  []Event
	subs    []chan Event
	Metrics Metrics
	Done    bool
	Summary *Summary
}

func New(cfg Config) (*Lab, error) {
	if cfg.StoreTTL <= 0 {
		cfg.StoreTTL = StoreTTL
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	origin := demostore.New(demostore.Config{
		HMACSecret:   cfg.HMACSecret,
		OriginSecret: cfg.OriginSecret,
	})
	l := &Lab{
		cfg:      cfg,
		origin:   origin,
		log:      cfg.Log,
		sessions: map[string]*session{},
		attacks:  map[string]*Attack{},
		creates:  map[string][]time.Time{},
	}
	p, err := edge.New(edge.Config{
		OriginHandler:  origin.Handler(),
		BruiserHandler: cfg.Gateway,
		BruiserURL:     cfg.BruiserURL,
		EdgeSecret:     cfg.EdgeSecret,
		OriginSecret:   cfg.OriginSecret,
		MaxInFlight:    8,
		ModeFn:         l.modeForRequest,
	})
	if err != nil {
		return nil, err
	}
	l.edge = p.Handler()
	return l, nil
}

func (l *Lab) Storefront() http.Handler { return l.edge }

func (l *Lab) Origin() *demostore.Registry { return l.origin }

func (l *Lab) modeForRequest(r *http.Request) string {
	id := storeIDFromPath(r.URL.Path)
	if id == "" {
		return edge.ModeEnforce
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.sessions[id]
	if !ok {
		return edge.ModeEnforce
	}
	return s.Mode
}

func storeIDFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "s" {
		return ""
	}
	return parts[1]
}

func (l *Lab) Sweep() {
	l.origin.ExpireDue()
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for id, s := range l.sessions {
		if !s.Store.ExpiresAt.After(now) {
			delete(l.sessions, id)
		}
	}
}

func (l *Lab) getSession(id string) (*session, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.sessions[id]
	if !ok || !s.Store.ExpiresAt.After(time.Now()) {
		if ok {
			delete(l.sessions, id)
		}
		return nil, false
	}
	return s, true
}

func newAttack(storeID, mode string, size int) *Attack {
	return &Attack{
		ID:        id.New("att"),
		StoreID:   storeID,
		Wave:      id.New("wav")[4:],
		Mode:      mode,
		SwarmSize: size,
		StartedAt: time.Now().UTC(),
	}
}

func (a *Attack) publish(e Event) {
	if e.At == 0 {
		e.At = time.Now().UnixMilli()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if e.Type == "agent" || e.Type == "metrics" || e.Type == "done" || e.Type == "error" {
		if len(a.events) > 800 {
			a.events = a.events[len(a.events)-600:]
		}
		a.events = append(a.events, e)
	}
	for _, ch := range a.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (a *Attack) subscribe() (chan Event, func(), []Event) {
	ch := make(chan Event, 64)
	a.mu.Lock()
	replay := append([]Event(nil), a.events...)
	a.subs = append(a.subs, ch)
	a.mu.Unlock()
	cancel := func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		out := a.subs[:0]
		for _, s := range a.subs {
			if s != ch {
				out = append(out, s)
			}
		}
		a.subs = out
	}
	return ch, cancel, replay
}

func (a *Attack) snapshot() (Metrics, bool, *Summary) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Metrics, a.Done, a.Summary
}

func (a *Attack) record(verdict string, forwarded bool, originOrder bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Metrics.Total++
	switch verdict {
	case "allowed":
		a.Metrics.Allowed++
	case "suspicious":
		a.Metrics.Suspicious++
		a.Metrics.WouldBeBlocked++
	case "blocked":
		a.Metrics.Blocked++
		a.Metrics.Suspicious++
	}
	if originOrder {
		a.Metrics.OriginOrders++
	}
	_ = forwarded
}

func looksLikeURL(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "//") {
		return true
	}
	if strings.Contains(s, "://") {
		return true
	}
	return false
}

func forbiddenKey(k string) bool {
	k = strings.ToLower(strings.TrimSpace(k))
	k = strings.ReplaceAll(k, "-", "")
	k = strings.ReplaceAll(k, "_", "")
	switch k {
	case "url", "uri", "target", "targeturl", "origin", "originurl", "host", "hostname",
		"endpoint", "webhook", "domain", "baseurl", "callback", "site", "website":
		return true
	}
	return strings.Contains(k, "url")
}
