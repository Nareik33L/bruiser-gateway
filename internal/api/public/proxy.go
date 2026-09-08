package publicapi

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// ProxyHandler is the Proxy deployment method: Bruiser terminates box-office
// traffic, admits via the same path as /v1/authorize, and forwards to origin
// with fence + origin secret. It is a placement, not a product.
func (s *Server) ProxyHandler(originURL string) (http.Handler, error) {
	ou, err := url.Parse(originURL)
	if err != nil {
		return nil, err
	}
	rp := httputil.NewSingleHostReverseProxy(ou)
	orig := rp.Director
	rp.Director = func(req *http.Request) {
		orig(req)
		req.Host = ou.Host
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		_ = r.Body.Close()

		eventID := eventIDFromBody(body)
		res := s.admit(r.Context(), r.Method, r.URL.Path, eventID, cookieValue(r, s.profile.Identity.Cookie), bearer(r), requestID(r))
		if !res.allow {
			writeAdmit(w, res)
			return
		}
		if res.exeID != "" {
			release, ok := s.limit.Take(res.exeID, time.Now())
			if !ok {
				w.Header().Set("Retry-After", "1")
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "execution throttled"})
				return
			}
			defer release()
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		for _, k := range []string{"X-Bruiser-Execution", "X-Bruiser-Fence", "X-Bruiser-Customer", "X-Bruiser-Origin-Secret"} {
			if v := res.headers.Get(k); v != "" {
				r.Header.Set(k, v)
			}
		}
		rp.ServeHTTP(w, r)
	}), nil
}
