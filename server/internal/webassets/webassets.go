// Package webassets embeds the dashboard frontend into the server
// binary so a deployment needs nothing besides the binary and a
// config file. The canonical frontend source lives in web/public/ at
// the repo root; dist/ is a build-time copy kept in sync by
// `make sync-web` and guarded by TestDistMatchesCanonicalSource.
package webassets

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the embedded frontend rooted at the dist directory, ready
// to serve with http.FileServer(http.FS(webassets.FS())).
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// Unreachable: "dist" is embedded at compile time.
		panic(err)
	}
	return sub
}
