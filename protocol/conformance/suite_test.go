package conformance

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIDeclaresQueueAndAcquire(t *testing.T) {
	raw, err := os.ReadFile("../openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	paths, _ := doc["paths"].(map[string]any)
	if _, ok := paths["/v1/executions/acquire"]; !ok {
		t.Fatal("openapi missing acquire")
	}
	text := string(raw)
	if !strings.Contains(text, "QUEUED") && !strings.Contains(text, "202") {
		t.Fatal("openapi should document QUEUED / 202")
	}
}

func TestLiveProtocolCapabilities(t *testing.T) {
	base := os.Getenv("BRUISER_CONFORMANCE_URL")
	if base == "" {
		t.Skip("BRUISER_CONFORMANCE_URL not set")
	}
	resp, err := http.Get(strings.TrimRight(base, "/") + "/.well-known/bruiser/protocol")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	need := map[string]bool{"ACQUIRE": false, "RENEW": false, "QUEUE": false, "AUTHORIZE": false}
	for _, c := range body.Capabilities {
		if _, ok := need[c]; ok {
			need[c] = true
		}
	}
	for k, ok := range need {
		if !ok {
			t.Fatalf("missing capability %s", k)
		}
	}
}
