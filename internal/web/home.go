package web

import (
	_ "embed"
	"net/http"
	"strings"
)

const githubAppURL = "https://github.com/apps/diffsense-ai"

//go:embed home.html
var homeHTML string

//go:embed logo.svg
var logoSVG []byte

// LogoHandler serves the DiffSense owl mascot.
func LogoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(logoSVG)
	}
}

// HomeHandler serves the public landing page at GET /.
func HomeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(homeHTML, "{{APP_URL}}", githubAppURL)))
	}
}
