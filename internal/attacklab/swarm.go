package attacklab

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/demostore"
	"github.com/Nareik33L/bruiser-gateway/internal/edge"
)

type Agent struct {
	Number          int
	Kind            string
	Human           bool
	CustomerID      string
	DisplayAccounts int
	Browse          int
	Checkouts       int
	DelayMin        time.Duration
	DelayMax        time.Duration
	NewSessionEach  bool
	Label           string
	Detail          string
}

func compose(n int, storeID string) []Agent {
	if n < MinSwarm {
		n = MinSwarm
	}
	if n > MaxSwarm {
		n = MaxSwarm
	}
	nNormal := n * 40 / 100
	nMobile := n * 16 / 100
	nRapid := n * 20 / 100
	nMulti := n * 14 / 100
	nDup := n - nNormal - nMobile - nRapid - nMulti
	if nDup < 0 {
		nDup = 0
	}

	agents := make([]Agent, 0, n)
	num := 1
	tag := idTail(storeID)
	add := func(a Agent) {
		a.Number = num
		num++
		agents = append(agents, a)
	}

	for i := 0; i < nNormal; i++ {
		add(Agent{
			Kind: "normal", Human: true,
			CustomerID: fmt.Sprintf("fan-%s-%02d", tag, i+1),
			Browse:     3 + i%4, Checkouts: 1,
			DelayMin: 35 * time.Millisecond, DelayMax: 90 * time.Millisecond,
			Label: "Normal supporter", Detail: "Single identity · normal checkout",
		})
	}
	for i := 0; i < nMobile; i++ {
		add(Agent{
			Kind: "mobile", Human: true,
			CustomerID: fmt.Sprintf("mob-%s-%02d", tag, i+1),
			Browse:     4 + i%3, Checkouts: 1,
			DelayMin: 80 * time.Millisecond, DelayMax: 180 * time.Millisecond,
			Label: "Mobile supporter", Detail: "Slower browsing · variable requests",
		})
	}

	rapidClusters := 2
	if nRapid >= 8 {
		rapidClusters = 3
	}
	for i := 0; i < nRapid; i++ {
		cluster := i % rapidClusters
		checkouts := 12 + (i % 7)
		add(Agent{
			Kind: "rapid", Human: false,
			CustomerID: fmt.Sprintf("bot-rapid-%s-%d", tag, cluster),
			Browse:     2, Checkouts: checkouts,
			DelayMin: 4 * time.Millisecond, DelayMax: 14 * time.Millisecond,
			NewSessionEach: true,
			Label:          "Rapid-fire bot", Detail: "Extremely fast repeated checkout attempts",
		})
	}
	multiClusters := 2
	if nMulti >= 8 {
		multiClusters = 3
	}
	for i := 0; i < nMulti; i++ {
		cluster := i % multiClusters
		inCluster := nMulti/multiClusters + 2
		add(Agent{
			Kind: "multi", Human: false,
			CustomerID:      fmt.Sprintf("bot-multi-%s-%d", tag, cluster),
			DisplayAccounts: inCluster,
			Browse:          2, Checkouts: 6 + i%4,
			DelayMin: 12 * time.Millisecond, DelayMax: 40 * time.Millisecond,
			NewSessionEach: true,
			Label:          "Multi-account bot", Detail: "Multiple accounts · shared identity",
		})
	}
	dupClusters := 2
	for i := 0; i < nDup; i++ {
		cluster := i % dupClusters
		add(Agent{
			Kind: "duplicate", Human: false,
			CustomerID: fmt.Sprintf("bot-dup-%s-%d", tag, cluster),
			Browse:     2, Checkouts: 8 + i%5,
			DelayMin: 15 * time.Millisecond, DelayMax: 45 * time.Millisecond,
			NewSessionEach: true,
			Label:          "Duplicate purchaser", Detail: "Same identity · many sessions",
		})
	}

	totalCO := 0
	for _, a := range agents {
		totalCO += a.Checkouts
	}
	if totalCO > MaxCheckoutsPerAttack && totalCO > 0 {
		scale := float64(MaxCheckoutsPerAttack) / float64(totalCO)
		for i := range agents {
			if agents[i].Human {
				continue
			}
			n := int(float64(agents[i].Checkouts) * scale)
			if n < 2 {
				n = 2
			}
			agents[i].Checkouts = n
		}
	}
	return agents
}

type labTransport struct{ h http.Handler }

