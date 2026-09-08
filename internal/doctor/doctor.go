// Package doctor is the bruiser doctor / config validate diagnostic.
package doctor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/check"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/identity"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

const (
	Pass = "PASS"
	Warn = "WARN"
	Fail = "FAIL"
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type Report struct {
	Checks  []Check `json:"checks"`
	Overall string  `json:"overall"`
}

func (r Report) Passed() bool { return r.Overall != Fail }

func (r Report) String() string {
	var b strings.Builder
	b.WriteString("Bruiser doctor\n\n")
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "  %s  %s\n", c.Status, c.Name)
		if c.Detail != "" {
			fmt.Fprintf(&b, "         %s\n", c.Detail)
		}
	}
	fmt.Fprintf(&b, "\n  Overall Result: %s\n", r.Overall)
	return b.String()
}

func (r *Report) add(name, status, detail string) {
	r.Checks = append(r.Checks, Check{Name: name, Status: status, Detail: detail})
}

func (r *Report) finish() {
	r.Overall = Pass
	for _, c := range r.Checks {
		if c.Status == Fail {
			r.Overall = Fail
			return
		}
		if c.Status == Warn {
			r.Overall = Warn
		}
	}
}

type Input struct {
	Config     config.Config
	Profile    merchant.Profile
	Store      *pgstore.Store
	ProbeStore bool
	HTTPBase   string
	FrontURL   string
	OriginURL  string
}

func Run(in Input) Report {
	var r Report
	checkConfig(&r, in.Config)
	checkIdentity(&r, in)
	checkRoutes(&r, in.Profile)
	checkUpstream(&r, in)
	checkSigning(&r, in)
	checkPersistence(&r, in)
	checkQueue(&r, in.Profile)
	checkAuthority(&r, in)
	checkLimits(&r, in.Config)
	checkMetrics(&r, in)
	checkAudit(&r, in.Config)
	r.finish()
	return r
}

func checkConfig(r *Report, cfg config.Config) {
	if err := cfg.Validate(); err != nil {
		r.add("configuration", Fail, err.Error())
		return
	}
	detail := "mode=" + cfg.Mode + " lease_ttl=" + cfg.LeaseTTL.String() + " heartbeat=" + cfg.HeartbeatInterval.String()
	if cfg.DevHMACSecret == "dev-secret-change-me" {
		r.add("configuration", Warn, detail+"; BRUISER_DEV_HMAC_SECRET is the lab default")
		return
	}
	r.add("configuration", Pass, detail)
}

func checkIdentity(r *Report, in Input) {
	issues := in.Profile.ValidateIssues()
	var fails, warns []string
	for _, i := range issues {
		if strings.HasPrefix(i.Field, "identity") {
			if i.Level == Fail {
				fails = append(fails, i.Message)
			} else {
				warns = append(warns, i.Message)
			}
		}
	}
	if len(fails) > 0 {
		r.add("identity extraction", Fail, strings.Join(fails, "; "))
		return
	}
	if in.Config.DevHMACSecret != "" {
		tok, err := auth.IssueBoxOfficeSession(in.Config.DevHMACSecret, "doctor-probe", time.Hour)
		if err == nil {
			if _, err := identity.Extract(in.Profile.Identity, in.Config.DevHMACSecret, tok, "", "", ""); err == nil {
				if len(warns) > 0 {
					r.add("identity extraction", Warn, strings.Join(warns, "; "))
					return
				}
				r.add("identity extraction", Pass, "extractor="+in.Profile.Identity.Extractor+" minted session parsed")
				return
			}
		}
	}
	if len(warns) > 0 {
		r.add("identity extraction", Warn, strings.Join(warns, "; "))
		return
	}
	r.add("identity extraction", Pass, "extractor="+in.Profile.Identity.Extractor)
}

func checkRoutes(r *Report, p merchant.Profile) {
	n := 0
	for _, rt := range p.Routes {
		if rt.Controlled() {
			n++
		}
	}
	if n == 0 {
		r.add("protected routes", Fail, "no allocation routes with resource / resource_from")
		return
	}
	r.add("protected routes", Pass, fmt.Sprintf("%d allocation route(s); unmatched=%s", n, p.Unmatched))
}

