package proxy

import (
	"errors"
	"log/slog"
	"net/http"
	"sort"

	"github.com/unclefroob/maraetai-service/internal/auth"
	"github.com/unclefroob/maraetai-service/internal/navidrome"
	"github.com/unclefroob/maraetai-service/internal/store"
	"github.com/unclefroob/maraetai-service/internal/subsonic"
)

// usersHandler serves /rest/getUsers. Navidrome's own implementation of this
// endpoint — unlike the wider Subsonic spec — only ever returns the caller
// themselves, which is why the admin web UI's per-user stats picker could
// never actually discover anyone else to switch to. For an admin caller, this
// instead merges in every username with at least one recorded play in the
// local store, the same store getRecentlyPlayed/stats already use to work
// around gaps in Navidrome's own Subsonic surface. Non-admins get just
// themselves, matching Navidrome's existing (and intentional) behaviour.
type usersHandler struct {
	store *store.Store
	auth  *auth.Validator
	nd    *navidrome.Client
	log   *slog.Logger
}

func (h *usersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	user, err := h.auth.Validate(r.Context(), q)
	switch {
	case errors.Is(err, auth.ErrMissingParams):
		subsonic.WriteError(w, q, subsonic.ErrRequiredParam, "Required parameter is missing")
		return
	case errors.Is(err, auth.ErrUnauthorized):
		subsonic.WriteError(w, q, subsonic.ErrWrongCredentials, "Wrong username or password")
		return
	case err != nil:
		h.log.Error("users: auth validation failed", "err", err)
		subsonic.WriteError(w, q, subsonic.ErrGeneric, "Authentication unavailable")
		return
	}

	usernames := []string{user}
	if isAdmin(r.Context(), h.nd, q, user, h.log) {
		known, err := h.store.DistinctUsers(r.Context())
		if err != nil {
			h.log.Error("users: distinct users query failed", "err", err)
		} else {
			seen := map[string]bool{user: true}
			for _, u := range known {
				if u == "" || seen[u] {
					continue
				}
				seen[u] = true
				usernames = append(usernames, u)
			}
			sort.Strings(usernames)
		}
	}

	subsonic.WriteUsers(w, q, usernames)
}
