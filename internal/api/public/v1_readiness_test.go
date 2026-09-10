package publicapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	publicapi "github.com/Nareik33L/bruiser-gateway/internal/api/public"
	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/ops"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
	"log/slog"
)

func TestSecurityAdminAndEdgeSecrets(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	srv, cfg := lab.Server, lab.Cfg
	if cfg.AdminSecret == cfg.EdgeSecret || cfg.AdminSecret == cfg.OperatorSecret {
		t.Fatal("lab must use distinct admin, operator, and edge secrets")
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/admin/status", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("public admin path want 404 got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, lab.Admin.URL+"/v1/admin/status", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("admin without secret %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, lab.Admin.URL+"/v1/admin/status", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.EdgeSecret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("edge secret must not unlock admin %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, lab.Admin.URL+"/v1/admin/status?secret="+cfg.AdminSecret, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("query-string admin secret must be rejected %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/authorize", bytes.NewBufferString(`{"method":"POST","path":"/api/events/ars-che/holds"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Edge-Secret", "wrong")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad edge secret %d", resp.StatusCode)
	}
}

func TestSecurityMalformedAndCrossTenant(t *testing.T) {
	srv, cfg := startServer(t)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/sessions", bytes.NewBufferString(`{`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("malformed session body %d", resp.StatusCode)
	}

	tok := session(t, srv, cfg, "alice", "agent-1")
	got := acquireJSON(t, srv, tok)
	if got.StatusCode != http.StatusCreated {
		t.Fatalf("acquire %d %s", got.StatusCode, got.Raw)
	}
	bob := session(t, srv, cfg, "bob", "agent-1")
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/"+got.ID+"/renew", nil)
	req.Header.Set("Authorization", "Bearer "+bob)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("bob must not renew alice's execution")
	}

	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/acquire", bytes.NewBufferString(`{"resource":"","action":"hold"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty resource %d", resp.StatusCode)
	}
}

func TestFailClosedOnStoreOutage(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	tok := session(t, lab.Server, lab.Cfg, "alice", "agent-1")
	lab.Store.Close()

	req, _ := http.NewRequest(http.MethodPost, lab.Server.URL+"/v1/executions/acquire", bytes.NewBufferString(`{"resource":"event:ars-che","action":"hold"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		t.Fatalf("store outage must not fail-open acquire: %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("store outage status %d", resp.StatusCode)
	}
}

func TestFailOpenOnlyWhenConfigured(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	body := `{"fail_closed":false,"updated_by":"test"}`
	req, _ := http.NewRequest(http.MethodPut, lab.Admin.URL+"/v1/admin/controls", bytes.NewBufferString(body))
	req.Header.Set("X-Bruiser-Admin-Secret", lab.Cfg.AdminSecret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("controls %d", resp.StatusCode)
	}
	tok := session(t, lab.Server, lab.Cfg, "alice", "agent-1")
	lab.Store.Close()
	req, _ = http.NewRequest(http.MethodPost, lab.Server.URL+"/v1/executions/acquire", bytes.NewBufferString(`{"resource":"event:ars-che","action":"hold"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte("fail-open")) {
		t.Fatalf("fail_closed=false should ALLOW: %d %s", resp.StatusCode, raw)
	}
}

func TestPersistenceAcrossReplica(t *testing.T) {
	a, b := testlab.GatewayPair(t, testlab.ArsenalProfile(t))
	srvA, srvB, cfg := a.Server, b.Server, a.Cfg
	tok := session(t, srvA, cfg, "alice", "agent-1")
	got := acquireJSON(t, srvA, tok)
	if got.StatusCode != http.StatusCreated {
		t.Fatalf("acquire on A %d %s", got.StatusCode, got.Raw)
	}
	tokB := session(t, srvB, cfg, "alice", "agent-1")
	req, _ := http.NewRequest(http.MethodPost, srvB.URL+"/v1/executions/acquire", bytes.NewBufferString(`{"resource":"event:ars-che","action":"purchase"}`))
	req.Header.Set("Authorization", "Bearer "+tokB)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var resumed struct {
		ID string `json:"execution_id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&resumed)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replica resume want 200 got %d", resp.StatusCode)
	}
	if resumed.ID != got.ID {
		t.Fatalf("replica resumed %s want %s", resumed.ID, got.ID)
	}

	req, _ = http.NewRequest(http.MethodPost, srvB.URL+"/v1/executions/"+got.ID+"/renew", nil)
	req.Header.Set("Authorization", "Bearer "+tokB)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replica renew %d", resp.StatusCode)
	}
}

func TestRestartNewProcessSameStore(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	tok := session(t, lab.Server, lab.Cfg, "alice", "agent-1")
	got := acquireJSON(t, lab.Server, tok)
	if got.StatusCode != http.StatusCreated {
		t.Fatalf("acquire %d %s", got.StatusCode, got.Raw)
	}

	store2, err := pgstore.Connect(context.Background(), lab.Cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store2.Close)
	cfg2 := lab.Cfg
	api2 := publicapi.New(cfg2, store2, lab.Signer, slog.New(slog.NewTextHandler(io.Discard, nil)), testlab.ArsenalProfile(t))
	t.Cleanup(api2.Close)

	e, err := store2.Get(context.Background(), cfg2.MerchantID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != got.ID {
		t.Fatalf("restarted process lost execution: %+v", e)
	}
	_, err = store2.Renew(context.Background(), cfg2.MerchantID, got.ID, e.SessionID, id.Request(), 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEmergencyDrainAndRevokeAll(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	srv, cfg := lab.Server, lab.Cfg
	tok1 := session(t, srv, cfg, "alice", "agent-1")
	tok2 := session(t, srv, cfg, "alice", "agent-2")
	a := acquireJSON(t, srv, tok1)
	if a.StatusCode != http.StatusCreated {
		t.Fatalf("acquire %d %s", a.StatusCode, a.Raw)
	}
	_ = tok2

	req, _ := http.NewRequest(http.MethodPost, lab.Admin.URL+"/v1/admin/controls/revoke-all", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Revoked int `json:"revoked"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || out.Revoked < 1 {
		t.Fatalf("revoke-all %d %+v", resp.StatusCode, out)
	}

	req, _ = http.NewRequest(http.MethodPost, lab.Admin.URL+"/v1/admin/controls/drain", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("drain %d", resp.StatusCode)
	}

	again := acquireJSON(t, srv, tok1)
	if again.StatusCode != http.StatusCreated {
		t.Fatalf("after revoke-all should grant: %d %s", again.StatusCode, again.Raw)
	}
}

func TestHTTPConcurrentAcquireRelease(t *testing.T) {
	srv, cfg := startServer(t)
	holder := session(t, srv, cfg, "alice", "holder")
	got := acquireJSON(t, srv, holder)
	if got.StatusCode != http.StatusCreated {
		t.Fatalf("seed %d %s", got.StatusCode, got.Raw)
	}
	const n = 40
	var created atomic.Int64
	var wg sync.WaitGroup
	wg.Add(n + 1)
	go func() {
		defer wg.Done()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/"+got.ID+"/release", nil)
		req.Header.Set("Authorization", "Bearer "+holder)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			tok := session(t, srv, cfg, "alice", "ag-"+id.New("x"))
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/acquire", bytes.NewBufferString(`{"resource":"event:ars-che","action":"purchase"}`))
			req.Header.Set("Authorization", "Bearer "+tok)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			if resp.StatusCode == http.StatusCreated {
				created.Add(1)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
		_ = i
	}
	wg.Wait()
	if created.Load() > 1 {
		t.Fatalf("created=%d concurrent release/acquire leaked actives", created.Load())
	}
}

func TestMetricsResumeDoesNotCountForwarded(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	srv, cfg := lab.Server, lab.Cfg
	cookie, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "metrics-alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func() {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/authorize", bytes.NewBufferString(`{"method":"POST","path":"/api/events/ars-che/holds"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Bruiser-Edge-Secret", cfg.EdgeSecret)
		req.Header.Set("Cookie", "boxoffice_session="+cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	authorize()
	authorize()
	req, _ := http.NewRequest(http.MethodGet, lab.Admin.URL+"/v1/admin/status", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&st)
	resp.Body.Close()
	eaf, _ := st["eaf"].(map[string]any)
	if eaf["forwarded"].(float64) != 1 {
		t.Fatalf("resume must not increment forwarded: %+v", eaf)
	}
	if eaf["attempts"].(float64) < 2 {
		t.Fatalf("both requests must be observed: %+v", eaf)
	}
}

func TestOneHundredPercentEnforcesSecondAgent(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), func(c *config.Config) {
		c.EnforcePercent = 100
		c.Mode = ops.ModeEnforce
	})
	srv, cfg := lab.Server, lab.Cfg
	body := `{"enforce_percent":100,"enforcement":true,"mode":"enforce","updated_by":"test"}`
	req, _ := http.NewRequest(http.MethodPut, lab.Admin.URL+"/v1/admin/controls", bytes.NewBufferString(body))
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	authorize := func(cust string) (int, string) {
		cookie, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, cust, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/authorize", bytes.NewBufferString(`{"method":"POST","path":"/api/events/ars-che/holds"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Bruiser-Edge-Secret", cfg.EdgeSecret)
		req.Header.Set("Cookie", "boxoffice_session="+cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, resp.Header.Get("X-Bruiser-Enforced")
	}
	code, enf := authorize("full-alice")
	if code != 200 || enf != "1" {
		t.Fatalf("100%% first %d enf=%s", code, enf)
	}
	code, enf = authorize("full-alice")
	if enf != "1" {
		t.Fatalf("100%% second must stay enforced, enf=%s", enf)
	}
	if code == 200 {
		t.Fatalf("100%% second agent must not ALLOW, got %d", code)
	}
}

func TestAdminLoginUsesCookieNotQuery(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	srv, cfg := lab.Admin, lab.Cfg
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/admin", bytes.NewBufferString("secret="+cfg.AdminSecret))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login redirect %d", resp.StatusCode)
	}
	var cookie string
	for _, c := range resp.Cookies() {
		if c.Name == "bruiser_admin" {
			cookie = c.Value
		}
	}
	if cookie == "" || cookie == cfg.AdminSecret {
		t.Fatal("login must set a signed HttpOnly admin cookie, not the raw secret")
	}
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/admin", nil)
	req.AddCookie(&http.Cookie{Name: "bruiser_admin", Value: cookie})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cookie session %d", resp.StatusCode)
	}
}
