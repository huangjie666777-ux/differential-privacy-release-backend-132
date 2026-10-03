package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"private-release/internal/domain"
	"private-release/internal/service"
	"private-release/internal/store"
)

type Server struct {
	service  *service.Service
	adminKey string
}

func NewRouter(svc *service.Service, adminKey string) http.Handler {
	s := &Server{service: svc, adminKey: adminKey}
	r := chi.NewRouter()
	r.Get("/healthz", s.health)
	r.Route("/v1/projects", func(r chi.Router) {
		r.With(s.requireAdmin).Post("/", s.createProject)
		r.Get("/{projectID}", s.getProject)
		r.Post("/{projectID}/queries", s.query)
	})
	return r
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("X-Admin-Key")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.adminKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid or missing admin key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in domain.CreateProjectInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.service.CreateProject(&in); err != nil {
		s.writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.service.Project(chi.URLParam(r, "projectID"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) query(w http.ResponseWriter, r *http.Request) {
	var req service.QueryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.service.Query(r.Context(), chi.URLParam(r, "projectID"), req)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrAlreadyExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrBudgetExceeded):
		writeError(w, http.StatusPaymentRequired, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
