package proxy

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/unclefroob/maraetai-service/internal/auth"
	"github.com/unclefroob/maraetai-service/internal/store"
	"github.com/unclefroob/maraetai-service/internal/subsonic"
)

// On Repeat defaults: most-replayed songs in the last 30 days, ≥3 plays, top 50.
const (
	defaultOnRepeatDays     = 30
	defaultOnRepeatMinPlays = 3
	defaultOnRepeatCount    = 50
	maxOnRepeatCount        = 500
)

// onRepeatHandler serves /rest/getOnRepeat: the user's most-replayed songs from
// the play store — song-level "On Repeat", which plain Navidrome can't provide
// (it only exposes play frequency per album).
type onRepeatHandler struct {
	store *store.Store
	auth  *auth.Validator
	log   *slog.Logger
}

func (h *onRepeatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		h.log.Error("onRepeat: auth validation failed", "err", err)
		subsonic.WriteError(w, q, subsonic.ErrGeneric, "Authentication unavailable")
		return
	}

	days := clampInt(q.Get("days"), defaultOnRepeatDays, 1, 3650)
	minPlays := clampInt(q.Get("minPlays"), defaultOnRepeatMinPlays, 1, 1000)
	count := clampInt(q.Get("count"), defaultOnRepeatCount, 1, maxOnRepeatCount)
	since := time.Now().AddDate(0, 0, -days)

	plays, err := h.store.OnRepeat(r.Context(), user, since, minPlays, count)
	if err != nil {
		h.log.Error("onRepeat: query failed", "user", user, "err", err)
		subsonic.WriteError(w, q, subsonic.ErrGeneric, "Could not load On Repeat")
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
			PlayCount:   p.Plays,
		})
	}
	subsonic.WriteOnRepeat(w, q, songs)
}
