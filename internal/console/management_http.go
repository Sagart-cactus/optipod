package console

import "net/http"

func (s *Server) managementRoute(w http.ResponseWriter, r *http.Request, actor string) {
	switch r.URL.Path {
	case "/api/admin/users", "/api/admin/groups":
		group := r.URL.Path == "/api/admin/groups"
		if r.Method == http.MethodGet {
			result, err := s.Store.Grants(r.Context(), group)
			s.adminResult(w, result, err)
			return
		}
		if !validateMethod(w, r, http.MethodPost) {
			return
		}
		var input UserGrant
		if !decodeInput(w, r, &input) {
			return
		}
		if !group {
			for _, reader := range s.Config.Readers {
				if reader.Subject == input.Subject {
					s.onboardingError(w, ErrConflict)
					return
				}
			}
		}
		token, err := s.Store.SaveGrant(r.Context(), actor, input, group)
		s.adminResult(w, map[string]string{"token": token}, err)
	case "/api/admin/connection":
		if !validateMethod(w, r, http.MethodPost) {
			return
		}
		var input struct {
			ID        string `json:"id"`
			Connected bool   `json:"connected"`
		}
		if decodeInput(w, r, &input) {
			s.adminResult(w, map[string]bool{savedKey: true}, s.Store.SetConnected(r.Context(), actor, input.ID, input.Connected))
		}
	default:
		s.fleetManagementRoute(w, r, actor)
	}
}

func (s *Server) fleetManagementRoute(w http.ResponseWriter, r *http.Request, actor string) {
	switch r.URL.Path {
	case "/api/admin/first-policy":
		s.firstPolicy(w, r)
	case "/api/admin/health":
		if validateMethod(w, r, http.MethodGet) {
			result, err := s.Store.Health(r.Context())
			s.adminResult(w, result, err)
		}
	case "/api/admin/notifications":
		if r.Method == http.MethodGet {
			result, err := s.Store.Notifications(r.Context())
			s.adminResult(w, result, err)
			return
		}
		if !validateMethod(w, r, http.MethodPost) {
			return
		}
		var input struct {
			ID int64 `json:"id"`
		}
		if !decodeInput(w, r, &input) {
			return
		}
		if input.ID < 1 {
			s.onboardingError(w, ErrInvalid)
			return
		}
		err := s.Store.AcknowledgeNotification(r.Context(), actor, input.ID)
		s.adminResult(w, map[string]bool{"acknowledged": true}, err)
	case "/api/admin/templates":
		if r.Method == http.MethodGet {
			result, err := s.Store.Templates(r.Context())
			s.adminResult(w, result, err)
			return
		}
		if !validateMethod(w, r, http.MethodPost) {
			return
		}
		var input EnvironmentTemplate
		if decodeInput(w, r, &input) {
			s.adminResult(w, map[string]bool{savedKey: true}, s.Store.SaveTemplate(r.Context(), actor, input))
		}
	case "/api/admin/bulk":
		if !validateMethod(w, r, http.MethodPost) {
			return
		}
		var input []Registration
		if !decodeInput(w, r, &input) {
			return
		}
		for _, reg := range input {
			for _, a := range s.Config.Agents {
				if a.ID == reg.ID {
					s.onboardingError(w, ErrConflict)
					return
				}
			}
		}
		for i := range input {
			if input[i].RequireMTLS && s.Authority == nil {
				s.onboardingError(w, ErrInvalid)
				return
			}
			input[i].RequireMTLS = s.Authority != nil
		}
		s.adminResult(w, map[string]bool{savedKey: true}, s.Store.RegisterBulk(r.Context(), actor, input))
	default:
		http.NotFound(w, r)
	}
}
