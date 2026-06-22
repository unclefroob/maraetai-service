package proxy

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/unclefroob/maraetai-service/internal/auth"
	"github.com/unclefroob/maraetai-service/internal/store"
	"github.com/unclefroob/maraetai-service/internal/subsonic"
)

const (
	defaultRecentCount = 50
	maxRecentCount     = 500
)

// recentsHandler serves /rest/getRecentlyPlayed from the play store: the
// per-song de-duplicated, most-recent-first listen history the Subsonic API
// doesn't provide. Auth is validated against upstream so only the requesting
// user's own history is returned.
type recentsHandler struct {
	store *store.Store
	auth  *auth.Validator
	log   *slog.Logger
}

func (h *recentsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		h.log.Error("recents: auth validation failed", "err", err)
		subsonic.WriteError(w, q, subsonic.ErrGeneric, "Authentication unavailable")
		return
	}

	count := clampInt(q.Get("count"), defaultRecentCount, 1, maxRecentCount)
	offset := clampInt(q.Get("offset"), 0, 0, 1<<31-1)

	plays, err := h.store.RecentlyPlayedDistinct(r.Context(), user, count, offset)
	if err != nil {
		h.log.Error("recents: query failed", "user", user, "err", err)
		subsonic.WriteError(w, q, subsonic.ErrGeneric, "Could not load play history")
		return
	}

	songs := make([]subsonic.Child, 0, len(plays))
	for _, p := range plays {
		songs = append(songs, subsonic.Child{
			ID:          p.SongID,
			IsDir:       false,
			Title:       p.Title,
			Album:       p.Album,
			Artist:      p.Artist,
			AlbumID:     p.AlbumID,
			CoverArt:    p.CoverArt,
			Duration:    p.Duration,
			Suffix:      p.Suffix,
			ContentType: p.ContentType,
			BitRate:     p.BitRate,
			Type:        "music",
			PlayedAt:    p.PlayedAt.Unix(),
		})
	}
	subsonic.WriteRecentlyPlayed(w, q, songs)
}

// clampInt parses s, falling back to def, then clamps to [min, max].
func clampInt(s string, def, min, max int) int {
	n := def
	if s != "" {
		if parsed, err := strconv.Atoi(s); err == nil {
			n = parsed
		}
	}
	if n < min {
		n = min
	}
	if n > max {
		n = max
	}
	return n
}
