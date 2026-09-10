package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Nareik33L/bruiser-gateway/demos/seed"
	"github.com/Nareik33L/bruiser-gateway/demos/shared"
)

const sessionCookie = "hufc_session"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	addr := shared.Env("HARCHESTER_HTTP_ADDR", ":8100")
	dbURL := shared.Env("HARCHESTER_DATABASE_URL", "postgres://bruiser:bruiser@127.0.0.1:5432/harchester?sslmode=disable")
	sso := shared.Env("HARCHESTER_SSO_SECRET", "harchester-sso-dev")
	ticketsURL := shared.Env("SIMTIX_PUBLIC_URL", "http://127.0.0.1:8091")
	simtixInternal := shared.Env("SIMTIX_ORIGIN_URL", "http://127.0.0.1:8090")
	opponent := shared.Env("DEMO_OPPONENT", seed.OpponentDefault)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := seed.ApplyHarchester(ctx, pool, seed.SupporterCount); err != nil {
		log.Error("seed", "err", err)
		os.Exit(1)
	}
	log.Info("supporters seeded")

	s := &server{
		pool:           pool,
		sso:            sso,
		ticketsURL:     ticketsURL,
		simtixInternal: simtixInternal,
		opponent:       opponent,
		adminSecret:    shared.Env("DEMO_ADMIN_SECRET", "demo-admin-dev"),
		log:            log,
	}
	srv := &http.Server{Addr: addr, Handler: s.routes(), ReadHeaderTimeout: 5 * time.Second}
	log.Info("harchester listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

type server struct {
	pool           *pgxpool.Pool
	sso            string
	ticketsURL     string
	simtixInternal string
	opponent       string
	adminSecret    string
	log            *slog.Logger
}

type member struct {
	seed.Supporter
}

func (s *server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		shared.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "harchester"})
	})
	r.Handle("/assets/*", staticHandler())
	r.Get("/", s.pageHome)
	r.Get("/news", s.pageNews)
	r.Get("/fixtures", s.pageFixtures)
	r.Get("/members", s.pageMembers)
	r.Get("/login", s.pageLogin)
	r.Post("/login", s.postLogin)
	r.Post("/logout", s.logout)
	r.Get("/account", s.pageAccount)
	r.Get("/tickets/hfc-ars", s.pageMatch)
	r.Get("/tickets/hfc-ars/buy", s.buy)
	r.Get("/_admin/online", s.online)
	r.Post("/_admin/reset-sessions", s.resetSessions)
	return r
}

func (s *server) adminAuth(w http.ResponseWriter, r *http.Request) bool {
	got := r.Header.Get("X-Demo-Admin-Secret")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.adminSecret)) != 1 {
		shared.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func (s *server) online(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuth(w, r) {
		return
	}
	var n int
	_ = s.pool.QueryRow(r.Context(), `select count(*) from club_sessions where last_seen > now() - interval '10 minutes'`).Scan(&n)
	shared.WriteJSON(w, http.StatusOK, map[string]int{"customers_online": n})
}

func (s *server) resetSessions(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuth(w, r) {
		return
	}
	tag, err := s.pool.Exec(r.Context(), `delete from club_sessions`)
	if err != nil {
		shared.WriteErr(w, http.StatusInternalServerError, "reset-sessions")
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"status":  "reset",
		"deleted": tag.RowsAffected(),
	})
}

func (s *server) lookup(ctx context.Context, membership, password string) (member, bool) {
	var m member
	err := s.pool.QueryRow(ctx, `
		select membership_number, customer_id, first_name, last_name, email, password,
		       membership_tier, loyalty_points, season_ticket, eligible_for_arsenal
		from supporters where membership_number=$1`, membership).Scan(
		&m.MembershipNumber, &m.CustomerID, &m.FirstName, &m.LastName, &m.Email, &m.Password,
		&m.MembershipTier, &m.LoyaltyPoints, &m.SeasonTicket, &m.EligibleForArsenal)
	if err != nil {
		return member{}, false
	}
	if password != "" && m.Password != password {
		return member{}, false
	}
	return m, true
}

func (s *server) current(r *http.Request) (member, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return member{}, false
	}
	var mem string
	err = s.pool.QueryRow(r.Context(), `
		select membership_number from club_sessions
		where id=$1 and expires_at > now()`, c.Value).Scan(&mem)
	if err != nil {
		return member{}, false
	}
	_, _ = s.pool.Exec(r.Context(), `update club_sessions set last_seen=now() where id=$1`, c.Value)
	return s.lookup(r.Context(), mem, "")
}

func (s *server) createSession(ctx context.Context, membership string) (string, error) {
	var b [16]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	_, err := s.pool.Exec(ctx, `
		insert into club_sessions (id, membership_number, expires_at)
		values ($1,$2, now() + interval '12 hours')`, id, membership)
	return id, err
}

func (s *server) postLogin(w http.ResponseWriter, r *http.Request) {
	membership, password := "", ""
	if strings.Contains(r.Header.Get("Content-Type"), "json") {
		var body struct {
			MembershipNumber string `json:"membership_number"`
			Password         string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		membership, password = body.MembershipNumber, body.Password
	} else {
		_ = r.ParseForm()
		membership = r.FormValue("membership_number")
		password = r.FormValue("password")
	}
	m, ok := s.lookup(r.Context(), membership, password)
	if !ok {
		if strings.Contains(r.Header.Get("Content-Type"), "json") {
			shared.WriteErr(w, http.StatusUnauthorized, "invalid membership number or password")
			return
		}
		s.renderLogin(w, r, "We could not match that membership number and password.")
		return
	}
	sid, err := s.createSession(r.Context(), m.MembershipNumber)
	if err != nil {
		shared.WriteErr(w, http.StatusInternalServerError, "session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: sid, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	next := r.URL.Query().Get("next")
	if next == "" {
		next = r.FormValue("next")
	}
	if next == "" {
		next = "/account"
	}
	if strings.Contains(r.Header.Get("Content-Type"), "json") {
		shared.WriteJSON(w, http.StatusOK, map[string]string{
			"membership_number": m.MembershipNumber, "name": m.FirstName + " " + m.LastName, "next": next,
		})
		return
	}
	http.Redirect(w, r, next, http.StatusFound)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_, _ = s.pool.Exec(r.Context(), `delete from club_sessions where id=$1`, c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *server) buy(w http.ResponseWriter, r *http.Request) {
	m, ok := s.current(r)
	if !ok {
		http.Redirect(w, r, "/login?next=/tickets/hfc-ars/buy", http.StatusFound)
		return
	}
	tok, err := shared.IssueHandoff(s.sso, m.MembershipNumber, m.FirstName+" "+m.LastName, m.Email, m.EligibleForArsenal)
	if err != nil {
		shared.WriteErr(w, http.StatusInternalServerError, "handoff")
		return
	}
	http.Redirect(w, r, strings.TrimRight(s.ticketsURL, "/")+"/sso?token="+tok, http.StatusFound)
}
