package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/unclefroob/maraetai-service/internal/auth"
	"github.com/unclefroob/maraetai-service/internal/navidrome"
	"github.com/unclefroob/maraetai-service/internal/subsonic"
)

const (
	favDefaultCount = 50
	favMaxCount     = 500
	favCacheTTL     = 60 * time.Second
)

// favouritesHandler serves /rest/getFavourites: a paged, slim view of the user's
// starred songs. Subsonic's getStarred2 can't page, so the proxy fetches the full
// starred list once (cached briefly per user, on the fast upstream link) and
// returns just the requested page over the slow client link.
type favouritesHandler struct {
	auth *auth.Validator
	nd   *navidrome.Client
	log  *slog.Logger

	mu    sync.Mutex
	cache map[string]favEntry // user → full starred list (short TTL)
}

type favEntry struct {
	songs []navidrome.Song
	at    time.Time
}

func newFavouritesHandler(validator *auth.Validator, nd *navidrome.Client, log *slog.Logger) *favouritesHandler {
	return &favouritesHandler{auth: validator, nd: nd, log: log, cache: map[string]favEntry{}}
}

func (h *favouritesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		h.log.Error("favourites: auth validation failed", "err", err)
		subsonic.WriteError(w, q, subsonic.ErrGeneric, "Authentication unavailable")
		return
	}

	offset := clampInt(q.Get("offset"), 0, 0, 1<<30)
	count := clampInt(q.Get("count"), favDefaultCount, 1, favMaxCount)

	authParams := url.Values{}
	for _, k := range []string{"u", "t", "s", "p", "c", "v"} {
		if v := q.Get(k); v != "" {
			authParams.Set(k, v)
		}
	}

	songs, err := h.starred(r.Context(), user, authParams)
	if err != nil {
		h.log.Error("favourites: starred lookup failed", "user", user, "err", err)
		subsonic.WriteError(w, q, subsonic.ErrGeneric, "Could not load favourites")
		return
	}

	// Page the cached full list.
	page := []subsonic.Child{}
	if offset < len(songs) {
		end := offset + count
		if end > len(songs) {
			end = len(songs)
		}
		for _, s := range songs[offset:end] {
			page = append(page, songToChild(s))
		}
	}
	subsonic.WriteFavourites(w, q, page)
}

// starred returns the user's full starred-songs list, cached briefly so paging
// through it doesn't re-hit upstream once per page.
func (h *favouritesHandler) starred(ctx context.Context, user string, authParams url.Values) ([]navidrome.Song, error) {
	h.mu.Lock()
	if e, ok := h.cache[user]; ok && time.Since(e.at) < favCacheTTL {
		h.mu.Unlock()
		return e.songs, nil
	}
	h.mu.Unlock()

	songs, err := h.nd.GetStarredSongs(ctx, authParams)
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	if len(h.cache) > 256 {
		h.cache = map[string]favEntry{}
	}
	h.cache[user] = favEntry{songs: songs, at: time.Now()}
	h.mu.Unlock()
	return songs, nil
}
