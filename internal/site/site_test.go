package site

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestPagesAndAssets(t *testing.T) {
	r := chi.NewRouter()
	Mount(r)

	assertContains := func(path, want string, contentType string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s → %d", path, rec.Code)
		}
		if contentType != "" && !strings.Contains(rec.Header().Get("Content-Type"), contentType) {
			t.Fatalf("%s content-type %s", path, rec.Header().Get("Content-Type"))
		}
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s missing %q", path, want)
		}
	}

	assertContains("/", "Launch Attack Lab", "text/html")
	assertContains("/attack-lab", "Can your store survive the drop?", "text/html")
	assertContains("/assets/css/site.css", "--lime:", "text/css")
	assertContains("/assets/js/attack-lab.js", "startAttack", "javascript")
}
