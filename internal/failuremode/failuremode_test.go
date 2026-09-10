package failuremode_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
	bruiser "github.com/Nareik33L/bruiser-gateway/sdk/go"
	fm "github.com/Nareik33L/bruiser-gateway/test/failuremode"
)

// Product decisions this suite encodes. "Fail closed" is only meaningful
// once each mode has an asserted allow vs deny.
//
//	store unavailable on acquire (default)     → deny
//	store unavailable + fail_closed=false      → allow (explicit)
//	origin/SDK cannot reach gateway, no token  → deny
//	origin/SDK cannot reach gateway, live tok  → allow (local verify)
//	duplicate Idempotency-Key / nonce          → exactly one grant
//	M concurrent acquires, max_active=k        → ACTIVE ≤ k
//	stale lease past TTL                       → new grant fences out stale
//	delayed older token after newer fence      → deny
//	clock skew beyond auth.Skew                → deny
//	restart mid-lease                          → same ACTIVE, not duplicated
//	cosmetic resource variants                 → one domain, ACTIVE ≤ 1

func TestStoreUnavailableDeniesGrant(t *testing.T) {
	h := fm.Start(t)
	tok := h.Session(t, "alice", "agent-1")
	h.Lab.Store.Close()
	got := h.Acquire(t, tok, "event:ars-che", "hold", nil)
	if got.FailOpen() || got.Status == http.StatusCreated {
		t.Fatalf("store down must deny grant, got %d %s", got.Status, got.Body)
	}
	if got.Status != http.StatusServiceUnavailable && got.Status != http.StatusInternalServerError {
		t.Fatalf("store down status %d %s", got.Status, got.Body)
	}
}

func TestStoreUnavailableFailOpenOnlyWhenConfigured(t *testing.T) {
	h := fm.Start(t)
	h.PutControls(t, `{"fail_closed":false,"updated_by":"failuremode"}`)
	tok := h.Session(t, "alice", "agent-1")
	h.Lab.Store.Close()
	got := h.Acquire(t, tok, "event:ars-che", "hold", nil)
	if !got.FailOpen() {
		t.Fatalf("explicit fail_closed=false must ALLOW fail-open, got %d %s", got.Status, got.Body)
	}
}

