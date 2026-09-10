package site

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
)

//go:embed all:static
var static embed.FS

func Mount(r chi.Router) {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		panic(err)
	}
	r.Get("/", serve(sub, "index.html"))
	r.Get("/attack-lab", serve(sub, "attack-lab.html"))
	r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.FS(sub))))
}

func serve(fsys fs.FS, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(b)
	}
}
