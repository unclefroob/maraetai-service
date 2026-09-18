package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/unclefroob/maraetai-service/internal/auth"
	"github.com/unclefroob/maraetai-service/internal/navidrome"
)

// validVideoID anchors to the character set Navidrome/Subsonic IDs actually
// use (alphanumerics, -, _) so a curated clip's filename can never escape
// videosDir. An allowlist, not a denylist — enumerating every dangerous
// path-traversal encoding is a losing game; anchoring what's *allowed* isn't.
var validVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// trackVideoHandler serves /rest/getTrackVideo: a self-curated, looping
// background video clip for a track, if the user has placed one in
// videosDir. Checks the song id first, then falls back to the song's album
// id — curators more often curate per-album than per-track.
//
// Unlike every other custom endpoint in this proxy, the response body is raw
// video bytes (consumed directly by AVPlayer), not a Subsonic envelope — so
// failures are plain HTTP status codes, not subsonic.WriteError.
type trackVideoHandler struct {
	auth      *auth.Validator
	nd        *navidrome.Client
	videosDir string
	log       *slog.Logger
}

func newTrackVideoHandler(validator *auth.Validator, nd *navidrome.Client, videosDir string, log *slog.Logger) *trackVideoHandler {
	return &trackVideoHandler{auth: validator, nd: nd, videosDir: videosDir, log: log}
}

func (h *trackVideoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	_, err := h.auth.Validate(r.Context(), q)
	switch {
	case errors.Is(err, auth.ErrMissingParams), errors.Is(err, auth.ErrUnauthorized):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	case err != nil:
		h.log.Error("trackVideo: auth validation failed", "err", err)
		http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
		return
	}

	id := q.Get("id")
	if !validVideoID.MatchString(id) {
		h.log.Warn("trackVideo: rejected id", "reason", "invalid", "raw", id)
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if h.serve(w, r, id) {
		return
	}

	if albumID, ok := h.resolveAlbum(r.Context(), id, authParams(q)); ok && validVideoID.MatchString(albumID) {
		if h.serve(w, r, albumID) {
			h.log.Info("trackVideo: hit", "tier", "album", "id", id, "albumId", albumID)
			return
		}
	}

	h.log.Info("trackVideo: miss", "id", id)
	http.NotFound(w, r)
}

// serve attempts to open <videosDir>/<id>.mp4 and, if present, stream it via
// http.ServeContent (Range-request support for seeking/looping). Returns
// false if no file exists for id, so the caller can try the next fallback —
// any error opening/stat-ing the candidate is treated as "not found" rather
// than surfaced, since a miss and an unreadable file both mean "nothing to
// serve here."
func (h *trackVideoHandler) serve(w http.ResponseWriter, r *http.Request, id string) bool {
	full := filepath.Join(h.videosDir, id+".mp4")

	// Defense in depth: even with validVideoID anchored, verify the resolved
	// path still lives inside videosDir before opening it.
	if !strings.HasPrefix(full, filepath.Clean(h.videosDir)+string(os.PathSeparator)) {
		return false
	}

	// Reject symlinks before following them — os.Open follows symlinks, and
	// the HasPrefix check above only validates the literal joined path, not
	// where a symlink at that path might point. videosDir shares its mounted
	// volume with the play-history SQLite DB, so a stray or malicious
	// symlink here must not become a way to read it.
	if li, err := os.Lstat(full); err != nil || li.Mode()&os.ModeSymlink != 0 {
		return false
	}

	f, err := os.Open(full)
	if err != nil {
		return false
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		return false
	}

	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, filepath.Base(full), fi.ModTime(), f)
	return true
}

// resolveAlbum looks up id's album id via the upstream getSong metadata, for
// the track→album fallback. auth carries the originating request's auth
// params so the lookup reuses the same credentials.
func (h *trackVideoHandler) resolveAlbum(ctx context.Context, id string, auth url.Values) (string, bool) {
	song, err := h.nd.GetSong(ctx, id, auth)
	if err != nil || song == nil || song.AlbumID == "" {
		return "", false
	}
	return song.AlbumID, true
}
