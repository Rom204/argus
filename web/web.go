// Package web holds the single-page UI and serves it.
//
// The page is embedded into the binary, so argus-api is self-contained: there
// is no directory of static files to deploy next to it, and no path to get
// wrong at startup. This is the same //go:embed mechanism the agent uses for
// the compiled BPF object.
//
// The embed lives in this package rather than in cmd/argus-api because
// //go:embed cannot reach a parent directory — the directive has to sit beside
// the file it embeds.
package web

import (
	"embed"
	"net/http"
)

//go:embed index.html
var files embed.FS

// Handler serves the page at / and nothing else.
//
// Written out rather than using http.FileServerFS so the behaviour is explicit:
// exactly one path, one content type, and a 404 for anything else.
func Handler() http.Handler {
	page, err := files.ReadFile("index.html")
	if err != nil {
		// Unreachable: //go:embed fails the build if index.html is missing.
		panic("web: embedded index.html missing: " + err.Error())
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
}
