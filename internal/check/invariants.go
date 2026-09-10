package check

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/resource"
	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
)

type runner struct {
	cfg     Config
	client  *http.Client
	origin  string
	control string
	admin   string
}

func (r *runner) originSecretHdr(extra map[string]string) map[string]string {
	h := map[string]string{"X-Bruiser-Origin-Secret": r.cfg.OriginSecret}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

func (r *runner) forgedOrigin() Probe {
	if r.origin == "" {
		return fail("--origin is required")
	}
	if strings.TrimSpace(r.cfg.OriginSecret) == "" {
		return inconclusive("origin secret not provided; refusing to PASS a forged-token probe that cannot send it")
	}
	holdPath := "/api/events/" + r.cfg.EventID + "/holds"
	ex := capturePOST(r.client, r.origin+holdPath, r.originSecretHdr(map[string]string{
		"X-Bruiser-Execution": "forged.not.signed",
		"X-Bruiser-Fence":     "1",
	}), map[string]int{"seats": 1})
	p := Probe{Evidence: &Evidence{Exchanges: []Exchange{ex}, Sent: []string{"forged.not.signed", "X-Bruiser-Origin-Secret"}}}
	if ex.Status >= 200 && ex.Status < 300 {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("origin accepted forged execution with a valid origin secret (%d) %s", ex.Status, ex.Response)
		return p
	}
	if ex.Status != http.StatusForbidden {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("forged execution + origin secret want 403 got %d %s", ex.Status, ex.Response)
		return p
	}
	p.Status, p.Pass, p.Detail = "PASS", true, fmt.Sprintf("forged execution + valid origin secret rejected (%d)", ex.Status)
	return p
}

func (r *runner) staleFenceOrigin() Probe {
	if r.origin == "" || r.control == "" {
		return fail("--origin and --control are required")
	}
	if strings.TrimSpace(r.cfg.OriginSecret) == "" {
		return inconclusive("origin secret not provided; refusing to PASS a stale-fence probe that cannot send it")
	}
	cust := fmt.Sprintf("stale-%d", time.Now().UnixNano())
	sess, err := r.mintAgentSession(cust, "stale-agent")
	if err != nil {
		return inconclusive("cannot mint a session to produce a stale-fence token: %s", err.Error())
	}
	res := "event:" + r.cfg.EventID
	first, err := r.acquire(sess, res, cust+"-1")
	if err != nil {
		return fail("first acquire: %s", err.Error())
	}
	holdPath := "/api/events/" + r.cfg.EventID + "/holds"
	firstHit := capturePOST(r.client, r.origin+holdPath, r.originSecretHdr(map[string]string{
		"X-Bruiser-Execution": first.Token,
		"X-Bruiser-Fence":     first.Fence,
	}), map[string]int{"seats": 1})
	if firstHit.Status >= 400 && firstHit.Status != http.StatusConflict {
		return fail("could not present the live token to origin (%d) %s", firstHit.Status, firstHit.Response)
	}
	if err := r.release(sess, first.ID); err != nil {
		return fail("release: %s", err.Error())
	}
	second, err := r.acquire(sess, res, cust+"-2")
	if err != nil {
		return fail("second acquire: %s", err.Error())
	}
	f1, _ := strconv.ParseInt(first.Fence, 10, 64)
	f2, _ := strconv.ParseInt(second.Fence, 10, 64)
	if f2 <= f1 {
		return fail("fence did not advance (%s → %s); cannot prove a stale-fence token", first.Fence, second.Fence)
	}
	secondHit := capturePOST(r.client, r.origin+holdPath, r.originSecretHdr(map[string]string{
		"X-Bruiser-Execution": second.Token,
		"X-Bruiser-Fence":     second.Fence,
	}), map[string]int{"seats": 1})
	stale := capturePOST(r.client, r.origin+holdPath, r.originSecretHdr(map[string]string{
		"X-Bruiser-Execution": first.Token,
		"X-Bruiser-Fence":     first.Fence,
	}), map[string]int{"seats": 1})
	ev := &Evidence{
		Exchanges: []Exchange{firstHit, secondHit, stale},
		Sent:      []string{"stale-fence-token", "X-Bruiser-Origin-Secret", first.Fence, second.Fence},
	}
	p := Probe{Evidence: ev}
	if stale.Status >= 200 && stale.Status < 300 {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("origin accepted a stale-fence token with a valid origin secret (%d) %s", stale.Status, stale.Response)
		return p
	}
	if stale.Status != http.StatusForbidden {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("stale-fence token + origin secret want 403 got %d %s", stale.Status, stale.Response)
		return p
	}
	p.Status, p.Pass, p.Detail = "PASS", true, fmt.Sprintf("stale-fence token (fence %s after %s) + origin secret rejected (%d)", first.Fence, second.Fence, stale.Status)
	_ = r.release(sess, second.ID)
	return p
}

func (r *runner) resourceVariants() Probe {
	if r.control == "" {
		return fail("--control is required to prove canonical resource domains")
	}
	cust := fmt.Sprintf("canon-%d", time.Now().UnixNano())
	variants := resource.CupfinalCorpus()
	var exchanges []Exchange
	ids := map[string]struct{}{}
	created := 0
	for i, res := range variants {
		sess, err := r.mintAgentSession(cust, fmt.Sprintf("v-%d", i))
		if err != nil {
			return inconclusive("cannot mint a session for the nine-variant corpus: %s", err.Error())
		}
		ex := capturePOST(r.client, r.control+"/v1/executions/acquire", map[string]string{
			"Authorization": "Bearer " + sess,
		}, map[string]string{"resource": res, "action": "hold"})
		exchanges = append(exchanges, ex)
		if ex.Status == http.StatusBadRequest {
			return fail("variant %q rejected instead of folding (%d) %s", res, ex.Status, ex.Response)
		}
		var body map[string]any
		_ = json.Unmarshal([]byte(ex.Response), &body)
		id, _ := body["execution_id"].(string)
		switch ex.Status {
		case http.StatusCreated:
			created++
			if id != "" {
				ids[id] = struct{}{}
			}
		case http.StatusOK, http.StatusConflict, http.StatusForbidden:
			if id != "" {
				ids[id] = struct{}{}
			}
		default:
			return fail("variant %q → %d %s", res, ex.Status, ex.Response)
		}
	}
	p := Probe{Evidence: &Evidence{Exchanges: exchanges, Sent: append([]string{"max_active=1"}, variants...)}}
	if created != 1 || len(ids) != 1 {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("nine-variant corpus minted %d CREATED and %d distinct executions (want 1 ACTIVE domain under max_active=1)", created, len(ids))
		return p
	}
	p.Status, p.Pass, p.Detail = "PASS", true, "nine-variant + Unicode corpus stayed on one ACTIVE domain"
	return p
}

func (r *runner) identityForgeryDevAssertionsOff() Probe {
	if r.control == "" {
		return fail("--control is required")
	}
	forged := capturePOST(r.client, r.control+"/v1/sessions", map[string]string{
		"Authorization": "Bearer not-a-jwt",
	}, map[string]string{"principal": "forged"})
	// Also send a correctly signed HMAC assertion. Production with
	// BRUISER_DEV_ASSERTIONS off must reject it. Lab HMAC mode cannot
	// honestly PASS this probe.
	var hmacEx Exchange
	hmacSent := false
	hmac := strings.TrimSpace(r.cfg.HMACSecret)
	if hmac == "" {
		hmac = "authority-check-client-only-hmac"
	}
	tok, err := auth.IssueDevAssertion(hmac, "forged-alice", time.Hour, nil)
	if err == nil {
		hmacEx = capturePOST(r.client, r.control+"/v1/sessions", map[string]string{
			"Authorization": "Bearer " + tok,
		}, map[string]any{"principal": map[string]string{"type": "agent", "id": "hmac-probe"}})
		hmacSent = true
	}
	exchanges := []Exchange{forged}
	sent := []string{"not-a-jwt"}
	if hmacSent {
		exchanges = append(exchanges, hmacEx)
		sent = append(sent, "hmac-dev-assertion")
	}
	p := Probe{Evidence: &Evidence{Exchanges: exchanges, Sent: sent}}
	if forged.Status != http.StatusUnauthorized {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("forged assertion want 401 got %d %s", forged.Status, forged.Response)
		return p
	}
	if hmacSent && hmacEx.Status >= 200 && hmacEx.Status < 300 {
		p.Status, p.Pass, p.Detail = "INCONCLUSIVE", false, "HMAC lab assertions are still accepted; not proving production identity (dev assertions off)"
		return p
	}
	if !hmacSent {
		p.Status, p.Pass, p.Detail = "INCONCLUSIVE", false, "HMAC secret not provided; forged assertion was 401 but production HMAC-off was not exercised"
		return p
	}
	p.Status, p.Pass, p.Detail = "PASS", true, "forged assertion and HMAC lab assertion both rejected"
	return p
}

func (r *runner) sessionRevocation() Probe {
	if r.control == "" {
		return fail("--control is required")
	}
	cust := fmt.Sprintf("rev-%d", time.Now().UnixNano())
	sess, err := r.mintAgentSession(cust, "revoke-agent")
	if err != nil {
		return inconclusive("cannot mint a session to prove revocation: %s", err.Error())
	}
	logout := capturePOST(r.client, r.control+"/v1/sessions/logout", map[string]string{
		"Authorization": "Bearer " + sess,
	}, nil)
	after := capturePOST(r.client, r.control+"/v1/executions/acquire", map[string]string{
		"Authorization": "Bearer " + sess,
	}, map[string]string{"resource": "event:" + r.cfg.EventID, "action": "hold"})
	p := Probe{Evidence: &Evidence{
		Exchanges: []Exchange{logout, after},
		Sent:      []string{"revoked-session", "/v1/sessions/logout"},
	}}
	if logout.Status != http.StatusOK {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("logout %d %s", logout.Status, logout.Response)
		return p
	}
	if after.Status != http.StatusUnauthorized {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("revoked session still acquired (%d) %s", after.Status, after.Response)
		return p
	}
	p.Status, p.Pass, p.Detail = "PASS", true, "logout then acquire → 401"
	return p
}

func (r *runner) replayRejected() Probe {
	if r.control == "" {
		return fail("--control is required")
	}
	cust := fmt.Sprintf("replay-%d", time.Now().UnixNano())
	sess, err := r.mintAgentSession(cust, "replay-agent")
	if err != nil {
		return inconclusive("cannot mint a session to prove replay rejection: %s", err.Error())
	}
	nonce := fmt.Sprintf("authority-check-nonce-%d", time.Now().UnixNano())
	hdr := map[string]string{
		"Authorization":   "Bearer " + sess,
		"X-Bruiser-Nonce": nonce,
	}
	body := map[string]string{"resource": "event:" + r.cfg.EventID, "action": "hold"}
	first := capturePOST(r.client, r.control+"/v1/executions/acquire", hdr, body)
	second := capturePOST(r.client, r.control+"/v1/executions/acquire", hdr, body)
	p := Probe{Evidence: &Evidence{
		Exchanges: []Exchange{first, second},
		Sent:      []string{"authority-check-nonce", nonce},
	}}
	if first.Status != http.StatusCreated && first.Status != http.StatusOK {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("first acquire %d %s", first.Status, first.Response)
		return p
	}
	if second.Status != http.StatusConflict {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("replay want 409 got %d %s", second.Status, second.Response)
		return p
	}
	p.Status, p.Pass, p.Detail = "PASS", true, "duplicate X-Bruiser-Nonce → 409"
	return p
}

func (r *runner) budgetEnforced() Probe {
	if r.control == "" {
		return fail("--control is required")
	}
	cust := fmt.Sprintf("budget-%d", time.Now().UnixNano())
	hdr, err := r.cfg.customerHeaders(cust)
	if err != nil {
		return fail("%s", err.Error())
	}
	authzHdr := hdr
	if authzHdr == nil {
		authzHdr = map[string]string{}
	}
	authzHdr["X-Bruiser-Edge-Secret"] = r.cfg.EdgeSecret
	path := "/api/events/" + r.cfg.EventID + "/holds"
	first := capturePOST(r.client, r.control+"/v1/authorize", authzHdr, map[string]string{"method": "POST", "path": path})
	second := capturePOST(r.client, r.control+"/v1/authorize", authzHdr, map[string]string{"method": "POST", "path": path})
	p := Probe{Evidence: &Evidence{
		Exchanges: []Exchange{first, second},
		Sent:      []string{"budget", "max_ops"},
	}}
	if first.Status != http.StatusOK {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("first authorize %d %s", first.Status, first.Response)
		return p
	}
	if second.Status != http.StatusConflict {
		// Unlimited budget cannot honestly PASS.
		p.Status, p.Pass, p.Detail = "INCONCLUSIVE", false, fmt.Sprintf("second authorize %d %s; budget.max_ops may be unlimited — not claiming enforcement", second.Status, second.Response)
		return p
	}
	p.Status, p.Pass, p.Detail = "PASS", true, "second authorize exhausted budget → 409"
	return p
}

func (r *runner) adminNotOnPublic() Probe {
	if r.control == "" {
		return fail("--control is required")
	}
	pub := captureGET(r.client, r.control+"/v1/admin/status", nil)
	var adminEx Exchange
	sent := []string{"/v1/admin/status"}
	if r.admin != "" && r.admin != r.control {
		adminEx = captureGET(r.client, r.admin+"/v1/admin/status", r.adminHeaders())
		sent = append(sent, r.admin+"/v1/admin/status")
	}
	exchanges := []Exchange{pub}
	if adminEx.URL != "" {
		exchanges = append(exchanges, adminEx)
	}
	p := Probe{Evidence: &Evidence{Exchanges: exchanges, Sent: sent}}
	if pub.Status != http.StatusNotFound {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("public listener served admin status (%d) %s", pub.Status, pub.Response)
		return p
	}
	if r.admin == "" || r.admin == r.control {
		p.Status, p.Pass, p.Detail = "INCONCLUSIVE", false, "public path 404'd but --admin was not a distinct listener, so the admin surface was not proven to exist elsewhere"
		return p
	}
	if adminEx.Status == http.StatusNotFound {
		p.Status, p.Pass, p.Detail = "FAIL", false, "admin listener also 404'd /v1/admin/status"
		return p
	}
	if adminEx.Status != http.StatusOK && adminEx.Status != http.StatusUnauthorized {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("admin listener status %d %s", adminEx.Status, adminEx.Response)
		return p
	}
	p.Status, p.Pass, p.Detail = "PASS", true, fmt.Sprintf("public 404; admin listener %d", adminEx.Status)
	return p
}

func (r *runner) failClosedStoreOutage() Probe {
	if r.cfg.StoreDownURL == "" {
		return Probe{
			Status:   "INCONCLUSIVE",
			Detail:   "no --store-down URL; refusing to PASS fail-closed without taking the store down",
			Evidence: &Evidence{Sent: []string{}},
		}
	}
	down := strings.TrimRight(r.cfg.StoreDownURL, "/")
	edgeSec := r.cfg.StoreDownEdgeSecret
	if edgeSec == "" {
		edgeSec = r.cfg.EdgeSecret
	}
	var hdr map[string]string
	if strings.TrimSpace(r.cfg.IdentityToken) != "" {
		var err error
		hdr, err = r.cfg.customerHeaders(fmt.Sprintf("down-%d", time.Now().UnixNano()))
		if err != nil {
			return fail("%s", err.Error())
		}
	} else {
		hmac := r.cfg.StoreDownHMAC
		if hmac == "" {
			hmac = r.cfg.HMACSecret
		}
		cookie, err := auth.IssueBoxOfficeSession(hmac, fmt.Sprintf("down-%d", time.Now().UnixNano()), time.Hour)
		if err != nil {
			return fail("%s", err.Error())
		}
		hdr = map[string]string{"Cookie": simtix.CookieName + "=" + cookie}
	}
	if hdr == nil {
		hdr = map[string]string{}
	}
	hdr["X-Bruiser-Edge-Secret"] = edgeSec
	ex := capturePOST(r.client, down+"/v1/authorize", hdr, map[string]string{"method": "POST", "path": "/api/events/" + r.cfg.EventID + "/holds"})
	p := Probe{Evidence: &Evidence{Exchanges: []Exchange{ex}, Sent: []string{"store-down", down}}}
	if ex.Status == http.StatusOK || ex.Status == http.StatusCreated {
		p.Status, p.Pass, p.Detail = "FAIL", false, fmt.Sprintf("store-down authorize fail-opened (%d) %s", ex.Status, ex.Response)
		return p
	}
	p.Status, p.Pass, p.Detail = "PASS", true, fmt.Sprintf("store-down authorize denied (%d)", ex.Status)
	return p
}

func (r *runner) productionSecrets() Probe {
	sent := []string{"validate-secrets"}
	probeCfg := config.Config{
		Environment:    "production",
		AdminSecret:    r.cfg.AdminSecret,
		OperatorSecret: r.cfg.OperatorSecret,
		EdgeSecret:     r.cfg.EdgeSecret,
		OriginSecret:   r.cfg.OriginSecret,
		DevHMACSecret:  r.cfg.HMACSecret,
		AdminAddr:      "127.0.0.1:8082",
	}
	err := probeCfg.ValidateSecrets()
	detail := "ValidateSecrets on provided credentials"
	if err != nil {
		detail = err.Error()
	}
	ex := Exchange{
		Method:   "LOCAL",
		URL:      "config.ValidateSecrets",
		Body:     "production",
		Response: detail,
	}
	if err == nil {
		ex.Status = 200
	} else {
		ex.Status = 400
	}
	p := Probe{Evidence: &Evidence{Exchanges: []Exchange{ex}, Sent: sent}}
	if !r.cfg.detectedProduction {
		p.Status, p.Pass, p.Detail = "INCONCLUSIVE", false, "target is not production-configured; not claiming production secret validation"
		return p
	}
	if err != nil {
		p.Status, p.Pass, p.Detail = "FAIL", false, detail
		return p
	}
	p.Status, p.Pass, p.Detail = "PASS", true, "provided secrets pass production ValidateSecrets"
	return p
}

func (r *runner) detectProduction() bool {
	if r.cfg.Production {
		return true
	}
	if r.admin == "" {
		return false
	}
	ex := captureGET(r.client, r.admin+"/v1/admin/status", r.adminHeaders())
	if ex.Status != http.StatusOK {
		return false
	}
	var body map[string]any
	if json.Unmarshal([]byte(ex.Response), &body) != nil {
		return false
	}
	if v, ok := body["production"].(bool); ok {
		return v
	}
	return false
}

func (r *runner) adminHeaders() map[string]string {
	if r.cfg.AdminSecret != "" {
		return map[string]string{"X-Bruiser-Admin-Secret": r.cfg.AdminSecret}
	}
	if r.cfg.OperatorSecret != "" {
		return map[string]string{"X-Bruiser-Operator-Secret": r.cfg.OperatorSecret}
	}
	return nil
}

func (r *runner) mintAgentSession(customer, principal string) (string, error) {
	assertion := strings.TrimSpace(r.cfg.IdentityToken)
	if assertion == "" {
		if strings.TrimSpace(r.cfg.HMACSecret) == "" {
			return "", fmt.Errorf("HMAC secret not provided")
		}
		var err error
		assertion, err = auth.IssueDevAssertion(r.cfg.HMACSecret, customer, time.Hour, nil)
		if err != nil {
			return "", err
		}
	}
	ex := capturePOST(r.client, r.control+"/v1/sessions", map[string]string{
		"Authorization": "Bearer " + assertion,
	}, map[string]any{"principal": map[string]string{"type": "agent", "id": principal}})
	if ex.Status != http.StatusOK && ex.Status != http.StatusCreated {
		return "", fmt.Errorf("sessions %d %s", ex.Status, ex.Response)
	}
	// 201 is the production response; 200 is tolerated.
	var out struct {
		Token string `json:"session_token"`
	}
	if err := json.Unmarshal([]byte(ex.Response), &out); err != nil || out.Token == "" {
		return "", fmt.Errorf("session token missing: %s", ex.Response)
	}
	return out.Token, nil
}

func (r *runner) acquire(sess, resourceID, _ string) (mintedExecution, error) {
	ex := capturePOST(r.client, r.control+"/v1/executions/acquire", map[string]string{
		"Authorization": "Bearer " + sess,
	}, map[string]string{"resource": resourceID, "action": "hold"})
	if ex.Status != http.StatusCreated && ex.Status != http.StatusOK {
		return mintedExecution{}, fmt.Errorf("acquire %d %s", ex.Status, ex.Response)
	}
	var out struct {
		ExecutionID string `json:"execution_id"`
		Fence       int64  `json:"fence"`
		Token       string `json:"execution_token"`
	}
	_ = json.Unmarshal([]byte(ex.Response), &out)
	exe := mintedExecution{Token: out.Token, ID: out.ExecutionID, Fence: fmt.Sprintf("%d", out.Fence)}
	if exe.Token == "" {
		return mintedExecution{}, fmt.Errorf("acquire returned no token: %s", ex.Response)
	}
	return exe, nil
}

func (r *runner) release(sess, id string) error {
	if id == "" {
		return fmt.Errorf("missing execution id")
	}
	ex := capturePOST(r.client, r.control+"/v1/executions/"+id+"/release", map[string]string{
		"Authorization": "Bearer " + sess,
	}, nil)
	if ex.Status >= 400 {
		return fmt.Errorf("release %d %s", ex.Status, ex.Response)
	}
	return nil
}

func inconclusive(format string, args ...any) Probe {
	return Probe{Status: "INCONCLUSIVE", Pass: false, Detail: fmt.Sprintf(format, args...)}
}
