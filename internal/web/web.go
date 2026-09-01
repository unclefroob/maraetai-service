// Package web serves the embedded single-page app from the service binary.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var files embed.FS

// Handler returns an http.Handler serving the SPA's static assets. Mount it
// under a prefix (e.g. http.StripPrefix("/app/", web.Handler())).
//
// Files embedded via go:embed report a zero ModTime, so http.FileServer never
// emits a Last-Modified (or any other validator) for them — with no
// Cache-Control either, browsers are free to cache a build's JS/CSS
// indefinitely and silently keep serving it after a redeploy ships a fix.
// (This bit us twice: once as a missing icon that turned out to already be
// fixed server-side, and once as a "drag-and-drop doesn't work" that was
// actually a stale app.js.) no-store forces a fresh fetch every load; these
// files are tiny, so the cost is negligible for a single-user home service.
func Handler() http.Handler {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err) // embedded path is a compile-time constant; cannot fail
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		fileServer.ServeHTTP(w, r)
	})
}
