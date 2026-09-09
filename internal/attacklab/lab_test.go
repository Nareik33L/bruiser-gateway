package attacklab

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
)

func TestRejectsTargetURL(t *testing.T) {
	lab, err := New(Config{HMACSecret: "s", OriginSecret: "lock"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(lab.API())
	t.Cleanup(srv.Close)

	for _, body := range []string{
		`{"name":"x","target_url":"https://example.com"}`,
		`{"name":"https://evil.test","product":"Shirt"}`,
		`{"origin":"http://127.0.0.1"}`,
		`{"webhook":"https://hooks.example/x"}`,
	} {
		resp, err := http.Post(srv.URL+"/stores", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %s: want 400 got %d %s", body, resp.StatusCode, b)
		}
		if !strings.Contains(string(b), "sandbox") {
			t.Fatalf("message: %s", b)
		}
	}
}

func TestCreateStoreDefaultsAndSwarmBounds(t *testing.T) {
	lab, err := New(Config{HMACSecret: "s", OriginSecret: "lock"})
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(lab.API())
	t.Cleanup(api.Close)

	resp, err := http.Post(api.URL+"/stores", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create %d %s", resp.StatusCode, b)
	}
	var st map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st["name"] != "Bruiser FC" || st["product"] != "Limited Edition Shirt" || st["price"] != "£50.00" {
		t.Fatalf("defaults %+v", st)
	}
	if st["mode"] != "dry-run" {
		t.Fatalf("mode %v", st["mode"])
	}
	id, _ := st["id"].(string)
	if id == "" {
		t.Fatal("missing id")
	}

	resp, err = http.Post(api.URL+"/stores/"+id+"/attacks", "application/json", strings.NewReader(`{"swarm_size":101}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("max swarm want 400 got %d", resp.StatusCode)
	}

	resp, err = http.Post(api.URL+"/stores/"+id+"/attacks", "application/json", strings.NewReader(`{"target":"https://shop.example"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("attack url want 400 got %d", resp.StatusCode)
	}
}

func TestComposeProfilesDiffer(t *testing.T) {
	agents := compose(50, "str_abcdefghij")
	if len(agents) != 50 {
		t.Fatalf("len %d", len(agents))
	}
	kinds := map[string]int{}
	customers := map[string]int{}
	for _, a := range agents {
		kinds[a.Kind]++
		customers[a.CustomerID]++
		if a.Checkouts < 1 {
			t.Fatalf("agent %+v has no checkouts", a)
		}
	}
	for _, k := range []string{"normal", "mobile", "rapid", "multi", "duplicate"} {
		if kinds[k] == 0 {
			t.Fatalf("missing kind %s: %v", k, kinds)
		}
	}
	if kinds["normal"] < kinds["rapid"] {
		t.Fatalf("want more fans than rapid bots: %v", kinds)
	}
	shared := 0
	for _, n := range customers {
		if n > 1 {
			shared++
		}
	}
	if shared == 0 {
		t.Fatal("expected shared identities among bot clusters")
	}
}

func TestSimulatorRefusesExternalHost(t *testing.T) {
	rt := labTransport{h: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	_, err := rt.RoundTrip(req)
	if err == nil || !strings.Contains(err.Error(), "non-sandbox") {
		t.Fatalf("want refuse, got %v", err)
	}
}

func TestDryRunForwardsAndEnforceRejects(t *testing.T) {
	gw, cfg := testlab.Gateway(t, withLabProfile(t))
	lab, err := New(Config{
		HMACSecret:   cfg.DevHMACSecret,
		EdgeSecret:   cfg.EdgeSecret,
		OriginSecret: cfg.OriginSecret,
		BruiserURL:   gw.URL,
		StoreTTL:     time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(lab.API())
	t.Cleanup(api.Close)

	resp, err := http.Post(api.URL+"/stores", "application/json", bytes.NewBufferString(`{"name":"Bruiser FC"}`))
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	dry := runAttack(t, api.URL, st.ID, 12, false)
	if dry.Summary == nil {
		t.Fatal("missing dry-run summary")
	}
	if dry.Summary.WouldBeBlocked < 1 {
		t.Fatalf("dry-run should flag bots: %+v", dry.Summary)
	}
	if dry.Summary.Blocked != 0 {
		t.Fatalf("dry-run must not block: %+v", dry.Summary)
	}
	if dry.Summary.OriginOrders < 1 {
		t.Fatalf("dry-run origin should receive checkouts: %+v", dry.Summary)
	}
	if dry.Summary.OriginOrders < dry.Summary.WouldBeBlocked {
		t.Fatalf("dry-run should still let suspicious checkouts through: %+v", dry.Summary)
	}

	req, _ := http.NewRequest(http.MethodPost, api.URL+"/stores/"+st.ID+"/mode", strings.NewReader(`{"mode":"enforce"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("mode %d", resp.StatusCode)
	}

	enf := runAttack(t, api.URL, st.ID, 12, true)
	if enf.Summary == nil {
		t.Fatal("missing enforce summary")
	}
	if enf.Summary.Blocked < 1 {
		t.Fatalf("enforce should reject: %+v", enf.Summary)
	}
	if enf.Summary.OriginOrders >= dry.Summary.OriginOrders {
		t.Fatalf("enforce origin orders %d should be below dry-run origin %d", enf.Summary.OriginOrders, dry.Summary.OriginOrders)
	}
}

func runAttack(t *testing.T, base, storeID string, size int, replay bool) attackView {
	t.Helper()
	body := map[string]any{"swarm_size": size, "replay": replay}
	b, _ := json.Marshal(body)
	resp, err := http.Post(base+"/stores/"+storeID+"/attacks", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start attack %d %s", resp.StatusCode, raw)
	}
	var started struct {
		ID string `json:"attack_id"`
	}
	if err := json.Unmarshal(raw, &started); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get(base + "/stores/" + storeID + "/attacks/" + started.ID)
		if err != nil {
			t.Fatal(err)
		}
		var view attackView
		if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
			resp.Body.Close()
			t.Fatal(err)
		}
		resp.Body.Close()
		if view.Done {
			return view
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatal("attack timeout")
	return attackView{}
}

type attackView struct {
	Done    bool     `json:"done"`
	Summary *Summary `json:"summary"`
	Metrics Metrics  `json:"metrics"`
}

func withLabProfile(t *testing.T) merchant.Profile {
	t.Helper()
	p := testlab.ArsenalProfile(t)
	p.Routes = append(p.Routes, Routes()...)
	return p
}
