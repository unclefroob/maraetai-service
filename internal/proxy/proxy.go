// Package proxy implements the transparent Navidrome reverse proxy.
//
// Everything is forwarded upstream unchanged by default. Specific routes are
// registered ahead of the catch-all so future milestones can tee or augment
// individual Subsonic endpoints (e.g. /rest/scrobble, /rest/getRecentlyPlayed)
// without touching the audio/streaming hot path.
package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/unclefroob/maraetai-service/internal/auth"
	"github.com/unclefroob/maraetai-service/internal/navidrome"
	"github.com/unclefroob/maraetai-service/internal/store"
	"github.com/unclefroob/maraetai-service/internal/web"
)

// New builds the top-level handler: a router whose catch-all is a streaming
// reverse proxy to the given Navidrome upstream.
//
// If st is non-nil, scrobble requests are tee'd into the play store before
// being forwarded. A nil store yields a pure passthrough proxy.
func New(upstream *url.URL, st *store.Store, log *slog.Logger) http.Handler {
	rp := httputil.NewSingleHostReverseProxy(upstream)

	// FlushInterval -1 flushes writes to the client immediately, so audio
	// streams (stream/download) and chunked responses are never buffered.
	rp.FlushInterval = -1

	// Preserve the default director's path/query rewriting, but also point the
	// Host header at the upstream so Navidrome sees a coherent request.
	inner := rp.Director
	rp.Director = func(r *http.Request) {
		inner(r)
		r.Host = upstream.Host
	}

	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Error("upstream proxy error", "path", r.URL.Path, "err", err)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}

	mux := http.NewServeMux()

	// Liveness check for the proxy itself (does not touch Navidrome).
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// M1: tee scrobbles into the play store, then forward unchanged. Subsonic
	// clients may call the endpoint with or without the ".view" suffix.
	if st != nil {
		tee := &scrobbleTee{
			next:  rp,
			store: st,
			meta:  newMetaResolver(navidrome.New(upstream)),
			log:   log,
		}
		mux.Handle("/rest/scrobble", tee)
		mux.Handle("/rest/scrobble.view", tee)

		// M2: serve per-song de-duplicated recent plays from the store. Auth is
		// validated against upstream (forward-and-validate); no local user table.
		recents := &recentsHandler{
			store: st,
			auth:  auth.NewValidator(upstream),
			log:   log,
		}
		mux.Handle("/rest/getRecentlyPlayed", recents)
		mux.Handle("/rest/getRecentlyPlayed.view", recents)

		// On Repeat: the user's most-replayed songs (song-level, from the store).
		onRepeat := &onRepeatHandler{
			store: st,
			auth:  auth.NewValidator(upstream),
			log:   log,
		}
		mux.Handle("/rest/getOnRepeat", onRepeat)
		mux.Handle("/rest/getOnRepeat.view", onRepeat)

		// M4: JSON stats API + the embedded SPA that consumes it (and the
		// recents endpoint above). The SPA lives at /app/ so it never shadows
		// the Subsonic surface forwarded to Navidrome.
		mux.Handle("/api/stats", &statsHandler{
			store: st,
			auth:  auth.NewValidator(upstream),
			log:   log,
		})
		mux.Handle("/app/", http.StripPrefix("/app/", web.Handler()))
		mux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/app/", http.StatusMovedPermanently)
		})
	}

	// getArtistSongs: whole-discography fan-out done server-side, so the client
	// pays one round-trip. Doesn't need the play store, so it's always offered.
	artistSongs := &artistSongsHandler{nd: navidrome.New(upstream), log: log}
	mux.Handle("/rest/getArtistSongs", artistSongs)
	mux.Handle("/rest/getArtistSongs.view", artistSongs)

	// Catch-all: forward everything else to Navidrome untouched.
	mux.Handle("/", rp)

	return logging(log, mux)
}

// logging wraps a handler with structured access logging. The wrapper
// preserves http.Flusher so streaming responses still flush incrementally.
func logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"bytes", rw.bytes,
		)
	})
}

// statusWriter captures the response status and byte count while transparently
// forwarding Flush so streamed bodies are not buffered.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wrote = true
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
