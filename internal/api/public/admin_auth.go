package publicapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

const (
	roleAdmin    = auth.RoleAdmin
	roleOperator = auth.RoleOperator
	adminCookie  = "bruiser_admin"
)

type adminActor struct {
	Role   string
	Actor  string
	Source string
}

func (a adminActor) ok() bool { return a.Role == roleAdmin || a.Role == roleOperator }

func roleAtLeast(have, need string) bool {
	if have == roleAdmin {
		return true
	}
	return have == need
}

func secretMatch(got, want string) bool {
	if want == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (s *Server) identifyAdmin(r *http.Request) adminActor {
	if raw := bearer(r); raw != "" {
		if claims, err := auth.ParseAccess(raw, s.signer.Public); err == nil && claims.MerchantID == s.cfg.MerchantID {
			actor := claims.Actor
			if actor == "" {
				actor = "token:" + claims.ID
			}
			return adminActor{Role: claims.Role, Actor: actor, Source: "token"}
		}
	}
	if got := r.Header.Get("X-Bruiser-Admin-Secret"); secretMatch(got, s.cfg.AdminSecret) {
		return adminActor{Role: roleAdmin, Actor: "admin-secret", Source: "secret"}
	}
	if got := r.Header.Get("X-Bruiser-Operator-Secret"); secretMatch(got, s.cfg.OperatorSecret) {
		return adminActor{Role: roleOperator, Actor: "operator-secret", Source: "secret"}
	}
	if c, err := r.Cookie(adminCookie); err == nil && c.Value != "" {
		if claims, err := auth.ParseAccess(c.Value, s.signer.Public); err == nil && claims.MerchantID == s.cfg.MerchantID {
			actor := claims.Actor
			if actor == "" {
				actor = "cookie"
			}
			return adminActor{Role: claims.Role, Actor: actor, Source: "cookie"}
		}
	}
	return adminActor{}
}

func (s *Server) requireRole(w http.ResponseWriter, r *http.Request, need string) (adminActor, bool) {
	actor := s.identifyAdmin(r)
	if !actor.ok() {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return adminActor{}, false
	}
	if !roleAtLeast(actor.Role, need) {
		writeErr(w, http.StatusForbidden, "insufficient role")
		return adminActor{}, false
	}
	return actor, true
}

func (s *Server) mintAccess(role, actor string, ttl time.Duration) (string, error) {
	return s.signer.SignAccess(role, actor, ttl)
}

func actorAudit(r *http.Request, actor adminActor) pgstore.AdminAudit {
	return pgstore.AdminAudit{
		Actor:     actor.Actor,
		Role:      actor.Role,
		IP:        clientIP(r),
		Auth:      actor.Source,
		RequestID: requestID(r),
	}
}

func (s *Server) writeAdminAudit(ctx context.Context, r *http.Request, actor adminActor, typ, reason string, attrs map[string]any) {
	if s.store == nil {
		return
	}
	if attrs == nil {
		attrs = map[string]any{}
	}
	attrs["actor"] = actor.Actor
	attrs["role"] = actor.Role
	attrs["auth"] = actor.Source
	attrs["ip"] = clientIP(r)
	_ = s.store.WriteActorAudit(ctx, s.cfg.MerchantID, typ, actor.Role, actor.Actor, reason, requestID(r), attrs)
}

func (s *Server) adminMintToken(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireRole(w, r, roleAdmin)
	if !ok {
		return
	}
	var body struct {
		Role       string `json:"role"`
		TTLSeconds int    `json:"ttl_seconds"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
	role := strings.ToLower(strings.TrimSpace(body.Role))
	if role == "" {
		role = roleOperator
	}
	if role != roleAdmin && role != roleOperator {
		writeErr(w, http.StatusBadRequest, "role must be admin or operator")
		return
	}
	ttl := time.Duration(body.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	tok, err := s.mintAccess(role, actor.Actor, ttl)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "sign token")
		return
	}
	s.writeAdminAudit(r.Context(), r, actor, "ADMIN_TOKEN_MINTED", role, map[string]any{"minted_role": role})
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      tok,
		"role":       role,
		"token_type": "Bearer",
		"expires_in": int(ttl.Seconds()),
	})
}
