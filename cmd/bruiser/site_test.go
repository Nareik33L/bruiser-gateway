package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/attacklab"
)

func TestComposeSiteServesStorefrontGET(t *testing.T) {
	lab, err := attacklab.New(attacklab.Config{HMACSecret: "s", OriginSecret: "lock"})
	if err != nil {
		t.Fatal(err)
	}
	h := composeSite(http.NotFoundHandler(), lab)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/lab/stores", strings.NewReader(`{"name":"Bruiser FC"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var st struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/lab/stores/"+st.ID+"/mode", strings.NewReader(`{"mode":"off"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("mode %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, st.URL, nil)
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("storefront %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "LIMITED DROP") {
		t.Fatalf("html %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Attack Lab") {
		t.Fatalf("home %d", rec.Code)
	}
}