func (t labTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := strings.ToLower(req.URL.Host)
	if host != "sandbox.bruiser.invalid" && host != "" {
		return nil, fmt.Errorf("refusing non-sandbox host %q", req.URL.Host)
	}
	if req.Body == nil {
		req.Body = http.NoBody
	}
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func (l *Lab) runAttack(a *Attack, agents []Agent) {
	defer func() {
		l.mu.Lock()
		if s, ok := l.sessions[a.StoreID]; ok {
			s.attacking = false
			s.lastAt = time.Now()
		}
		l.running--
		if l.running < 0 {
			l.running = 0
		}
		l.mu.Unlock()
	}()
	client := &http.Client{
		Transport: labTransport{h: l.edge},
		Timeout:   8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	sem := make(chan struct{}, 12)
	var wg sync.WaitGroup
	for i := range agents {
		ag := agents[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			l.runAgent(client, a, ag)
		}()
	}
	wg.Wait()

	a.mu.Lock()
	m := a.Metrics
	a.Done = true
	sum := Summary{
		Mode:           a.Mode,
		SwarmSize:      a.SwarmSize,
		Total:          m.Total,
		Allowed:        m.Allowed,
		Suspicious:     m.Suspicious,
		Blocked:        m.Blocked,
		WouldBeBlocked: m.WouldBeBlocked,
		OriginOrders:   m.OriginOrders,
	}
	if a.Mode == edge.ModeDryRun {
		sum.Headline = fmt.Sprintf("%s would have been stopped", formatInt(sum.WouldBeBlocked))
	} else {
		sum.Headline = fmt.Sprintf("%s bad actors stopped", formatInt(sum.Blocked))
	}
	a.Summary = &sum
	a.mu.Unlock()

	a.publish(Event{Type: "done", Summary: &sum, Metrics: &m})
}

func (l *Lab) runAgent(client *http.Client, a *Attack, ag Agent) {
	base := "http://sandbox.bruiser.invalid/s/" + a.StoreID
	cookie, err := auth.IssueBoxOfficeSession(l.cfg.HMACSecret, ag.CustomerID, time.Hour)
	if err != nil {
		return
	}
	ua := "BruiserAttackLab/1.0"
	if ag.Kind == "mobile" {
		ua = "BruiserAttackLab/1.0 (iPhone; Mobile)"
	} else if !ag.Human {
		ua = "BruiserAttackLab/1.0 (agent; " + ag.Kind + ")"
	}

	checkouts := 0
	start := time.Now()
	emitCheckout := func(i int) bool {
		if ag.Human {
			return true
		}
		return i == 0 || i == ag.Checkouts-1 || i%4 == 0
	}

	for i := 0; i < ag.Browse; i++ {
		path := base
		if i%2 == 1 {
			path = base + "/product"
		}
		req, err := http.NewRequest(http.MethodGet, path, nil)
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", ua)
		req.AddCookie(&http.Cookie{Name: demostore.CookieName, Value: cookie})
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
		verdict, _, _, _, fwd := classify(a.Mode, resp)
		origin := false
		a.record(verdict, fwd, origin)
		sleepJitter(ag.DelayMin, ag.DelayMax)
	}

	for i := 0; i < ag.Checkouts; i++ {
		if ag.NewSessionEach || i == 0 {
			tok, err := auth.IssueBoxOfficeSession(l.cfg.HMACSecret, ag.CustomerID, time.Hour)
			if err != nil {
				return
			}
			cookie = tok
		}
		url := base + "/drops/" + a.Wave + "/checkout"
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(`{"qty":1}`))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", ua)
		req.AddCookie(&http.Cookie{Name: demostore.CookieName, Value: cookie})
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
		checkouts++
		verdict, label, note, bruiser, fwd := classify(a.Mode, resp)
		if ag.Kind == "duplicate" && bruiser == "BUSY" {
			label = "DUPLICATE"
		}
		origin := resp.StatusCode >= 200 && resp.StatusCode < 300 && fwd
		a.record(verdict, fwd, origin)
		if emitCheckout(i) {
			elapsed := time.Since(start).Seconds()
			detail := ag.Detail
			if !ag.Human && elapsed > 0 && checkouts > 1 {
				rate := float64(checkouts) / elapsed
				if ag.Kind == "rapid" {
					detail = fmt.Sprintf("%.0f requests/sec", rate)
				} else if ag.Kind == "multi" {
					detail = fmt.Sprintf("%d accounts · %d requests", max(ag.DisplayAccounts, 2), checkouts)
				} else if ag.Kind == "duplicate" {
					detail = fmt.Sprintf("Same identity · %d sessions", checkouts)
				}
			}
			icon := "bot"
			if ag.Human {
				icon = "human"
			}
			a.publish(Event{
				Type:      "agent",
				ID:        ag.Number,
				Kind:      ag.Kind,
				Icon:      icon,
				Title:     fmt.Sprintf("Agent #%03d", ag.Number),
				Subtitle:  ag.Label,
				Detail:    detail,
				Verdict:   verdict,
				Label:     label,
				Note:      note,
				Bruiser:   bruiser,
				Forwarded: fwd,
			})
		}
		met := a.cloneMetrics()
		a.publish(Event{Type: "metrics", Metrics: &met})
		sleepJitter(ag.DelayMin, ag.DelayMax)
	}
}

func (a *Attack) cloneMetrics() Metrics {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Metrics
}

func classify(mode string, resp *http.Response) (verdict, label, note, bruiser string, forwarded bool) {
	bruiser = resp.Header.Get(edge.HeaderDecision)
	if bruiser == "" {
		if resp.StatusCode == http.StatusConflict {
			bruiser = "BUSY"
		} else if resp.StatusCode >= 400 {
			bruiser = "DENIED"
		} else {
			bruiser = "ALLOW"
		}
	}
	forwarded = resp.Header.Get(edge.HeaderForwarded) == "1" || (resp.Header.Get(edge.HeaderForwarded) == "" && resp.StatusCode < 400)
	busy := bruiser == "BUSY" || bruiser == "DENIED" || bruiser == "THROTTLED"
	if !busy {
		verdict = "allowed"
		label = "LEGITIMATE"
		if mode == edge.ModeDryRun {
			note = "Would be allowed"
		} else {
			note = "Allowed"
		}
		return
	}
	if mode == edge.ModeDryRun {
		verdict = "suspicious"
		label = "SUSPICIOUS"
		note = "Would be blocked"
		return
	}
	verdict = "blocked"
	label = "BLOCKED"
	note = "Rejected at the gateway"
	return
}

func sleepJitter(min, max time.Duration) {
	if max <= min {
		time.Sleep(min)
		return
	}
	d := min + time.Duration(time.Now().UnixNano()%int64(max-min))
	time.Sleep(d)
}

func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 1000 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre == 0 {
		pre = 3
	}
	b.WriteString(s[:pre])
	for i := pre; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func idTail(s string) string {
	if len(s) <= 6 {
		return s
	}
	return s[len(s)-6:]
}
