package proxy

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/unclefroob/maraetai-service/internal/auth"
	"github.com/unclefroob/maraetai-service/internal/store"
)

const (
	defaultStatsDays = 365
	maxStatsDays     = 3650
)

// statsHandler serves /api/stats: a Wrapped-style JSON summary of the
// authenticated user's listening. This is the service's own JSON API (not
// Subsonic-shaped) and backs the SPA's stats view.
type statsHandler struct {
	store *store.Store
	auth  *auth.Validator
	log   *slog.Logger
}

type statsError struct {
	Error string `json:"error"`
}

func (h *statsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	user, err := h.auth.Validate(r.Context(), q)
	switch {
	case errors.Is(err, auth.ErrMissingParams):
		writeJSON(w, http.StatusBadRequest, statsError{"missing credentials"})
		return
	case errors.Is(err, auth.ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, statsError{"unauthorized"})
		return
	case err != nil:
		h.log.Error("stats: auth validation failed", "err", err)
		writeJSON(w, http.StatusBadGateway, statsError{"authentication unavailable"})
		return
	}

	days := clampInt(q.Get("days"), defaultStatsDays, 1, maxStatsDays)
	since := time.Now().AddDate(0, 0, -days)

	st, err := h.store.Stats(r.Context(), user, since, 10)
	if err != nil {
		h.log.Error("stats: query failed", "user", user, "err", err)
		writeJSON(w, http.StatusInternalServerError, statsError{"could not load stats"})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
