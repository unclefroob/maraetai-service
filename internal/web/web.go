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
func Handler() http.Handler {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err) // embedded path is a compile-time constant; cannot fail
	}
	return http.FileServer(http.FS(sub))
}
