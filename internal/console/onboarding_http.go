package console

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func decodeInput(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	d.DisallowUnknownFields()
	var trailing any
	if d.Decode(out) != nil || d.Decode(&trailing) != io.EOF {
		http.Error(w, "invalid JSON request", http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) onboardingError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, ErrDenied):
		http.Error(w, ErrDenied.Error(), http.StatusUnauthorized)
	case errors.Is(err, ErrInvalid):
		http.Error(w, ErrInvalid.Error(), http.StatusBadRequest)
	case errors.Is(err, pgx.ErrNoRows):
		http.Error(w, "registration or credential not found", http.StatusNotFound)
	case errors.Is(err, ErrConflict), errors.As(err, &pgErr) && pgErr.Code == "23505":
		http.Error(w, "identity already exists; use the existing registration", http.StatusConflict)
	default:
		s.unavailable(w, err)
	}
}

func (s *Server) authConfig(w http.ResponseWriter, r *http.Request) {
	if !validateMethod(w, r, http.MethodGet) {
		return
	}
	available := false
	if s.Config.BootstrapTokenHash != "" {
		if err := s.Store.Pool.QueryRow(r.Context(), `SELECT NOT EXISTS(SELECT 1 FROM op_bootstrap)`).Scan(&available); err != nil {
			s.unavailable(w, err)
			return
		}
	}
	writeJSON(w, map[string]bool{"oidc": s.OIDC != nil, "bootstrap": available})
}

func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	if !validateMethod(w, r, http.MethodPost) || !s.authRateAllowed(w, r) {
		return
	}
	if !tokenEqual(bearer(r), s.Config.BootstrapTokenHash) {
		s.onboardingError(w, ErrDenied)
		return
	}
	var input struct {
		Subject string `json:"subject"`
		Token   string `json:"token"`
	}
	if !decodeInput(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Subject) == "" || len(input.Subject) > 200 || len(input.Token) < 32 || len(input.Token) > 4096 || strings.ContainsAny(input.Token, " \t\r\n") || tokenEqual(input.Token, s.Config.BootstrapTokenHash) {
		s.onboardingError(w, ErrInvalid)
		return
	}
	for _, reader := range s.Config.Readers {
		if reader.Subject == input.Subject || tokenEqual(input.Token, reader.TokenHash) {
			s.onboardingError(w, ErrConflict)
			return
		}
	}
	for _, agent := range s.Config.Agents {
		if tokenEqual(input.Token, agent.TokenHash) {
			s.onboardingError(w, ErrConflict)
			return
		}
	}
	if err := s.Store.Bootstrap(r.Context(), input.Subject, TokenHash(input.Token)); err != nil {
		s.onboardingError(w, err)
		return
	}
	if err := s.issueSession(r.Context(), w, input.Subject); err != nil {
		s.unavailable(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	if !validateMethod(w, r, http.MethodPost) || !s.authRateAllowed(w, r) {
		return
	}
	var input struct {
		ClusterID string `json:"clusterID"`
		KubeUID   string `json:"kubeUID"`
	}
	if !decodeInput(w, r, &input) {
		return
	}
	credential, err := s.Store.RedeemEnrollment(r.Context(), bearer(r), input.ClusterID, input.KubeUID)
	if err != nil {
		s.onboardingError(w, err)
		return
	}
	writeJSON(w, credential)
}

func (s *Server) adminRoute(w http.ResponseWriter, r *http.Request) {
	reader, err := s.reader(r)
	if err != nil {
		s.unavailable(w, err)
		return
	}
	if reader == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if reader.Role != RoleAdmin {
		http.Error(w, "administrator access required", http.StatusForbidden)
		return
	}
	// JSON-only writes and the public-origin check reject browser CSRF forms.
	switch r.URL.Path {
	case "/api/admin/clusters":
		if r.Method == http.MethodPost {
			s.registerCluster(w, r, reader.Subject)
		} else if validateMethod(w, r, http.MethodGet) {
			result, err := s.Store.Registrations(r.Context())
			s.adminResult(w, result, err)
		}
	case "/api/admin/enrollments":
		if validateMethod(w, r, http.MethodPost) {
			var input struct {
				ClusterID string `json:"clusterID"`
			}
			if decodeInput(w, r, &input) {
				result, err := s.Store.IssueEnrollment(r.Context(), reader.Subject, input.ClusterID)
				s.adminResult(w, result, err)
			}
		}
	case "/api/admin/credentials":
		if validateMethod(w, r, http.MethodGet) {
			result, err := s.Store.CredentialInfos(r.Context())
			s.adminResult(w, result, err)
		}
	case "/api/admin/revoke":
		if validateMethod(w, r, http.MethodPost) {
			var input struct {
				ID string `json:"id"`
			}
			if decodeInput(w, r, &input) {
				if err := s.Store.Revoke(r.Context(), reader.Subject, input.ID); err != nil {
					s.onboardingError(w, err)
				} else {
					w.WriteHeader(http.StatusNoContent)
				}
			}
		}
	case "/api/admin/audit":
		if !validateMethod(w, r, http.MethodGet) {
			return
		}
		var before int64
		if r.URL.Query().Has("before") {
			before, err = strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
			if err != nil || before < 1 {
				s.onboardingError(w, ErrInvalid)
				return
			}
		}
		result, err := s.Store.Audit(r.Context(), before)
		s.adminResult(w, result, err)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) adminResult(w http.ResponseWriter, result any, err error) {
	if err != nil {
		s.onboardingError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) registerCluster(w http.ResponseWriter, r *http.Request, actor string) {
	var input Registration
	if !decodeInput(w, r, &input) {
		return
	}
	// Legacy registrations are authoritative until explicitly migrated; never
	// allow onboarding to take over an existing collector or its history.
	for _, a := range s.Config.Agents {
		if a.ID == input.ID {
			s.onboardingError(w, ErrConflict)
			return
		}
	}
	if err := s.Store.Register(r.Context(), actor, input); err != nil {
		s.onboardingError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]string{"id": input.ID})
}
