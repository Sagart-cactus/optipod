package report

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

//go:embed ui/*
var assets embed.FS

// Handler is read-only and rejects cross-origin and non-loopback Host headers,
// including DNS rebinding. Serve it only on a 127.0.0.1 listener.
func (c *Collector) Handler(host string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		if r.Host != host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+host) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "read-only endpoint", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		var result any
		var err error
		switch r.URL.Path {
		case "/api/fleet":
			result, err = c.Fleet(ctx)
		case "/api/history":
			ns := r.URL.Query().Get("namespace")
			if ns == "" {
				http.Error(w, "namespace is required", 400)
				return
			}
			result, err = c.History(ctx, ns, r.URL.Query().Get("uid"))
		case "/", "/app.js", "/style.css":
			name := strings.TrimPrefix(r.URL.Path, "/")
			if name == "" {
				name = "index.html"
			}
			data, e := assets.ReadFile("ui/" + name)
			if e != nil {
				http.Error(w, "asset unavailable", 500)
				return
			}
			mime := map[string]string{"index.html": "text/html; charset=utf-8", "app.js": "text/javascript; charset=utf-8", "style.css": "text/css; charset=utf-8"}
			w.Header().Set("Content-Type", mime[name])
			_, _ = w.Write(data)
			return
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("Kubernetes read failed: %v", err), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
}
