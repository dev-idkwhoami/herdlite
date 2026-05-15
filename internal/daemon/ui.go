package daemon

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed ui/dist
var uiDist embed.FS

func (s Service) registerUIHandlers(mux *http.ServeMux) {
	dist, err := fs.Sub(uiDist, "ui/dist")
	if err != nil {
		return
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/app/", http.StatusFound)
	})
	mux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/app/", func(w http.ResponseWriter, r *http.Request) {
		serveUI(dist, w, r)
	})
}

func serveUI(dist fs.FS, w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/app/")
	if name == "" {
		name = "index.html"
	}
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if fileExists(dist, name) {
		http.ServeFileFS(w, r, dist, name)
		return
	}
	if path.Ext(name) != "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFileFS(w, r, dist, "index.html")
}

func fileExists(files fs.FS, name string) bool {
	info, err := fs.Stat(files, name)
	return err == nil && !info.IsDir()
}