func checkUpstream(r *Report, in Input) {
	url := firstNonEmpty(in.OriginURL, in.Config.OriginURL)
	if url == "" {
		r.add("upstream connectivity", Warn, "BRUISER_ORIGIN_URL not set; skip (Edge/Embedded may not need it)")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(url, "/")+"/healthz", nil)
	if err != nil {
		r.add("upstream connectivity", Fail, err.Error())
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.add("upstream connectivity", Fail, err.Error())
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	r.add("upstream connectivity", Pass, fmt.Sprintf("%s → %d", url, resp.StatusCode))
}

func checkSigning(r *Report, in Input) {
	if !in.ProbeStore && in.Store == nil {
		r.add("signing keys", Warn, "store not probed")
		return
	}
	st := in.Store
	var closer func()
	if st == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var err error
		st, err = pgstore.Connect(ctx, in.Config.DatabaseURL)
		if err != nil {
			r.add("signing keys", Fail, err.Error())
			return
		}
		closer = st.Close
	}
	if closer != nil {
		defer closer()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	keys, err := st.ActiveSigningKeys(ctx, in.Config.MerchantID)
	if err != nil {
		r.add("signing keys", Fail, err.Error())
		return
	}
	if len(keys) == 0 {
		r.add("signing keys", Warn, "no active signing key yet (created on serve)")
		return
	}
	r.add("signing keys", Pass, fmt.Sprintf("%d active Ed25519 key(s)", len(keys)))
}

func checkPersistence(r *Report, in Input) {
	if !in.ProbeStore && in.Store == nil {
		r.add("persistence", Warn, "store not probed")
		return
	}
	st := in.Store
	var closer func()
	if st == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var err error
		st, err = pgstore.Connect(ctx, in.Config.DatabaseURL)
		if err != nil {
			r.add("persistence", Fail, err.Error())
			return
		}
		closer = st.Close
	}
	if closer != nil {
		defer closer()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := st.Ping(ctx); err != nil {
		r.add("persistence", Fail, err.Error())
		return
	}
	r.add("persistence", Pass, "postgres reachable")
}

func checkQueue(r *Report, p merchant.Profile) {
	w := p.Policy.Waiting
	if w.Mode == "" && w.MaxWaiters == 0 {
		r.add("queue configuration", Pass, "queue off (BUSY after max_active); intra-customer only when enabled")
		return
	}
	if strings.EqualFold(w.Mode, "bounded") && w.MaxWaiters < 1 {
		r.add("queue configuration", Fail, "bounded queue requires max_waiters >= 1")
		return
	}
	if w.Mode != "" && !strings.EqualFold(w.Mode, "bounded") && !strings.EqualFold(w.Mode, "off") {
		r.add("queue configuration", Fail, "waiting.mode must be bounded or off")
		return
	}
	r.add("queue configuration", Pass, fmt.Sprintf("mode=%s max_waiters=%d (intra-customer, not a waiting room)", w.Mode, w.MaxWaiters))
}

func checkAuthority(r *Report, in Input) {
	front := firstNonEmpty(in.FrontURL, in.Config.CheckEdgeURL)
	origin := firstNonEmpty(in.OriginURL, in.Config.CheckOriginURL)
	if in.FrontURL == "" {
		r.add("authority enforcement", Warn, "pass --front to run bruiser authority-check (go-live gate)")
		return
	}
	rep, err := check.Run(check.Config{
		EdgeURL:    front,
		OriginURL:  origin,
		HMACSecret: in.Config.DevHMACSecret,
	})
	if err != nil {
		r.add("authority enforcement", Fail, err.Error())
		return
	}
	if !rep.Passed() {
		r.add("authority enforcement", Fail, "Overall Result "+rep.Overall)
		return
	}
	r.add("authority enforcement", Pass, "Overall Result PASS")
}

func checkLimits(r *Report, cfg config.Config) {
	if cfg.MaxInFlight < 1 {
		r.add("active-execution protection", Fail, "BRUISER_MAX_IN_FLIGHT must be >= 1")
		return
	}
	if cfg.RatePerSec <= 0 {
		r.add("active-execution protection", Warn, "BRUISER_RATE_PER_SEC is 0; a shared execution can be hammered")
		return
	}
	r.add("active-execution protection", Pass, fmt.Sprintf("in_flight=%d rate=%.1f/s burst=%d deadline=8s", cfg.MaxInFlight, cfg.RatePerSec, cfg.Burst))
}

func checkMetrics(r *Report, in Input) {
	base := in.HTTPBase
	if base == "" {
		r.add("metrics", Warn, "pass --http to probe /metrics")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/metrics", nil)
	if err != nil {
		r.add("metrics", Fail, err.Error())
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.add("metrics", Fail, err.Error())
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(raw), "bruiser_") {
		r.add("metrics", Fail, fmt.Sprintf("GET /metrics → %d (expected bruiser_ series)", resp.StatusCode))
		return
	}
	r.add("metrics", Pass, "Prometheus bruiser_* series present")
}

func checkAudit(r *Report, cfg config.Config) {
	if cfg.AuditRetention <= 0 {
		r.add("audit configuration", Warn, "BRUISER_AUDIT_RETENTION is 0 (purge disabled)")
		return
	}
	r.add("audit configuration", Pass, "retention="+cfg.AuditRetention.String()+" (merchant Postgres, no vendor store)")
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func LoadProfile(path string, merchantID string) (merchant.Profile, error) {
	if path == "" {
		return merchant.Empty(merchantID), fmt.Errorf("profile path empty")
	}
	if _, err := os.Stat(path); err != nil {
		return merchant.Empty(merchantID), err
	}
	return merchant.LoadFile(path)
}
