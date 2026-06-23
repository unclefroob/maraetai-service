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
	artistsDefaultCount = 60
	artistsMaxCount     = 500
	artistsCacheTTL     = 60 * time.Second
)

// artistsHandler serves /rest/getArtistList: a paged, slim view of the library
// artist index. Subsonic's getArtists returns the whole index in one shot, so
// the proxy fetches it once (cached briefly per user, on the fast upstream link)
// and returns just the requested offset/count window over the slow client link.
type artistsHandler struct {
	auth *auth.Validator
	nd   *navidrome.Client
	log  *slog.Logger

	mu    sync.Mutex
	cache map[string]artistsEntry // user → full index (short TTL)
}

type artistsEntry struct {
	artists []navidrome.Artist
	at      time.Time
}

func newArtistsHandler(validator *auth.Validator, nd *navidrome.Client, log *slog.Logger) *artistsHandler {
	return &artistsHandler{auth: validator, nd: nd, log: log, cache: map[string]artistsEntry{}}
}

func (h *artistsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		h.log.Error("artists: auth validation failed", "err", err)
		subsonic.WriteError(w, q, subsonic.ErrGeneric, "Authentication unavailable")
		return
	}

	offset := clampInt(q.Get("offset"), 0, 0, 1<<30)
	count := clampInt(q.Get("count"), artistsDefaultCount, 1, artistsMaxCount)

	authParams := url.Values{}
	for _, k := range []string{"u", "t", "s", "p", "c", "v"} {
		if v := q.Get(k); v != "" {
			authParams.Set(k, v)
		}
	}

	artists, err := h.index(r.Context(), user, authParams)
	if err != nil {
		h.log.Error("artists: index lookup failed", "user", user, "err", err)
		subsonic.WriteError(w, q, subsonic.ErrGeneric, "Could not load artists")
		return
	}

	page := []subsonic.Artist{}
	if offset < len(artists) {
		end := offset + count
		if end > len(artists) {
			end = len(artists)
		}
		for _, a := range artists[offset:end] {
			page = append(page, subsonic.Artist{
				ID:         a.ID,
				Name:       a.Name,
				CoverArt:   a.CoverArt,
				AlbumCount: a.AlbumCount,
			})
		}
	}
	subsonic.WriteArtistList(w, q, page)
}

// index returns the user's full artist index, cached briefly so paging through
// it doesn't re-hit upstream once per page.
func (h *artistsHandler) index(ctx context.Context, user string, authParams url.Values) ([]navidrome.Artist, error) {
	h.mu.Lock()
	if e, ok := h.cache[user]; ok && time.Since(e.at) < artistsCacheTTL {
		h.mu.Unlock()
		return e.artists, nil
	}
	h.mu.Unlock()

	artists, err := h.nd.GetArtists(ctx, authParams)
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	if len(h.cache) > 256 {
		h.cache = map[string]artistsEntry{}
	}
	h.cache[user] = artistsEntry{artists: artists, at: time.Now()}
	h.mu.Unlock()
	return artists, nil
}
