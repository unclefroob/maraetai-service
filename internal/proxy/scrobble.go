package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/unclefroob/maraetai-service/internal/navidrome"
	"github.com/unclefroob/maraetai-service/internal/store"
)

// metaResolver fetches and caches song metadata. Metadata changes rarely, so a
// process-lifetime cache keyed by song id avoids hammering upstream on every
// scrobble.
type metaResolver struct {
	nd    *navidrome.Client
	mu    sync.RWMutex
	cache map[string]navidrome.Song
}

func newMetaResolver(nd *navidrome.Client) *metaResolver {
	return &metaResolver{nd: nd, cache: make(map[string]navidrome.Song)}
}

func (m *metaResolver) get(ctx context.Context, id string, auth url.Values) (navidrome.Song, bool) {
	m.mu.RLock()
	s, ok := m.cache[id]
	m.mu.RUnlock()
	if ok {
		return s, true
	}
	song, err := m.nd.GetSong(ctx, id, auth)
	if err != nil || song == nil {
		return navidrome.Song{}, false
	}
	m.mu.Lock()
	m.cache[id] = *song
	m.mu.Unlock()
	return *song, true
}

// scrobbleTee records plays from a Subsonic scrobble request, then forwards the
// request upstream unchanged. Recording happens asynchronously so it never
// delays or breaks the client's playback path.
type scrobbleTee struct {
	next  http.Handler
	store *store.Store
	meta  *metaResolver
	log   *slog.Logger
}

func (t *scrobbleTee) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Snapshot what we need from the query before forwarding (the reverse proxy
	// mutates the request). Subsonic scrobble is a GET, so params live in the URL.
	q := r.URL.Query()

	// submission=false is a "now playing" notification, not a completed play.
	if v := q.Get("submission"); v == "false" || v == "False" {
		t.next.ServeHTTP(w, r)
		return
	}

	user := q.Get("u")
	client := q.Get("c")
	ids := q["id"]
	times := q["time"]

	// Auth params reused for the metadata lookup.
	auth := url.Values{}
	for _, k := range []string{"u", "t", "s", "p", "c", "v"} {
		if val := q.Get(k); val != "" {
			auth.Set(k, val)
		}
	}

	if user != "" && len(ids) > 0 {
		// Pair each id with its corresponding time param when present.
		recs := make([]pendingPlay, len(ids))
		for i, id := range ids {
			pp := pendingPlay{songID: id, playedAt: time.Now().UTC()}
			if i < len(times) {
				if ms, err := strconv.ParseInt(times[i], 10, 64); err == nil && ms > 0 {
					pp.playedAt = time.UnixMilli(ms).UTC()
				}
			}
			recs[i] = pp
		}
		go t.record(user, client, auth, recs)
	}

	t.next.ServeHTTP(w, r)
}

type pendingPlay struct {
	songID   string
	playedAt time.Time
}

func (t *scrobbleTee) record(user, client string, auth url.Values, recs []pendingPlay) {
	// Detached from the request lifecycle; bound it ourselves.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for _, pp := range recs {
		p := store.Play{
			User:     user,
			SongID:   pp.songID,
			PlayedAt: pp.playedAt,
			Client:   client,
		}
		if song, ok := t.meta.get(ctx, pp.songID, auth); ok {
			p.Title = song.Title
			p.Artist = song.Artist
			p.Album = song.Album
			p.AlbumID = song.AlbumID
			p.CoverArt = song.CoverArt
			p.Duration = song.Duration
			p.Suffix = song.Suffix
			p.ContentType = song.ContentType
			p.BitRate = song.BitRate
		} else {
			t.log.Warn("scrobble: metadata lookup failed", "song_id", pp.songID)
		}
		if err := t.store.InsertPlay(ctx, p); err != nil {
			t.log.Error("scrobble: insert play failed", "song_id", pp.songID, "err", err)
		}
	}
}
