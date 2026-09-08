package publicapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
)

func TestPolicyNotifyReloadsOtherNode(t *testing.T) {
	_, srvA, apiB, srvB, cfg := testlab.GatewayPair(t, testlab.ArsenalProfile(t))
	a1 := session(t, srvB, cfg, "alice", "agent-1")
	a2 := session(t, srvB, cfg, "alice", "agent-2")

	first := acquireJSON(t, srvB, a1)
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first %d %s", first.StatusCode, first.Raw)
	}
	second := acquireJSON(t, srvB, a2)
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("second want 409 got %d %s", second.StatusCode, second.Raw)
	}

	putPolicy(t, srvA.URL, cfg.AdminSecret, policyMax2)

	deadline := time.Now().Add(3 * time.Second)
	var again exeBody
	for time.Now().Before(deadline) {
		if apiB.PolicyVersion() >= 1 {
			again = acquireJSON(t, srvB, a2)
			if again.StatusCode == http.StatusCreated {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if again.StatusCode != http.StatusCreated {
		t.Fatalf("node B after NOTIFY want 201 got %d %s version=%d", again.StatusCode, again.Raw, apiB.PolicyVersion())
	}
}
