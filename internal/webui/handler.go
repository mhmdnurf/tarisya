package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed dist
var content embed.FS

func Handler() http.Handler {
	dist, err := fs.Sub(content, "dist")
	if err != nil {
		panic(err)
	}

	files := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		if requestPath == "." || requestPath == "" {
			r.URL.Path = "/"
			files.ServeHTTP(w, r)
			return
		}

		if _, err := fs.Stat(dist, requestPath); err == nil {
			files.ServeHTTP(w, r)
			return
		}

		// React Router fallback.
		r.URL.Path = "/"
		files.ServeHTTP(w, r)
	})
}
