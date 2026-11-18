package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// staticFiles are the third-party scripts the web UI loads (web/vendor, see
// THIRD_PARTY.md) and the licence texts its About dialog links to.
//
//go:embed web/vendor web/licenses
var staticFiles embed.FS

var staticFS = func() fs.FS {
	f, err := fs.Sub(staticFiles, "web")
	if err != nil {
		panic(err)
	}
	return f
}()

// handleStatic serves /vendor/ and /licenses/. Both are public: static code
// and text without any data. Each vendored directory carries its version in
// its name, so its files can be cached for good.
func handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/") {
		http.NotFound(w, r) // no directory listings
		return
	}
	if strings.HasPrefix(r.URL.Path, "/vendor/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.FileServerFS(staticFS).ServeHTTP(w, r)
}
