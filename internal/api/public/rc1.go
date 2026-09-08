package publicapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/identity"
	"github.com/Nareik33L/bruiser-gateway/internal/limit"
	"github.com/Nareik33L/bruiser-gateway/internal/resource"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

func (s *Server) identityInput(r *http.Request, trustedEdge bool) identity.Input {
	jwks := s.profile.Identity.JWKSURL
	if jwks == "" {
		jwks = s.cfg.JWKSURL
	}
	id := s.profile.Identity
	id.JWKSURL = jwks
	if id.Issuer == "" {
		id.Issuer = s.cfg.Issuer
	}
	if id.Audience == "" {
		id.Audience = s.cfg.Audience
	}
	return identity.Input{
		Identity:            id,
		Secret:              s.cfg.DevHMACSecret,
		EdgeSecret:          s.cfg.EdgeSecret,
		Cookie:              cookieValue(r, firstNonEmpty(s.profile.Identity.Cookie, "boxoffice_session")),
		Bearer:              bearer(r),
		Header:              headerValue(r, s.profile.Identity.Header),
		Principal:           headerValue(r, s.profile.Identity.PrincipalHeader),
		Introspect:          firstNonEmpty(s.profile.Identity.IntrospectURL, s.cfg.IntrospectURL),
		MerchantID:          s.cfg.MerchantID,
		AllowDevAssertions:  s.cfg.DevAssertions && !s.cfg.Production(),
		AllowUnsignedHeader: false,
		TrustedEdge:         trustedEdge,
		RequireIssuer:       s.cfg.Production(),
		RequireAudience:     s.cfg.Production(),
	}
}

func (s *Server) edgeAuthorized(r *http.Request) bool {
	if s.cfg.EdgeSecret == "" {
		return false
	}
	got := r.Header.Get("X-Bruiser-Edge-Secret")
	return subtleEqual(got, s.cfg.EdgeSecret)
}

func subtleEqual(got, want string) bool {
	if len(got) != len(want) {
		// still compare to keep timing flatter on equal length; reject immediately if empty want
		return false
	}
	var v byte
	for i := 0; i < len(want); i++ {
		v |= got[i] ^ want[i]
	}
	return v == 0
}

// stripInboundBruiser drops client-supplied control headers. Edge/admin
// credentials and the event hint are the only inbound X-Bruiser-* accepted.
func stripInboundBruiser(next http.Handler) http.Handler {
	keep := map[string]bool{
		"X-Bruiser-Edge-Secret":  true,
		"X-Bruiser-Admin-Secret": true,
		"X-Bruiser-Event-Id":     true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name := range r.Header {
			if !strings.HasPrefix(http.CanonicalHeaderKey(name), "X-Bruiser-") {
				continue
			}
			if keep[http.CanonicalHeaderKey(name)] {
				continue
			}
			r.Header.Del(name)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) rateGuards() *rateGuards {
	return &rateGuards{
		sessions:  limit.NewKeyed("sessions", s.cfg.RateSessions, 0),
		acquire:   limit.NewKeyed("acquire", s.cfg.RateAcquire, 0),
		renew:     limit.NewKeyed("renew", s.cfg.RateRenew, 0),
		release:   limit.NewKeyed("release", s.cfg.RateRelease, 0),
		authorize: limit.NewKeyed("authorize", s.cfg.RateAuthorize, 0),
		merchant:  limit.NewKeyed("merchant", s.cfg.RateMerchant, 0),
		customer:  limit.NewKeyed("customer", s.cfg.RateCustomer, 0),
		principal: limit.NewKeyed("principal", s.cfg.RatePrincipal, 0),
		ip:        limit.NewKeyed("ip", s.cfg.RateIP, 0),
	}
}

type rateGuards struct {
	sessions, acquire, renew, release, authorize *limit.Keyed
	merchant, customer, principal, ip            *limit.Keyed
}

func (g *rateGuards) check(w http.ResponseWriter, class, key string, bucket *limit.Keyed) bool {
	if bucket == nil || bucket.Allow(key, time.Now()) {
		return true
	}
	w.Header().Set("Retry-After", "1")
	writeErr(w, http.StatusTooManyRequests, "rate limited")
	return false
}

func (s *Server) replayKey(r *http.Request) string {
	if v := r.Header.Get("Idempotency-Key"); v != "" {
		return "idem:" + v
	}
	if v := r.Header.Get("X-Bruiser-Nonce"); v != "" {
		return "nonce:" + v
	}
	if v := r.Header.Get("X-Request-Id"); v != "" {
		return "rid:" + v
	}
	return ""
}

func (s *Server) claimReplay(w http.ResponseWriter, r *http.Request, kind string) bool {
	key := s.replayKey(r)
	if key == "" {
		return true
	}
	if err := s.store.ClaimReplay(r.Context(), s.cfg.MerchantID, key, kind, 10*time.Minute); err != nil {
		if err == pgstore.ErrReplay {
			writeErr(w, http.StatusConflict, "replayed request")
			return false
		}
		s.storeError(w, err)
		return false
	}
	return true
}

func canonicalizeResource(raw string) (string, error) {
	return resource.Canonical(raw)
}

func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		return host[:i]
	}
	return host
}
