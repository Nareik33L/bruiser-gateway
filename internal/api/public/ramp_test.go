package publicapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/ops"
	"github.com/Nareik33L/bruiser-gateway/internal/policy"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
	"gopkg.in/yaml.v3"
)

func TestProgressiveRampDeterministicAndScoped(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	srv, cfg := lab.Server, lab.Cfg
	doc := policy.DefaultDocument(cfg.MerchantID)
	doc.Domains[0].Waiting = policy.Waiting{Mode: "bounded", MaxWaiters: 1}
	raw, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPut, lab.Admin.URL+"/v1/policy", bytes.NewReader(raw))
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	req.Header.Set("Content-Type", "application/yaml")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	put := func(body string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPut, lab.Admin.URL+"/v1/admin/controls", bytes.NewBufferString(body))
		req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("put controls %d %s", resp.StatusCode, b)
		}
	}

	authorize := func(customer string) (int, string, map[string]any) {
		t.Helper()
		cookie, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, customer, 0)
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
		var out map[string]any
		b, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(b, &out)
		return resp.StatusCode, resp.Header.Get("X-Bruiser-Enforced"), out
	}

	inCust, outCust := "ramp-in", "ramp-out"
	for i := 0; i < 400; i++ {
		id := "c" + string(rune('a'+i%26)) + string(rune('A'+i/26%26)) + string(rune('0'+i%10))
		if ops.InRamp(id, 10, "") {
			inCust = id
		} else {
			outCust = id
		}
	}
	if ops.InRamp(inCust, 10, "") == ops.InRamp(outCust, 10, "") {
		t.Fatal("need one in-bucket and one out-bucket customer")
	}

	put(`{"enforce_percent":10,"enforcement":true,"mode":"enforce","updated_by":"test"}`)

	code, enf, body := authorize(inCust)
	if code != http.StatusOK || enf != "1" || body["status"] != "ALLOW" {
		t.Fatalf("in-bucket should enforce: %d enf=%s %v", code, enf, body)
	}
	cookie2, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, inCust, 0)
	if err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/authorize", bytes.NewBufferString(`{"method":"POST","path":"/api/events/ars-che/holds"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Edge-Secret", cfg.EdgeSecret)
	req.Header.Set("Cookie", "boxoffice_session="+cookie2)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK && bytes.Contains(b, []byte(`"status":"ALLOW"`)) && resp.Header.Get("X-Bruiser-Enforced") == "0" {
		t.Fatal("in-bucket second agent must be enforced, not observed")
	}
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusConflict {
		t.Fatalf("in-bucket second agent want QUEUED/BUSY got %d %s", resp.StatusCode, b)
	}

	code, enf, body = authorize(outCust)
	if code != http.StatusOK || enf != "0" || body["status"] != "ALLOW" {
		t.Fatalf("out-bucket must observe only: %d enf=%s %v", code, enf, body)
	}
	if body["would"] == nil {
		t.Fatalf("out-bucket should record hypothetical: %v", body)
	}

	// Same out-bucket customer stays unenforced.
	_, enf2, _ := authorize(outCust)
	if enf2 != "0" {
		t.Fatal("customer must not flip buckets")
	}

	put(`{"scope":{"events":["other-event"]},"enforce_percent":100,"updated_by":"test"}`)
	code, enf, body = authorize(inCust)
	if code != http.StatusOK || enf != "0" {
		t.Fatalf("out-of-scope event should observe: %d enf=%s %v", code, enf, body)
	}

	put(`{"enforce_percent":0,"updated_by":"test"}`)
	code, enf, body = authorize(inCust)
	if code != http.StatusOK || enf != "0" || body["status"] != "ALLOW" {
		t.Fatalf("0%% is dry-run: %d enf=%s %v", code, enf, body)
	}

	req, _ = http.NewRequest(http.MethodGet, lab.Admin.URL+"/v1/admin/ramp", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var report map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if report["requests_observed"].(float64) < 1 || report["requests_evaluated"].(float64) < 1 {
		t.Fatalf("report %+v", report)
	}
}
