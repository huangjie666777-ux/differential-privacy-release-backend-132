// Package httpapi exposes the HTTP API: admin project/contribution
// management and public differentially private releases.
package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

	"private-release/internal/decimal"
	"private-release/internal/dp"
	"private-release/internal/store"
)

type Server struct {
	st       *store.Store
	mech     *dp.Mechanism
	adminKey string
}

func NewServer(st *store.Store, adminKey string) *Server {
	return &Server{st: st, mech: dp.NewMechanism(), adminKey: adminKey}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Route("/projects", func(r chi.Router) {
		r.With(s.requireAdmin).Post("/", s.createProject)
		r.With(s.requireAdmin).Post("/{id}/contributions", s.addContribution)
		r.Get("/{id}/count", s.release("count"))
		r.Get("/{id}/sum", s.release("sum"))
		r.Get("/{id}/histogram", s.release("histogram"))
	})
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return r
}

var projectIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

type createProjectReq struct {
	ID            string `json:"id"`
	Lower         int64  `json:"lower"`
	Upper         int64  `json:"upper"`
	Bins          int64  `json:"bins"`
	EpsilonBudget string `json:"epsilon_budget"`
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !projectIDRe.MatchString(req.ID) {
		writeErr(w, http.StatusBadRequest, "id must be 1-64 chars of [A-Za-z0-9_-]")
		return
	}
	cfg := dp.Config{Lower: req.Lower, Upper: req.Upper, Bins: req.Bins}
	if err := cfg.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	budget, err := decimal.Parse(req.EpsilonBudget)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid epsilon_budget: "+err.Error())
		return
	}
	p := store.Project{ID: req.ID, Lower: req.Lower, Upper: req.Upper, Bins: req.Bins, Budget: budget}
	if err := s.st.CreateProject(p); err != nil {
		if errors.Is(err, store.ErrProjectExists) {
			writeErr(w, http.StatusConflict, "project already exists; projects are immutable")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": p.ID, "lower": p.Lower, "upper": p.Upper, "bins": p.Bins,
		"epsilon_budget": budget.String(),
	})
}

type contributionReq struct {
	UserID string `json:"user_id"`
	Value  int64  `json:"value"`
}

func (s *Server) addContribution(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.st.GetProject(id)
	if err != nil {
		projectErr(w, err)
		return
	}
	var req contributionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.UserID == "" || len(req.UserID) > 256 {
		writeErr(w, http.StatusBadRequest, "user_id must be 1-256 chars")
		return
	}
	cfg := dp.Config{Lower: p.Lower, Upper: p.Upper, Bins: p.Bins}
	clamped := cfg.Clamp(req.Value)
	if err := s.st.AddContribution(id, req.UserID, clamped); err != nil {
		if errors.Is(err, store.ErrDuplicateUser) {
			writeErr(w, http.StatusConflict, "user_id already contributed")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "recorded"})
}

// release returns a handler for one public DP release operation.
func (s *Server) release(op string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		p, err := s.st.GetProject(id)
		if err != nil {
			projectErr(w, err)
			return
		}
		epsStr := r.URL.Query().Get("epsilon")
		eps, err := decimal.Parse(epsStr)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid epsilon: "+err.Error())
			return
		}
		cfg := dp.Config{Lower: p.Lower, Upper: p.Upper, Bins: p.Bins}

		outcome, err := s.st.Publish(r.Context(), id, op, eps, func() ([]byte, error) {
			values, err := s.st.Contributions(id)
			if err != nil {
				return nil, err
			}
			return s.compute(op, cfg, values, eps.Float64())
		})
		if err != nil {
			if errors.Is(err, store.ErrBudgetExhausted) {
				writeErr(w, http.StatusForbidden, err.Error())
				return
			}
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(outcome.Result, &payload); err != nil {
			payload = map[string]json.RawMessage{}
		}
		meta, _ := json.Marshal(map[string]any{
			"epsilon":           eps.String(),
			"epsilon_spent":     outcome.Spent.String(),
			"epsilon_remaining": outcome.Remaining.String(),
			"reused":            outcome.Reused,
		})
		payload["privacy"] = meta
		json.NewEncoder(w).Encode(payload)
	}
}

// compute runs the DP mechanism for a fresh release.
func (s *Server) compute(op string, cfg dp.Config, values []int64, eps float64) ([]byte, error) {
	switch op {
	case "count":
		n, err := s.mech.NoisyCount(int64(len(values)), eps)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"operation": op, "count": n})
	case "sum":
		var total int64
		for _, v := range values {
			total += v
		}
		n, err := s.mech.NoisySum(total, cfg, eps)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"operation": op, "sum": n})
	case "histogram":
		counts := make([]int64, cfg.Bins)
		for _, v := range values {
			counts[cfg.BinOf(v)]++
		}
		noisy, err := s.mech.NoisyHistogram(counts, eps)
		if err != nil {
			return nil, err
		}
		layout := cfg.BinLayout()
		type binOut struct {
			Index int64   `json:"index"`
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Count int64   `json:"count"`
		}
		bins := make([]binOut, len(layout))
		for i, b := range layout {
			bins[i] = binOut{Index: b.Index, Start: b.Start, End: b.End, Count: noisy[i]}
		}
		return json.Marshal(map[string]any{"operation": op, "bins": bins})
	}
	return nil, errors.New("unknown operation")
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-Admin-Key")
		if key == "" || subtle.ConstantTimeCompare([]byte(key), []byte(s.adminKey)) != 1 {
			writeErr(w, http.StatusUnauthorized, "invalid admin key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func projectErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	writeErr(w, http.StatusInternalServerError, err.Error())
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