func TestStoreCrashMidAcquireDenies(t *testing.T) {
	h := fm.Start(t)
	h.PutPolicy(t, fm.PolicyMaxActive(1))
	holder := h.Session(t, "alice", "holder")
	first := h.Acquire(t, holder, "event:ars-che", "hold", nil)
	if first.Status != http.StatusCreated {
		t.Fatalf("setup grant %d %s", first.Status, first.Body)
	}
	const n = 8
	tokens := make([]string, n)
	for i := 0; i < n; i++ {
		tokens[i] = h.Session(t, "alice", fmt.Sprintf("waiter-%d", i))
	}
	held, err := h.Get(t, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	unlock := h.LockDomain(t, held.DomainKey)
	defer unlock()
	var wg sync.WaitGroup
	var opened, failOpen atomic.Int64
	wg.Add(n)
	for i := 0; i < n; i++ {
		tok := tokens[i]
		go func() {
			defer wg.Done()
			got := h.Acquire(t, tok, "event:ars-che", "hold", nil)
			if got.FailOpen() {
				failOpen.Add(1)
			}
			if got.Status == http.StatusCreated {
				opened.Add(1)
			}
		}()
	}
	time.Sleep(80 * time.Millisecond)
	h.Lab.Store.Close()
	wg.Wait()
	if failOpen.Load() != 0 {
		t.Fatalf("mid-acquire crash fail-opened %d grants", failOpen.Load())
	}
	if opened.Load() != 0 {
		t.Fatalf("mid-acquire crash minted %d extra grants", opened.Load())
	}
}

func TestPartitionSDKDeniesWithoutTokenAllowsHeldToken(t *testing.T) {
	h := fm.Start(t)
	tok := h.Session(t, "alice", "agent-1")
	grant := h.Acquire(t, tok, "event:ars-che", "hold", nil)
	if grant.Status != http.StatusCreated || grant.Token == "" {
		t.Fatalf("setup %d %s", grant.Status, grant.Body)
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	// Active errors simulate origin → gateway partition.
	hnd := bruiser.Protect(bruiser.ProtectConfig{
		Public:     h.Lab.Signer.Public,
		MerchantID: h.Lab.Cfg.MerchantID,
		Fences:     &bruiser.FenceCache{},
		Active: func(string, int64) (bool, error) {
			return false, errors.New("gateway unreachable")
		},
	})(inner)
	if code := fm.Hit(t, hnd, ""); code != http.StatusUnauthorized {
		t.Fatalf("no token want deny 401 got %d", code)
	}
	if code := fm.Hit(t, hnd, "forged.not.signed"); code != http.StatusForbidden {
		t.Fatalf("garbage want deny 403 got %d", code)
	}
	if code := fm.Hit(t, hnd, grant.Token); code != http.StatusCreated {
		t.Fatalf("held live token must allow on partition, got %d", code)
	}
	revoked := bruiser.Protect(bruiser.ProtectConfig{
		Public: h.Lab.Signer.Public,
		Active: func(string, int64) (bool, error) { return false, nil },
	})(inner)
	if code := fm.Hit(t, revoked, grant.Token); code != http.StatusForbidden {
		t.Fatalf("known revoke must deny, got %d", code)
	}
}

func TestDuplicateIdempotencyOneGrant(t *testing.T) {
	h := fm.Start(t)
	tok := h.Session(t, "alice", "agent-1")
	const n = 20
	var wg sync.WaitGroup
	var created, replayed atomic.Int64
	ids := make(chan string, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			got := h.Acquire(t, tok, "event:ars-che", "hold", http.Header{"Idempotency-Key": []string{"fm-idem-1"}})
			switch got.Status {
			case http.StatusCreated:
				created.Add(1)
				ids <- got.ID
			case http.StatusConflict:
				replayed.Add(1)
			default:
				t.Errorf("unexpected %d %s", got.Status, got.Body)
			}
		}()
	}
	wg.Wait()
	close(ids)
	if created.Load() != 1 {
		t.Fatalf("created=%d want 1", created.Load())
	}
	if replayed.Load() != n-1 {
		t.Fatalf("replayed=%d want %d", replayed.Load(), n-1)
	}
	if h.ActiveCount(t, "alice") != 1 {
		t.Fatalf("ACTIVE=%d want 1", h.ActiveCount(t, "alice"))
	}
}

func TestNonceReplayOneGrant(t *testing.T) {
	h := fm.Start(t)
	tok := h.Session(t, "alice", "agent-1")
	first := h.Acquire(t, tok, "event:ars-che", "hold", http.Header{"X-Bruiser-Nonce": []string{"nonce-1"}})
	if first.Status != http.StatusCreated {
		t.Fatalf("first %d %s", first.Status, first.Body)
	}
	second := h.Acquire(t, tok, "event:ars-che", "hold", http.Header{"X-Bruiser-Nonce": []string{"nonce-1"}})
	if second.Status != http.StatusConflict {
		t.Fatalf("replay want 409 got %d %s", second.Status, second.Body)
	}
	if err := h.Lab.Store.ClaimReplay(context.Background(), h.Lab.Cfg.MerchantID, "nonce:nonce-1", "acquire", time.Minute); !errors.Is(err, pgstore.ErrReplay) {
		t.Fatalf("ClaimReplay: %v", err)
	}
}

func TestSimultaneousGrantsNeverExceedK(t *testing.T) {
	h := fm.Start(t)
	const k, m = 3, 24
	h.PutPolicy(t, fm.PolicyMaxActive(k))
	tokens := make([]string, m)
	for i := 0; i < m; i++ {
		tokens[i] = h.Session(t, "alice", fmt.Sprintf("p-%d", i))
	}
	var wg sync.WaitGroup
	var created atomic.Int64
	wg.Add(m)
	for i := 0; i < m; i++ {
		tok := tokens[i]
		go func() {
			defer wg.Done()
			got := h.Acquire(t, tok, "event:ars-che", "hold", nil)
			if got.Status == http.StatusCreated {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	active := h.ActiveCount(t, "alice")
	if active > k {
		t.Fatalf("ACTIVE=%d exceeds max_active=%d (created=%d)", active, k, created.Load())
	}
	if created.Load() > int64(k) {
		t.Fatalf("created=%d exceeds k=%d", created.Load(), k)
	}
	if created.Load() != int64(k) || active != k {
		t.Fatalf("want exactly k=%d grants/ACTIVE, created=%d active=%d", k, created.Load(), active)
	}
}

func TestStaleLeaseFencedOut(t *testing.T) {
	h := fm.Start(t)
	h.PutControls(t, `{"lease_ttl_seconds":1,"updated_by":"failuremode"}`)
	tok := h.Session(t, "alice", "agent-1")
	stale := h.Acquire(t, tok, "event:ars-che", "hold", nil)
	if stale.Status != http.StatusCreated {
		t.Fatalf("setup %d %s", stale.Status, stale.Body)
	}
	time.Sleep(1200 * time.Millisecond)
	if n := h.ExpireDue(t); n < 1 {
		t.Fatalf("ExpireDue=%d want ≥1", n)
	}
	if h.ActiveCount(t, "alice") != 0 {
		t.Fatalf("stale still ACTIVE")
	}
	fresh := h.Acquire(t, tok, "event:ars-che", "hold", nil)
	if fresh.Status != http.StatusCreated || fresh.ID == stale.ID {
		t.Fatalf("new acquire should fence out stale: %d id=%s vs %s %s", fresh.Status, fresh.ID, stale.ID, fresh.Body)
	}
	if fresh.Fence <= stale.Fence {
		t.Fatalf("fence %d not greater than stale %d", fresh.Fence, stale.Fence)
	}
	e, err := h.Get(t, stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.State == lease.StateActive {
		t.Fatalf("stale execution still ACTIVE")
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	fences := &bruiser.FenceCache{}
	hnd := bruiser.Protect(bruiser.ProtectConfig{Public: h.Lab.Signer.Public, Fences: fences})(inner)
	if code := fm.Hit(t, hnd, fresh.Token); code != http.StatusCreated {
		t.Fatalf("fresh token %d", code)
	}
	if code := fm.Hit(t, hnd, stale.Token); code != http.StatusForbidden {
		t.Fatalf("stale token must be rejected downstream, got %d", code)
	}
}

func TestDelayedOlderTokenRejected(t *testing.T) {
	h := fm.Start(t)
	tok := h.Session(t, "alice", "agent-1")
	first := h.Acquire(t, tok, "event:ars-che", "hold", nil)
	if first.Status != http.StatusCreated {
		t.Fatalf("first %d %s", first.Status, first.Body)
	}
	req, _ := http.NewRequest(http.MethodPost, h.Lab.Server.URL+"/v1/executions/"+first.ID+"/release", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	second := h.Acquire(t, tok, "event:ars-che", "hold", nil)
	if second.Status != http.StatusCreated || second.Fence <= first.Fence {
		t.Fatalf("second %d fence=%d vs %d %s", second.Status, second.Fence, first.Fence, second.Body)
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	fences := &bruiser.FenceCache{}
	hnd := bruiser.Protect(bruiser.ProtectConfig{Public: h.Lab.Signer.Public, Fences: fences})(inner)
	if code := fm.Hit(t, hnd, second.Token); code != http.StatusCreated {
		t.Fatalf("newer token %d", code)
	}
	if code := fm.Hit(t, hnd, first.Token); code != http.StatusForbidden {
		t.Fatalf("delayed older token want deny, got %d", code)
	}
}

func TestClockSkewBeyondLeewayDenied(t *testing.T) {
	h := fm.Start(t)
	expired := signSkewed(t, h.Lab.Signer, -10*time.Second, 0, 0)
	if _, err := auth.ParseExecution(expired, h.Lab.Signer.Public); err == nil {
		t.Fatal("exp beyond Skew must reject")
	}
	future := signSkewed(t, h.Lab.Signer, time.Hour, 0, 10*time.Second)
	if _, err := auth.ParseExecution(future, h.Lab.Signer.Public); err == nil {
		t.Fatal("nbf beyond Skew must reject")
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	hnd := bruiser.Protect(bruiser.ProtectConfig{Public: h.Lab.Signer.Public})(inner)
	if code := fm.Hit(t, hnd, expired); code != http.StatusForbidden {
		t.Fatalf("expired SDK %d", code)
	}
	within := signSkewed(t, h.Lab.Signer, 2*time.Second, 0, 0)
	if _, err := auth.ParseExecution(within, h.Lab.Signer.Public); err != nil {
		t.Fatalf("within Skew must allow: %v", err)
	}
}

func TestRestartPreservesActiveLease(t *testing.T) {
	h := fm.Start(t)
	tok := h.Session(t, "alice", "agent-1")
	first := h.Acquire(t, tok, "event:ars-che", "hold", nil)
	if first.Status != http.StatusCreated {
		t.Fatalf("setup %d %s", first.Status, first.Body)
	}
	h2 := fm.RestartControlPlane(t, h)
	tok2 := h2.Session(t, "alice", "agent-1")
	resumed := h2.Acquire(t, tok2, "event:ars-che", "hold", nil)
	if resumed.Status != http.StatusOK && resumed.Status != http.StatusCreated {
		t.Fatalf("after restart %d %s", resumed.Status, resumed.Body)
	}
	if resumed.ID != first.ID {
		t.Fatalf("restart duplicated execution %s vs %s", resumed.ID, first.ID)
	}
	if h2.ActiveCount(t, "alice") != 1 {
		t.Fatalf("ACTIVE=%d after restart, silently dropped or duplicated", h2.ActiveCount(t, "alice"))
	}
	e, err := h2.Get(t, first.ID)
	if err != nil || e.State != lease.StateActive {
		t.Fatalf("lease did not survive restart: %+v %v", e, err)
	}
	if e.Fence != first.Fence {
		t.Fatalf("fence changed across restart %d vs %d", e.Fence, first.Fence)
	}
}

func TestResourceVariantInvariant(t *testing.T) {
	h := fm.Start(t)
	variants := fm.CosmeticVariants("ticket:cupfinal")
	if len(variants) != 9 {
		t.Fatalf("want the nine RC1 variants, got %d", len(variants))
	}
	tokens := make([]string, 0, len(variants)*4)
	for i := 0; i < 4; i++ {
		for range variants {
			tokens = append(tokens, h.Session(t, "carol", fmt.Sprintf("v-%d-%d", i, len(tokens))))
		}
	}
	var wg sync.WaitGroup
	var created atomic.Int64
	ids := sync.Map{}
	wg.Add(len(tokens))
	for i, res := range variants {
		for round := 0; round < 4; round++ {
			tok := tokens[round*len(variants)+i]
			res := res
			go func() {
				defer wg.Done()
				got := h.Acquire(t, tok, res, "hold", nil)
				if got.Status == http.StatusCreated {
					created.Add(1)
					ids.Store(got.ID, true)
				}
			}()
		}
	}
	wg.Wait()
	active := h.ActiveCount(t, "carol")
	nIDs := 0
	ids.Range(func(_, _ any) bool { nIDs++; return true })
	if active > 1 || nIDs > 1 || created.Load() > 1 {
		t.Fatalf("resource variants split the domain: ACTIVE=%d created=%d ids=%d (RC1 minted nine)", active, created.Load(), nIDs)
	}
	if active != 1 || created.Load() != 1 {
		t.Fatalf("want one ACTIVE grant, ACTIVE=%d created=%d", active, created.Load())
	}
}

func TestClaimReplayStoreUnavailableDenies(t *testing.T) {
	h := fm.Start(t)
	h.Lab.Store.Close()
	err := h.Lab.Store.ClaimReplay(context.Background(), h.Lab.Cfg.MerchantID, "idem:x", "acquire", time.Minute)
	if err == nil || !errors.Is(err, lease.ErrUnavailable) {
		t.Fatalf("ClaimReplay on dead store: %v", err)
	}
}

func signSkewed(t *testing.T, s auth.Signer, expOffset, iatOffset, nbfOffset time.Duration) string {
	t.Helper()
	now := time.Now().UTC()
	claims := auth.ExecutionClaims{
		MerchantID:  s.MerchantID,
		CustomerID:  "c",
		ExecutionID: id.New("exe"),
		Domain:      "d",
		Resource:    "event:ars-che",
		Action:      "hold",
		Principal:   "agent:a",
		Fence:       1,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "bruiser/" + s.MerchantID,
			Subject:   "c",
			ExpiresAt: jwt.NewNumericDate(now.Add(expOffset)),
			IssuedAt:  jwt.NewNumericDate(now.Add(iatOffset)),
			ID:        id.New("jti"),
		},
	}
	if nbfOffset != 0 {
		claims.NotBefore = jwt.NewNumericDate(now.Add(nbfOffset))
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tok.Header["kid"] = s.KID
	raw, err := tok.SignedString(s.Private)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
