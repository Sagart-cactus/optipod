package console

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const sessionCookie = "optipod_session"
const sessionLifetime = 8 * time.Hour

func randomToken() string { return base64.RawURLEncoding.EncodeToString(randomBytes()) }
func randomBytes() []byte { b := make([]byte, 32); _, _ = rand.Read(b); return b }

func tokenEqual(token, hash string) bool {
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(TokenHash(token)), []byte(hash)) == 1
}

func bearer(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(value, "Bearer ")
}

func (s *Server) reader(r *http.Request) (*Reader, error) {
	if token := bearer(r); token != "" {
		for _, reader := range s.Config.Readers {
			if tokenEqual(token, reader.TokenHash) {
				return &reader, nil
			}
		}
		return nil, nil
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil, nil
	}
	var subject string
	err = s.Store.Pool.QueryRow(r.Context(), `SELECT subject FROM op_sessions WHERE token_hash=$1 AND expires_at>now()`, TokenHash(cookie.Value)).Scan(&subject)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Current configuration remains authoritative: removing a user or grant
	// revokes access even when a previously issued session still exists.
	for _, reader := range s.Config.Readers {
		if reader.Subject == subject {
			return &reader, nil
		}
	}
	return nil, nil
}

func (s *Server) issueSession(ctx context.Context, w http.ResponseWriter, subject string) error {
	token := randomToken()
	_, err := s.Store.Pool.Exec(ctx, `INSERT INTO op_sessions(token_hash,subject,clusters,expires_at) VALUES($1,$2,$3,$4)`, TokenHash(token), subject, []string{}, time.Now().Add(sessionLifetime))
	if err != nil {
		return err
	}
	s.cookie(w, sessionCookie, token, int(sessionLifetime.Seconds()))
	return nil
}

func (s *Server) cookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.Config.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: age})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		http.Error(w, "invalid login", http.StatusBadRequest)
		return
	}
	for _, reader := range s.Config.Readers {
		if tokenEqual(input.Token, reader.TokenHash) {
			if err := s.issueSession(r.Context(), w, reader.Subject); err != nil {
				s.unavailable(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	http.Error(w, "invalid credentials", http.StatusUnauthorized)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if _, err = s.Store.Pool.Exec(r.Context(), `DELETE FROM op_sessions WHERE token_hash=$1`, TokenHash(cookie.Value)); err != nil {
			s.unavailable(w, err)
			return
		}
	}
	s.cookie(w, sessionCookie, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func validateMethod(w http.ResponseWriter, r *http.Request, want string) bool {
	if r.Method == want {
		return true
	}
	w.Header().Set("Allow", want)
	http.Error(w, fmt.Sprintf("%s required", want), http.StatusMethodNotAllowed)
	return false
}
