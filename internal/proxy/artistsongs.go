package proxy

import (
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"sync"

	"github.com/unclefroob/maraetai-service/internal/navidrome"
	"github.com/unclefroob/maraetai-service/internal/subsonic"
)

// artistSongsConcurrency bounds how many getAlbum calls run at once when
// assembling a discography, so a deep catalogue doesn't stampede Navidrome.
const artistSongsConcurrency = 8

// artistSongsHandler serves /rest/getArtistSongs: every song in an artist's
// discography, in album order, in a single client request. The proxy does the
// getArtist + per-album getAlbum fan-out server-side (concurrently, next to
// Navidrome) so the client pays one round-trip instead of N.
type artistSongsHandler struct {
	nd  *navidrome.Client
	log *slog.Logger
}

func (h *artistSongsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("id")
	if id == "" {
		subsonic.WriteError(w, q, subsonic.ErrRequiredParam, "Required parameter id is missing")
		return
	}

	// Reuse the caller's credentials for the upstream calls.
	auth := url.Values{}
	for _, k := range []string{"u", "t", "s", "p", "c", "v"} {
		if v := q.Get(k); v != "" {
			auth.Set(k, v)
		}
	}

	albums, err := h.nd.GetArtist(r.Context(), id, auth)
	if err != nil {
		h.log.Warn("getArtistSongs: getArtist failed", "artist", id, "err", err)
		subsonic.WriteError(w, q, subsonic.ErrGeneric, "Could not load artist")
		return
	}

	// Newest-first, so the discography order matches what the apps expect.
	sort.SliceStable(albums, func(i, j int) bool { return albums[i].Year > albums[j].Year })

	// Fetch each album's songs concurrently (bounded), keeping album order by
	// writing into a per-album slot. Failed albums are skipped (best-effort).
	perAlbum := make([][]subsonic.Child, len(albums))
	sem := make(chan struct{}, artistSongsConcurrency)
	var wg sync.WaitGroup
	for i, alb := range albums {
		wg.Add(1)
		sem <- struct{}{}
		go func(slot int, albumID string) {
			defer wg.Done()
			defer func() { <-sem }()
			songs, err := h.nd.GetAlbum(r.Context(), albumID, auth)
			if err != nil {
				h.log.Warn("getArtistSongs: getAlbum failed", "album", albumID, "err", err)
				return
			}
			children := make([]subsonic.Child, 0, len(songs))
			for _, s := range songs {
				children = append(children, songToChild(s))
			}
			perAlbum[slot] = children
		}(i, alb.ID)
	}
	wg.Wait()

	var songs []subsonic.Child
	for _, album := range perAlbum {
		songs = append(songs, album...)
	}
	subsonic.WriteArtistSongs(w, q, songs)
}

func songToChild(s navidrome.Song) subsonic.Child {
	return subsonic.Child{
		ID:          s.ID,
		IsDir:       false,
		Title:       s.Title,
		Album:       s.Album,
		Artist:      s.Artist,
		AlbumID:     s.AlbumID,
		CoverArt:    s.CoverArt,
		Duration:    s.Duration,
		Suffix:      s.Suffix,
		ContentType: s.ContentType,
		BitRate:     s.BitRate,
		Type:        "music",
	}
}
