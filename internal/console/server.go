package console

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

//go:embed ui/*
var assets embed.FS

const MaxBatchBytes = 8 * 1024 * 1024

type Server struct {
	Store  *Store
	Config Config
	OIDC   *LoginProvider
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.URL.Path == "/healthz" {
			w.WriteHeader(200)
			return
		}
		if r.URL.Path == "/readyz" {
			if err := s.Store.Pool.Ping(r.Context()); err != nil {
				http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			}
			return
		}
		// Agents use their scoped credentials over a private service address;
		// browser routes are bound to the configured public origin.
		if r.URL.Path == "/api/ingest" {
			s.ingest(w, r)
			return
		}
		u, _ := url.Parse(s.Config.PublicURL)
		if r.Host != u.Host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != strings.TrimSuffix(s.Config.PublicURL, "/")) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		s.route(w, r)
	})
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/auth/login":
		if validateMethod(w, r, http.MethodPost) {
			s.login(w, r)
		}
		return
	case "/auth/logout":
		if validateMethod(w, r, http.MethodPost) {
			s.logout(w, r)
		}
		return
	case "/auth/oidc", "/auth/callback":
		if validateMethod(w, r, http.MethodGet) {
			s.oidcRoute(w, r)
		}
		return
	case "/auth/config":
		writeJSON(w, map[string]bool{"oidc": s.OIDC != nil})
		return
	case "/", "/app.js", "/style.css":
		if validateMethod(w, r, http.MethodGet) {
			s.asset(w, r)
		}
		return
	}
	if !validateMethod(w, r, http.MethodGet) {
		return
	}
	reader, err := s.reader(r)
	if err != nil {
		s.unavailable(w, err)
		return
	}
	if reader == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if r.URL.Path == "/api/me" {
		writeJSON(w, map[string]any{"subject": reader.Subject, "clusters": reader.Clusters, "namespaces": reader.Namespaces})
		return
	}
	if r.URL.Path == "/api/clusters" {
		clusters, err := s.Store.Clusters(r.Context(), reader.Clusters)
		if err != nil {
			s.unavailable(w, err)
			return
		}
		if len(reader.Namespaces) > 0 {
			for i := range clusters {
				if len(clusters[i].Warnings) > 0 {
					clusters[i].Warnings = []string{"Some observations are incomplete; collection warnings require cluster-wide access."}
				}
			}
		}
		writeJSON(w, clusters)
		return
	}
	if r.URL.Path == "/api/summary" {
		cluster := r.URL.Query().Get("cluster")
		if cluster != "" && !slices.Contains(reader.Clusters, cluster) {
			http.Error(w, "scope not allowed", http.StatusForbidden)
			return
		}
		result, err := s.Store.Summary(r.Context(), reader, cluster)
		if err != nil {
			s.unavailable(w, err)
			return
		}
		writeJSON(w, result)
		return
	}
	s.list(w, r, reader)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request, reader *Reader) {
	kind := strings.TrimPrefix(r.URL.Path, "/api/")
	if !slices.Contains([]string{resourceWorkloads, "history", resourcePolicies, "revisions", "samples"}, kind) {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	cluster, ns := q.Get("cluster"), q.Get("namespace")
	if (cluster != "" && !slices.Contains(reader.Clusters, cluster)) || (ns != "" && len(reader.Namespaces) > 0 && !slices.Contains(reader.Namespaces, ns)) {
		http.Error(w, "scope not allowed", http.StatusForbidden)
		return
	}
	limit := 50
	var err error
	if q.Has("limit") {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 || limit > 200 {
			http.Error(w, "limit must be 1..200", http.StatusBadRequest)
			return
		}
	}
	if cursor := q.Get("cursor"); cursor != "" {
		n, e := strconv.ParseInt(cursor, 10, 64)
		if e != nil || n < 1 {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
	}
	page, err := s.Store.List(r.Context(), kind, Query{Clusters: reader.Clusters, Namespaces: reader.Namespaces, Cluster: cluster, Namespace: ns, UID: q.Get("uid"), Search: q.Get("search"), Mode: q.Get("mode"), Cursor: q.Get("cursor"), Limit: limit, IncludeDeleted: q.Get("deleted") == "true"})
	if err != nil {
		s.unavailable(w, err)
		return
	}
	writeJSON(w, page)
}

func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	if !validateMethod(w, r, http.MethodPost) {
		return
	}
	var agent *AgentCredential
	for _, a := range s.Config.Agents {
		if tokenEqual(bearer(r), a.TokenHash) {
			agent = &a
			break
		}
	}
	if agent == nil {
		http.Error(w, "agent authentication required", http.StatusUnauthorized)
		return
	}
	var b Batch
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBatchBytes))
	d.DisallowUnknownFields()
	if err := d.Decode(&b); err != nil {
		http.Error(w, "invalid batch", http.StatusBadRequest)
		return
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		http.Error(w, "trailing batch data", http.StatusBadRequest)
		return
	}
	if err := b.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	err := s.Store.Ingest(ctx, agent.ID, agent.Name, &b)
	if errors.Is(err, ErrConflict) {
		http.Error(w, ErrConflict.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		s.unavailable(w, err)
		return
	}
	writeJSON(w, map[string]int64{"acknowledged": b.Sequence})
}

func (s *Server) unavailable(w http.ResponseWriter, err error) {
	// Database errors may contain record contents or connection information.
	// Only their type is logged; payloads and credentials never enter HTTP errors.
	slog.Error("console storage operation failed", "errorType", errorType(err))
	http.Error(w, "storage temporarily unavailable", http.StatusServiceUnavailable)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) asset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	data, err := assets.ReadFile("ui/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", map[string]string{"index.html": "text/html; charset=utf-8", "app.js": "text/javascript; charset=utf-8", "style.css": "text/css; charset=utf-8"}[name])
	_, _ = w.Write(data)
}
