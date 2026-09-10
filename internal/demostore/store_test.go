package demostore

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOriginLockdownBlocksDirectCheckout(t *testing.T) {
	reg := New(Config{HMACSecret: "s", OriginSecret: "lock"})
	st := reg.Create("Bruiser FC", "Shirt", "£50.00", time.Hour)
	srv := httptest.NewServer(reg.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/s/"+st.ID+"/checkout", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403 got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/s/"+st.ID+"/checkout", strings.NewReader(`{}`))
	req.Header.Set("X-Bruiser-Origin-Secret", "lock")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("admitted checkout %d", resp.StatusCode)
	}
}

func TestProductPageAndExpiry(t *testing.T) {
	reg := New(Config{HMACSecret: "s"})
	st := reg.Create("Bruiser FC", "Limited Edition Shirt", "£50.00", 30*time.Millisecond)
	srv := httptest.NewServer(reg.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/s/" + st.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("page %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "LIMITED DROP") || !strings.Contains(string(b), "Bruiser FC") {
		t.Fatalf("html: %s", b)
	}

	time.Sleep(40 * time.Millisecond)
	resp, err = http.Get(srv.URL + "/s/" + st.ID)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expired want 404 got %d", resp.StatusCode)
	}
}

func TestProductJSON(t *testing.T) {
	reg := New(Config{})
	st := reg.Create("Club", "Shirt", "£10", time.Hour)
	srv := httptest.NewServer(reg.Handler())
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/s/" + st.ID + "/product")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out Store
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.ID != st.ID || out.Product != "Shirt" {
		t.Fatalf("%+v", out)
	}
}
