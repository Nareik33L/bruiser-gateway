package main

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Nareik33L/bruiser-gateway/internal/attacklab"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	"github.com/Nareik33L/bruiser-gateway/internal/site"
)

func composeSite(gw http.Handler, lab *attacklab.Lab) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	site.Mount(r)
	r.Mount("/lab", lab.API())
	front := lab.Storefront()
	r.Handle("/s/{storeID}", front)
	r.Handle("/s/{storeID}/*", front)

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		p := req.URL.Path
		if p == "/" || p == "/attack-lab" || strings.HasPrefix(p, "/assets/") || strings.HasPrefix(p, "/lab/") || strings.HasPrefix(p, "/s/") {
			r.ServeHTTP(w, req)
			return
		}
		gw.ServeHTTP(w, req)
	})
}

func withAttackLabRoutes(p merchant.Profile) merchant.Profile {
	p.Routes = append(p.Routes, attacklab.Routes()...)
	return p
}

func sweepLab(lab *attacklab.Lab, every time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			lab.Sweep()
		}
	}
}
